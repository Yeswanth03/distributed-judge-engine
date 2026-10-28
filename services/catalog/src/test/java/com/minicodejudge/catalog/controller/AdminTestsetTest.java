package com.minicodejudge.catalog.controller;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.hamcrest.Matchers.containsString;
import static org.hamcrest.Matchers.matchesPattern;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc;
import org.springframework.dao.DataAccessException;
import org.springframework.http.MediaType;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.ResultActions;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.postgresql.PostgreSQLContainer;
import tools.jackson.databind.ObjectMapper;

/** Test set upload and publish against a real Postgres (Testcontainers), with the Flyway schema applied. */
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
class AdminTestsetTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    private static final ObjectMapper JSON = new ObjectMapper();

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    private UUID problemId;

    /** TRUNCATE skips the row-level immutability triggers, so published rows from earlier tests are cleared too. */
    @BeforeEach
    void seedDraftProblem() {
        jdbc.sql("TRUNCATE test_cases, testsets, problems CASCADE").update();
        problemId = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb)
                VALUES (:id, 'two-sum', 'Two Sum', 'Find the pair.', 'easy', 100, 1000, 256)
                """)
                .param("id", problemId)
                .update();
    }

    // ---- upload ----

    @Test
    void firstUploadIsDraftVersionOneWithCasesInOrder() throws Exception {
        upload(problemId, twoCases())
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.version").value(1))
                .andExpect(jsonPath("$.content_hash").value(matchesPattern("^[0-9a-f]{64}$")));

        assertThat(testsetStatus(1)).isEqualTo("draft");
        List<Map<String, Object>> cases = jdbc.sql("""
                SELECT ordinal, input, expected_output, is_sample FROM test_cases
                 WHERE problem_id = :id AND testset_version = 1 ORDER BY ordinal
                """).param("id", problemId).query().listOfRows();
        assertThat(cases).hasSize(2);
        assertThat(cases.get(0)).containsEntry("ordinal", 1).containsEntry("input", "2 7 11 15\n9")
                .containsEntry("expected_output", "0 1").containsEntry("is_sample", true);
        assertThat(cases.get(1)).containsEntry("ordinal", 2).containsEntry("expected_output", "")
                .containsEntry("is_sample", false);
    }

    @Test
    void secondUploadIsVersionTwoAndLeavesVersionOneUntouched() throws Exception {
        String firstHash = hashOf(upload(problemId, twoCases()));
        String secondHash = hashOf(upload(problemId, twoCases()).andExpect(jsonPath("$.version").value(2)));

        assertThat(secondHash).as("identical cases give an identical fingerprint").isEqualTo(firstHash);
        assertThat(caseCount(1)).isEqualTo(2);
        assertThat(caseCount(2)).isEqualTo(2);
    }

    @Test
    void uploadToUnknownProblemIsNotFound() throws Exception {
        upload(UUID.randomUUID(), twoCases())
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.error").value("not_found"));
    }

    @Test
    void uploadWithoutASampleCaseIsRejected() throws Exception {
        upload(problemId, List.of(testCase("1", "1", false)))
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value("cases must include at least one sample case"));
        assertThat(jdbc.sql("SELECT COUNT(*) FROM testsets").query(Integer.class).single()).isZero();
    }

    @Test
    void caseMissingExpectedOutputIsRejectedNamingTheField() throws Exception {
        Map<String, Object> broken = new LinkedHashMap<>();
        broken.put("input", "1");
        broken.put("is_sample", true);

        upload(problemId, List.of(broken))
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value(containsString("cases[0].expected_output")));
    }

    @Test
    void uploadRequiresAdminRole() throws Exception {
        mvc.perform(post("/admin/problems/" + problemId + "/testsets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(JSON.writeValueAsString(Map.of("cases", twoCases()))))
                .andExpect(status().isForbidden());
    }

    // ---- publish ----

    @Test
    void publishMakesTheVersionCurrentAndTheProblemPublished() throws Exception {
        upload(problemId, twoCases());

        publish(problemId, 1).andExpect(status().isNoContent());

        Map<String, Object> testset = jdbc.sql("SELECT status, published_at FROM testsets WHERE version = 1")
                .query().singleRow();
        assertThat(testset).containsEntry("status", "published");
        assertThat(testset.get("published_at")).isNotNull();
        Map<String, Object> problem = jdbc.sql("SELECT status, current_version FROM problems").query().singleRow();
        assertThat(problem).containsEntry("status", "published").containsEntry("current_version", 1);
    }

    @Test
    void publishingTheSameVersionTwiceIsAConflict() throws Exception {
        upload(problemId, twoCases());
        publish(problemId, 1).andExpect(status().isNoContent());

        publish(problemId, 1)
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.message").value("test set version 1 is already published"));
    }

    @Test
    void publishingAnOlderVersionIsAConflict() throws Exception {
        upload(problemId, twoCases());
        upload(problemId, twoCases());
        publish(problemId, 2).andExpect(status().isNoContent());

        publish(problemId, 1).andExpect(status().isConflict());

        assertThat(testsetStatus(1)).isEqualTo("draft");
        assertThat(currentVersion()).isEqualTo(2);
    }

    @Test
    void publishingAnUnknownVersionIsNotFound() throws Exception {
        publish(problemId, 7)
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.message").value("test set version 7 not found"));
    }

    @Test
    void uploadAfterPublishIsADraftAndTheCurrentVersionStays() throws Exception {
        upload(problemId, twoCases());
        publish(problemId, 1);

        upload(problemId, twoCases()).andExpect(jsonPath("$.version").value(2));

        assertThat(testsetStatus(2)).isEqualTo("draft");
        assertThat(currentVersion()).isEqualTo(1);
    }

    @Test
    void databaseRefusesAnyChangeToAPublishedTestSet() throws Exception {
        upload(problemId, twoCases());
        publish(problemId, 1);

        assertThatThrownBy(() -> jdbc.sql("UPDATE test_cases SET expected_output = 'x'").update())
                .isInstanceOf(DataAccessException.class);
        assertThatThrownBy(() -> jdbc.sql("DELETE FROM test_cases").update())
                .isInstanceOf(DataAccessException.class);
        assertThatThrownBy(() -> jdbc.sql("""
                INSERT INTO test_cases (id, problem_id, testset_version, ordinal, input, expected_output)
                VALUES (:caseId, :id, 1, 3, 'x', 'y')
                """).param("caseId", UUID.randomUUID()).param("id", problemId).update())
                .isInstanceOf(DataAccessException.class);
        assertThatThrownBy(() -> jdbc.sql("UPDATE testsets SET status = 'draft'").update())
                .isInstanceOf(DataAccessException.class);

        assertThat(testsetStatus(1)).isEqualTo("published");
        assertThat(caseCount(1)).isEqualTo(2);
    }

    // ---- helpers ----

    private ResultActions upload(UUID id, List<Map<String, Object>> cases) throws Exception {
        return mvc.perform(post("/admin/problems/" + id + "/testsets")
                .header("X-User-Role", "admin")
                .contentType(MediaType.APPLICATION_JSON)
                .content(JSON.writeValueAsString(Map.of("cases", cases))));
    }

    private ResultActions publish(UUID id, int version) throws Exception {
        return mvc.perform(post("/admin/problems/" + id + "/testsets/" + version + "/publish")
                .header("X-User-Role", "admin"));
    }

    /** One sample case and one hidden case whose expected output is empty. */
    private static List<Map<String, Object>> twoCases() {
        return List.of(testCase("2 7 11 15\n9", "0 1", true), testCase("0\n0", "", false));
    }

    private static Map<String, Object> testCase(String input, String expectedOutput, boolean sample) {
        Map<String, Object> c = new LinkedHashMap<>();
        c.put("input", input);
        c.put("expected_output", expectedOutput);
        c.put("is_sample", sample);
        return c;
    }

    private static String hashOf(ResultActions result) throws Exception {
        return JSON.readTree(result.andReturn().getResponse().getContentAsString()).get("content_hash").asString();
    }

    private String testsetStatus(int version) {
        return jdbc.sql("SELECT status FROM testsets WHERE problem_id = :id AND version = :version")
                .param("id", problemId).param("version", version).query(String.class).single();
    }

    private int caseCount(int version) {
        return jdbc.sql("SELECT COUNT(*) FROM test_cases WHERE problem_id = :id AND testset_version = :version")
                .param("id", problemId).param("version", version).query(Integer.class).single();
    }

    private Integer currentVersion() {
        return jdbc.sql("SELECT current_version FROM problems WHERE id = :id")
                .param("id", problemId).query(Integer.class).single();
    }
}
