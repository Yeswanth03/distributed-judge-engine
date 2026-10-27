package com.minicodejudge.catalog.controller;

import static org.hamcrest.Matchers.containsString;
import static org.hamcrest.Matchers.hasSize;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.header;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.ResultActions;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.postgresql.PostgreSQLContainer;

/** GET /internal/problems/{id}/testset against a real Postgres (Testcontainers). */
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
class InternalTestsetTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    @Value("${catalog.internal-token}")
    String token;

    private UUID problemId;

    /** A problem whose version 1 is published, with one sample case and one hidden case. */
    @BeforeEach
    void seedPublishedProblem() {
        jdbc.sql("TRUNCATE test_cases, testsets, problems").update();
        problemId = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb)
                VALUES (:id, 'two-sum', 'Two Sum', 'Find the pair.', 'easy', 100, 1500, 128)
                """)
                .param("id", problemId)
                .update();
        testset(1, "sample-in", "sample-out", true, "hidden-in", "hidden-out", false);
        publish(1);
    }

    @Test
    void explicitVersionReturnsEveryCaseAndIsCacheableForever() throws Exception {
        fetch("?version=1", token)
                .andExpect(status().isOk())
                .andExpect(header().string("Cache-Control", containsString("immutable")))
                .andExpect(jsonPath("$.problem_id").value(problemId.toString()))
                .andExpect(jsonPath("$.version").value(1))
                .andExpect(jsonPath("$.content_hash").value("hash-v1"))
                .andExpect(jsonPath("$.time_limit_ms").value(1500))
                .andExpect(jsonPath("$.memory_limit_mb").value(128))
                .andExpect(jsonPath("$.cases", hasSize(2)))
                .andExpect(jsonPath("$.cases[0].ordinal").value(1))
                .andExpect(jsonPath("$.cases[0].is_sample").value(true))
                .andExpect(jsonPath("$.cases[1].input").value("hidden-in"))
                .andExpect(jsonPath("$.cases[1].expected_output").value("hidden-out"))
                .andExpect(jsonPath("$.cases[1].is_sample").value(false));
    }

    @Test
    void withoutVersionReturnsTheCurrentVersionAndIsNotCached() throws Exception {
        testset(2, "v2-in", "v2-out", true);
        publish(2);

        fetch("", token)
                .andExpect(status().isOk())
                .andExpect(header().string("Cache-Control", "no-cache"))
                .andExpect(jsonPath("$.version").value(2))
                .andExpect(jsonPath("$.cases[0].input").value("v2-in"));
    }

    @Test
    void missingOrWrongTokenIsUnauthorized() throws Exception {
        fetch("?version=1", null)
                .andExpect(status().isUnauthorized())
                .andExpect(jsonPath("$.error").value("unauthorized"));
        fetch("?version=1", "wrong-token").andExpect(status().isUnauthorized());
    }

    @Test
    void draftVersionIsNeverServed() throws Exception {
        testset(2, "draft-in", "draft-out", true);

        fetch("?version=2", token)
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.message").value("test set version 2 is not published"));
    }

    @Test
    void unknownOrNeverPublishedProblemIsNotFound() throws Exception {
        mvc.perform(get("/internal/problems/" + UUID.randomUUID() + "/testset").header("X-Service-Token", token))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.message").value("problem not found"));

        UUID draftOnly = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb)
                VALUES (:id, 'draft-only', 'Draft', 'Not yet.', 'easy', 100, 1000, 256)
                """).param("id", draftOnly).update();
        mvc.perform(get("/internal/problems/" + draftOnly + "/testset").header("X-Service-Token", token))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.message").value("problem has no published test set"));
    }

    @Test
    void olderVersionStaysAvailableAfterANewerOneIsPublished() throws Exception {
        testset(2, "v2-in", "v2-out", true);
        publish(2);

        fetch("?version=1", token)
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.version").value(1))
                .andExpect(jsonPath("$.cases[0].input").value("sample-in"));
    }

    @Test
    void malformedIdIsABadRequest() throws Exception {
        mvc.perform(get("/internal/problems/not-a-uuid/testset").header("X-Service-Token", token))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error").value("bad_request"));
    }

    // ---- helpers ----

    private ResultActions fetch(String query, String serviceToken) throws Exception {
        var request = get("/internal/problems/" + problemId + "/testset" + query);
        if (serviceToken != null) {
            request.header("X-Service-Token", serviceToken);
        }
        return mvc.perform(request);
    }

    /** Inserts a draft version; cases are given as repeating (input, expected_output, is_sample) triples. */
    private void testset(int version, Object... cases) {
        jdbc.sql("INSERT INTO testsets (problem_id, version, content_hash) VALUES (:id, :v, :hash)")
                .param("id", problemId).param("v", version).param("hash", "hash-v" + version).update();
        for (int i = 0; i < cases.length; i += 3) {
            jdbc.sql("""
                    INSERT INTO test_cases (id, problem_id, testset_version, ordinal, input, expected_output, is_sample)
                    VALUES (:caseId, :id, :v, :ordinal, :input, :expected, :sample)
                    """)
                    .param("caseId", UUID.randomUUID()).param("id", problemId).param("v", version)
                    .param("ordinal", i / 3 + 1).param("input", cases[i]).param("expected", cases[i + 1])
                    .param("sample", cases[i + 2])
                    .update();
        }
    }

    private void publish(int version) {
        jdbc.sql("UPDATE testsets SET status = 'published', published_at = now() WHERE problem_id = :id AND version = :v")
                .param("id", problemId).param("v", version).update();
        jdbc.sql("UPDATE problems SET status = 'published', current_version = :v WHERE id = :id")
                .param("id", problemId).param("v", version).update();
    }
}
