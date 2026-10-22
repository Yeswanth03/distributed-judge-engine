package com.minicodejudge.catalog.controller;

import static org.assertj.core.api.Assertions.assertThat;
import static org.hamcrest.Matchers.containsString;
import static org.hamcrest.Matchers.hasSize;
import static org.hamcrest.Matchers.nullValue;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.test.web.servlet.MockMvc;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.postgresql.PostgreSQLContainer;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.ObjectMapper;

/** GET /problems and GET /problems/{slug} against a real Postgres (Testcontainers). No role header is ever sent. */
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
class ProblemReadTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    private static final ObjectMapper JSON = new ObjectMapper();

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    @BeforeEach
    void emptyTables() {
        jdbc.sql("TRUNCATE test_cases, testsets, problems").update();
    }

    // ---- GET /problems/{slug} ----

    @Test
    void publishedProblemShowsOnlySampleCasesInOrder() throws Exception {
        UUID id = problem("two-sum", "easy");
        testset(id, 1, "1", "a", true, "2", "secret", false, "3", "c", true);
        publish(id, 1);

        mvc.perform(get("/problems/two-sum"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.id").value(id.toString()))
                .andExpect(jsonPath("$.statement_md").value("Statement of two-sum"))
                .andExpect(jsonPath("$.time_limit_ms").value(1000))
                .andExpect(jsonPath("$.samples", hasSize(2)))
                .andExpect(jsonPath("$.samples[0].input").value("1"))
                .andExpect(jsonPath("$.samples[1].expected_output").value("c"))
                .andExpect(jsonPath("$.status").doesNotExist())
                .andExpect(jsonPath("$.current_version").doesNotExist())
                .andExpect(content -> {
                    String body = content.getResponse().getContentAsString();
                    if (body.contains("secret")) {
                        throw new AssertionError("hidden case leaked: " + body);
                    }
                });
    }

    @Test
    void draftProblemIsNotFound() throws Exception {
        problem("draft-only", "easy");

        mvc.perform(get("/problems/draft-only"))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.error").value("not_found"));
    }

    @Test
    void unknownSlugIsNotFound() throws Exception {
        mvc.perform(get("/problems/no-such-problem")).andExpect(status().isNotFound());
    }

    @Test
    void samplesFollowThePublishedVersionNotTheNewestDraft() throws Exception {
        UUID id = problem("two-sum", "easy");
        testset(id, 1, "v1", "old", true);
        publish(id, 1);
        testset(id, 2, "v2", "new", true);

        mvc.perform(get("/problems/two-sum")).andExpect(jsonPath("$.samples[0].input").value("v1"));

        publish(id, 2);
        mvc.perform(get("/problems/two-sum")).andExpect(jsonPath("$.samples[0].input").value("v2"));
    }

    // ---- GET /problems ----

    @Test
    void listShowsOnlyPublishedProblemsSortedBySlug() throws Exception {
        publishedProblem("zebra", "hard");
        publishedProblem("apple", "easy");
        problem("mango-draft", "medium");

        mvc.perform(get("/problems"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.items", hasSize(2)))
                .andExpect(jsonPath("$.items[0].slug").value("apple"))
                .andExpect(jsonPath("$.items[1].slug").value("zebra"))
                .andExpect(jsonPath("$.next_cursor").value(nullValue()));
    }

    @Test
    void cursorWalksEveryPageWithoutGapsOrRepeats() throws Exception {
        for (String slug : List.of("a", "b", "c", "d", "e")) {
            publishedProblem(slug, "easy");
        }

        List<String> seen = new ArrayList<>();
        String cursor = null;
        int pages = 0;
        do {
            String url = "/problems?limit=2" + (cursor == null ? "" : "&cursor=" + cursor);
            JsonNode page = JSON.readTree(mvc.perform(get(url)).andExpect(status().isOk())
                    .andReturn().getResponse().getContentAsString());
            page.get("items").forEach(item -> seen.add(item.get("slug").asString()));
            cursor = page.get("next_cursor").isNull() ? null : page.get("next_cursor").asString();
            pages++;
        } while (cursor != null);

        assertThat(seen).containsExactly("a", "b", "c", "d", "e");
        assertThat(pages).isEqualTo(3);
    }

    @Test
    void difficultyFilterReturnsOnlyThatDifficulty() throws Exception {
        publishedProblem("easy-one", "easy");
        publishedProblem("hard-one", "hard");

        mvc.perform(get("/problems?difficulty=hard"))
                .andExpect(jsonPath("$.items", hasSize(1)))
                .andExpect(jsonPath("$.items[0].slug").value("hard-one"));
    }

    @Test
    void invalidLimitOrDifficultyIsABadRequest() throws Exception {
        mvc.perform(get("/problems?limit=500"))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error").value("bad_request"))
                .andExpect(jsonPath("$.message").value(containsString("limit")));
        mvc.perform(get("/problems?limit=0")).andExpect(status().isBadRequest());
        mvc.perform(get("/problems?difficulty=brutal"))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.message").value(containsString("difficulty")));
    }

    // ---- helpers: seed straight into the database ----

    private UUID problem(String slug, String difficulty) {
        UUID id = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb)
                VALUES (:id, :slug, :title, :statement, :difficulty, 100, 1000, 256)
                """)
                .param("id", id).param("slug", slug).param("title", "Title of " + slug)
                .param("statement", "Statement of " + slug).param("difficulty", difficulty)
                .update();
        return id;
    }

    private void publishedProblem(String slug, String difficulty) {
        UUID id = problem(slug, difficulty);
        testset(id, 1, "in", "out", true);
        publish(id, 1);
    }

    /** Inserts a draft test set; cases are given as repeating (input, expected_output, is_sample) triples. */
    private void testset(UUID problemId, int version, Object... cases) {
        jdbc.sql("INSERT INTO testsets (problem_id, version, content_hash) VALUES (:id, :v, 'test')")
                .param("id", problemId).param("v", version).update();
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

    private void publish(UUID problemId, int version) {
        jdbc.sql("UPDATE testsets SET status = 'published', published_at = now() WHERE problem_id = :id AND version = :v")
                .param("id", problemId).param("v", version).update();
        jdbc.sql("UPDATE problems SET status = 'published', current_version = :v WHERE id = :id")
                .param("id", problemId).param("v", version).update();
    }
}
