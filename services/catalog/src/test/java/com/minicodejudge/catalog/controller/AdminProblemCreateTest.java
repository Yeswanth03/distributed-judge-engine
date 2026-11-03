package com.minicodejudge.catalog.controller;

import static org.assertj.core.api.Assertions.assertThat;
import static org.hamcrest.Matchers.allOf;
import static org.hamcrest.Matchers.containsString;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
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
import org.springframework.http.MediaType;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.MvcResult;
import org.springframework.test.web.servlet.ResultActions;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.postgresql.PostgreSQLContainer;
import tools.jackson.databind.ObjectMapper;

/** POST /admin/problems against a real Postgres (Testcontainers), with the Flyway schema applied. */
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
class AdminProblemCreateTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    private static final ObjectMapper JSON = new ObjectMapper();

    private static final String STARTER = """
            class Solution:
                def twoSum(self, nums: list[int], target: int) -> list[int]:
                    pass
            """;

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    @BeforeEach
    void emptyTable() {
        jdbc.sql("DELETE FROM problems").update();
    }

    @Test
    void validProblemIsCreatedAsDraft() throws Exception {
        MvcResult result = create(validBody(), "admin").andExpect(status().isCreated()).andReturn();

        UUID id = UUID.fromString(JSON.readTree(result.getResponse().getContentAsString()).get("id").asString());
        Map<String, Object> row = jdbc.sql("SELECT * FROM problems WHERE id = :id").param("id", id).query().singleRow();
        assertThat(row)
                .containsEntry("slug", "two-sum")
                .containsEntry("title", "Two Sum")
                .containsEntry("statement_md", "Return the indices of the two numbers that add up to the target.")
                .containsEntry("difficulty", "easy")
                .containsEntry("points", 100)
                .containsEntry("time_limit_ms", 1000)
                .containsEntry("memory_limit_mb", 256)
                .containsEntry("status", "draft");

        mvc.perform(get("/admin/problems/" + id).header("X-User-Role", "admin"))
                .andExpect(jsonPath("$.function_name").value("twoSum"))
                .andExpect(jsonPath("$.parameters[1].name").value("target"))
                .andExpect(jsonPath("$.parameters[1].type").value("int"))
                .andExpect(jsonPath("$.return_type").value("int[]"))
                .andExpect(jsonPath("$.starter_code").value(STARTER));
    }

    @Test
    void functionDetailsAreRequiredAndChecked() throws Exception {
        Map<String, Object> missing = validBody();
        missing.remove("function_name");
        missing.remove("parameters");
        create(missing, "admin")
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value(
                        allOf(containsString("function_name"), containsString("parameters"))));

        Map<String, Object> badType = validBody();
        badType.put("return_type", "list<int>");
        create(badType, "admin")
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value(containsString("return_type")));

        Map<String, Object> repeated = validBody();
        repeated.put("parameters", List.of(
                Map.of("name", "nums", "type", "int[]"), Map.of("name", "nums", "type", "int")));
        create(repeated, "admin")
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value("parameters must not repeat the name 'nums'"));

        Map<String, Object> reserved = validBody();
        reserved.put("parameters", List.of(Map.of("name", "self", "type", "int")));
        create(reserved, "admin")
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value("parameters must not use the reserved word 'self'"));

        Map<String, Object> wrongStarter = validBody();
        wrongStarter.put("starter_code", "class Solution:\n    def other(self):\n        pass\n");
        create(wrongStarter, "admin")
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value("starter_code must define twoSum"));

        assertThat(rowCount()).isZero();
    }

    @Test
    void withoutAdminRoleIsForbidden() throws Exception {
        create(validBody(), null)
                .andExpect(status().isForbidden())
                .andExpect(jsonPath("$.error").value("forbidden"));
        assertThat(rowCount()).isZero();
    }

    @Test
    void duplicateSlugIsAConflictAndTheFirstProblemIsKept() throws Exception {
        create(validBody(), "admin").andExpect(status().isCreated());

        Map<String, Object> second = validBody();
        second.put("title", "Another Title");
        create(second, "admin")
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.error").value("conflict"))
                .andExpect(jsonPath("$.message").value("a problem with slug 'two-sum' already exists"));

        assertThat(jdbc.sql("SELECT title FROM problems").query(String.class).list()).containsExactly("Two Sum");
    }

    @Test
    void invalidFieldsAreAllReportedByName() throws Exception {
        Map<String, Object> body = validBody();
        body.put("slug", "Bad Slug");
        body.put("difficulty", "brutal");
        body.put("time_limit_ms", 99);

        create(body, "admin")
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.error").value("validation_failed"))
                .andExpect(jsonPath("$.message").value(
                        allOf(containsString("slug"), containsString("difficulty"), containsString("time_limit_ms"))));
        assertThat(rowCount()).isZero();
    }

    @Test
    void missingNumberFieldIsAValidationErrorNotAMalformedBody() throws Exception {
        Map<String, Object> body = validBody();
        body.remove("points");

        create(body, "admin")
                .andExpect(status().isUnprocessableContent())
                .andExpect(jsonPath("$.message").value(containsString("points")));
    }

    @Test
    void malformedJsonIsABadRequest() throws Exception {
        mvc.perform(post("/admin/problems")
                        .header("X-User-Role", "admin")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content("{\"slug\":"))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error").value("bad_request"));
    }

    @Test
    void clientCannotChooseIdOrStatus() throws Exception {
        UUID chosen = UUID.randomUUID();
        Map<String, Object> body = validBody();
        body.put("id", chosen.toString());
        body.put("status", "published");

        create(body, "admin").andExpect(status().isCreated());

        Map<String, Object> row = jdbc.sql("SELECT id, status FROM problems").query().singleRow();
        assertThat(row.get("id")).isNotEqualTo(chosen);
        assertThat(row).containsEntry("status", "draft");
    }

    // ---- helpers ----

    private ResultActions create(Map<String, Object> body, String role) throws Exception {
        var request = post("/admin/problems")
                .contentType(MediaType.APPLICATION_JSON)
                .content(JSON.writeValueAsString(body));
        if (role != null) {
            request.header("X-User-Role", role);
        }
        return mvc.perform(request);
    }

    private static Map<String, Object> validBody() {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("slug", "two-sum");
        body.put("title", "Two Sum");
        body.put("statement_md", "Return the indices of the two numbers that add up to the target.");
        body.put("difficulty", "easy");
        body.put("points", 100);
        body.put("time_limit_ms", 1000);
        body.put("memory_limit_mb", 256);
        body.put("function_name", "twoSum");
        body.put("parameters", List.of(
                Map.of("name", "nums", "type", "int[]"), Map.of("name", "target", "type", "int")));
        body.put("return_type", "int[]");
        body.put("starter_code", STARTER);
        return body;
    }

    private int rowCount() {
        return jdbc.sql("SELECT COUNT(*) FROM problems").query(Integer.class).single();
    }
}
