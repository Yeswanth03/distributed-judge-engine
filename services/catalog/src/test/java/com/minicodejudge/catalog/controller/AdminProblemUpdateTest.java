package com.minicodejudge.catalog.controller;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.put;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.time.OffsetDateTime;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc;
import org.springframework.http.MediaType;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.ResultActions;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.postgresql.PostgreSQLContainer;
import tools.jackson.databind.ObjectMapper;

/** PUT /admin/problems/{id} against a real Postgres (Testcontainers), with the Flyway schema applied. */
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
class AdminProblemUpdateTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    private static final ObjectMapper JSON = new ObjectMapper();

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    private UUID id;

    /** Seeds one problem directly in the database, last updated an hour ago. */
    @BeforeEach
    void seedProblem() {
        jdbc.sql("DELETE FROM problems").update();
        id = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb,
                                      created_at, updated_at)
                VALUES (:id, 'two-sum', 'Two Sum', 'Original statement.', 'easy', 100, 1000, 256,
                        now() - interval '1 hour', now() - interval '1 hour')
                """)
                .param("id", id)
                .update();
    }

    @Test
    void draftProblemHasEveryEditableFieldReplaced() throws Exception {
        OffsetDateTime before = updatedAt();
        Map<String, Object> body = validBody();
        body.put("slug", "renamed-slug"); // not part of an update; must be ignored

        update(id.toString(), body).andExpect(status().isNoContent());

        Map<String, Object> row = jdbc.sql("SELECT * FROM problems WHERE id = :id").param("id", id).query().singleRow();
        assertThat(row)
                .containsEntry("slug", "two-sum")
                .containsEntry("title", "Two Sum II")
                .containsEntry("statement_md", "Updated statement.")
                .containsEntry("difficulty", "medium")
                .containsEntry("points", 200)
                .containsEntry("time_limit_ms", 2000)
                .containsEntry("memory_limit_mb", 512)
                .containsEntry("status", "draft");
        assertThat(updatedAt()).isAfter(before);
    }

    @Test
    void unknownIdIsNotFound() throws Exception {
        update(UUID.randomUUID().toString(), validBody())
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.error").value("not_found"));
    }

    @Test
    void malformedIdIsABadRequest() throws Exception {
        update("not-a-uuid", validBody())
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error").value("bad_request"));
    }

    @Test
    void invalidFieldIsRejectedAndNothingChanges() throws Exception {
        Map<String, Object> body = validBody();
        body.put("difficulty", "brutal");

        update(id.toString(), body).andExpect(status().isUnprocessableContent());

        assertThat(jdbc.sql("SELECT title FROM problems").query(String.class).single()).isEqualTo("Two Sum");
    }

    @Test
    void publishedProblemRejectsChangedPointsAndKeepsTheRow() throws Exception {
        publish();
        Map<String, Object> body = unchangedLockedFields();
        body.put("points", 500);

        update(id.toString(), body)
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.error").value("conflict"));

        Map<String, Object> row = jdbc.sql("SELECT title, points FROM problems").query().singleRow();
        assertThat(row).containsEntry("title", "Two Sum").containsEntry("points", 100);
    }

    @Test
    void publishedProblemAllowsTitleAndStatementChanges() throws Exception {
        publish();

        update(id.toString(), unchangedLockedFields()).andExpect(status().isNoContent());

        Map<String, Object> row = jdbc.sql("SELECT title, statement_md FROM problems").query().singleRow();
        assertThat(row).containsEntry("title", "Two Sum II").containsEntry("statement_md", "Updated statement.");
    }

    // ---- helpers ----

    private ResultActions update(String problemId, Map<String, Object> body) throws Exception {
        return mvc.perform(put("/admin/problems/" + problemId)
                .header("X-User-Role", "admin")
                .contentType(MediaType.APPLICATION_JSON)
                .content(JSON.writeValueAsString(body)));
    }

    /** Changes every editable field, including the ones that lock on publish. */
    private static Map<String, Object> validBody() {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("title", "Two Sum II");
        body.put("statement_md", "Updated statement.");
        body.put("difficulty", "medium");
        body.put("points", 200);
        body.put("time_limit_ms", 2000);
        body.put("memory_limit_mb", 512);
        return body;
    }

    /** New title and statement; difficulty, points and limits resent exactly as seeded. */
    private static Map<String, Object> unchangedLockedFields() {
        Map<String, Object> body = validBody();
        body.put("difficulty", "easy");
        body.put("points", 100);
        body.put("time_limit_ms", 1000);
        body.put("memory_limit_mb", 256);
        return body;
    }

    /** Nothing publishes yet (that arrives with test sets), so flip the status directly. */
    private void publish() {
        jdbc.sql("UPDATE problems SET status = 'published' WHERE id = :id").param("id", id).update();
    }

    private OffsetDateTime updatedAt() {
        return jdbc.sql("SELECT updated_at FROM problems WHERE id = :id").param("id", id).query(OffsetDateTime.class).single();
    }
}
