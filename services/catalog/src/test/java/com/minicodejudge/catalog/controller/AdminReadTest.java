package com.minicodejudge.catalog.controller;

import static org.hamcrest.Matchers.hasSize;
import static org.hamcrest.Matchers.nullValue;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.ResultActions;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.postgresql.PostgreSQLContainer;

/** Admin reads (drafts included) and 404 for unknown URLs, against a real Postgres. */
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
class AdminReadTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    private UUID published;
    private UUID draft;

    /** "apple" is published at version 1 and has a draft version 2; "banana" is a draft with nothing uploaded. */
    @BeforeEach
    void seed() {
        jdbc.sql("TRUNCATE test_cases, testsets, problems CASCADE").update();
        published = problem("apple");
        draft = problem("banana");
        testset(published, 1, true, false);
        testset(published, 2, true);
        jdbc.sql("UPDATE testsets SET status = 'published', published_at = now() WHERE problem_id = :id AND version = 1")
                .param("id", published).update();
        jdbc.sql("UPDATE problems SET status = 'published', current_version = 1 WHERE id = :id")
                .param("id", published).update();
    }

    @Test
    void listIncludesDraftsWithStatusAndCurrentVersion() throws Exception {
        admin("/admin/problems")
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.items", hasSize(2)))
                .andExpect(jsonPath("$.items[0].slug").value("apple"))
                .andExpect(jsonPath("$.items[0].status").value("published"))
                .andExpect(jsonPath("$.items[0].current_version").value(1))
                .andExpect(jsonPath("$.items[1].slug").value("banana"))
                .andExpect(jsonPath("$.items[1].status").value("draft"))
                .andExpect(jsonPath("$.items[1].current_version").value(nullValue()))
                .andExpect(jsonPath("$.next_cursor").value(nullValue()));

        admin("/admin/problems?status=draft")
                .andExpect(jsonPath("$.items", hasSize(1)))
                .andExpect(jsonPath("$.items[0].slug").value("banana"));
        admin("/admin/problems?limit=1")
                .andExpect(jsonPath("$.items", hasSize(1)))
                .andExpect(jsonPath("$.next_cursor").value("apple"));
        admin("/admin/problems?status=archived").andExpect(status().isBadRequest());
    }

    @Test
    void getShowsADraftInFull() throws Exception {
        admin("/admin/problems/" + draft)
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.slug").value("banana"))
                .andExpect(jsonPath("$.statement_md").value("Statement of banana"))
                .andExpect(jsonPath("$.status").value("draft"))
                .andExpect(jsonPath("$.created_at").exists());
        admin("/admin/problems/" + UUID.randomUUID()).andExpect(status().isNotFound());
    }

    @Test
    void versionsAreListedNewestFirstWithCountsAndTheCurrentOneMarked() throws Exception {
        admin("/admin/problems/" + published + "/testsets")
                .andExpect(status().isOk())
                .andExpect(jsonPath("$", hasSize(2)))
                .andExpect(jsonPath("$[0].version").value(2))
                .andExpect(jsonPath("$[0].status").value("draft"))
                .andExpect(jsonPath("$[0].current").value(false))
                .andExpect(jsonPath("$[0].published_at").value(nullValue()))
                .andExpect(jsonPath("$[1].version").value(1))
                .andExpect(jsonPath("$[1].status").value("published"))
                .andExpect(jsonPath("$[1].current").value(true))
                .andExpect(jsonPath("$[1].case_count").value(2))
                .andExpect(jsonPath("$[1].sample_count").value(1));

        admin("/admin/problems/" + draft + "/testsets").andExpect(jsonPath("$", hasSize(0)));
        admin("/admin/problems/" + UUID.randomUUID() + "/testsets").andExpect(status().isNotFound());
    }

    @Test
    void adminReadsNeedTheAdminRole() throws Exception {
        mvc.perform(get("/admin/problems")).andExpect(status().isForbidden());
        mvc.perform(get("/admin/problems/" + draft + "/testsets")).andExpect(status().isForbidden());
    }

    @Test
    void unknownUrlIsNotFoundNotForbidden() throws Exception {
        admin("/admin/problemz")
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.error").value("not_found"))
                .andExpect(jsonPath("$.message").value("no such endpoint"));
        mvc.perform(get("/no-such-page")).andExpect(status().isNotFound());
    }

    // ---- helpers ----

    private ResultActions admin(String url) throws Exception {
        return mvc.perform(get(url).header("X-User-Role", "admin"));
    }

    private UUID problem(String slug) {
        UUID id = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb)
                VALUES (:id, :slug, :slug, :statement, 'easy', 100, 1000, 256)
                """)
                .param("id", id).param("slug", slug).param("statement", "Statement of " + slug)
                .update();
        return id;
    }

    /** A draft version with one case per flag (true = sample). */
    private void testset(UUID problemId, int version, boolean... samples) {
        jdbc.sql("INSERT INTO testsets (problem_id, version, content_hash) VALUES (:id, :v, 'hash')")
                .param("id", problemId).param("v", version).update();
        for (int i = 0; i < samples.length; i++) {
            jdbc.sql("""
                    INSERT INTO test_cases (id, problem_id, testset_version, ordinal, input, expected_output, is_sample)
                    VALUES (:caseId, :id, :v, :ordinal, 'in', 'out', :sample)
                    """)
                    .param("caseId", UUID.randomUUID()).param("id", problemId).param("v", version)
                    .param("ordinal", i + 1).param("sample", samples[i])
                    .update();
        }
    }
}
