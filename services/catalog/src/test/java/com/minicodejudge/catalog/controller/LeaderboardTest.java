package com.minicodejudge.catalog.controller;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.hamcrest.Matchers.hasSize;
import static org.hamcrest.Matchers.nullValue;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.minicodejudge.catalog.messaging.VerdictEvent;
import com.minicodejudge.catalog.service.InvalidVerdictException;
import com.minicodejudge.catalog.service.LeaderboardService;
import java.time.Instant;
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

/** Leaderboard rules and endpoints against a real Postgres. Verdicts are applied directly, without a broker. */
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
class LeaderboardTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    private static final Instant T0 = Instant.parse("2026-11-01T10:00:00Z");

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    @Autowired
    LeaderboardService leaderboard;

    private UUID easy;
    private UUID hard;

    @BeforeEach
    void seedTwoProblems() {
        jdbc.sql("TRUNCATE results, user_problem_status, user_scores, test_cases, testsets, problems").update();
        easy = problem("easy-one", 100);
        hard = problem("hard-one", 300);
    }

    @Test
    void acceptedVerdictScoresTheProblemsPoints() {
        assertThat(leaderboard.apply(verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, T0))).isTrue();

        assertThat(score("alice")).isEqualTo(100);
    }

    @Test
    void theSameMessageTwiceCountsOnce() {
        VerdictEvent event = verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, T0);

        leaderboard.apply(event);
        assertThat(leaderboard.apply(event)).as("duplicate is ignored").isFalse();

        assertThat(score("alice")).isEqualTo(100);
    }

    @Test
    void lowerRejudgeSeqIsIgnored() {
        UUID submission = UUID.randomUUID();
        leaderboard.apply(verdict(submission, "alice", easy, "wrong_answer", 2, T0));

        assertThat(leaderboard.apply(verdict(submission, "alice", easy, "accepted", 1, T0))).isFalse();

        assertThat(score("alice")).isZero();
    }

    @Test
    void rejudgeThatTurnsAcceptedIntoWrongAnswerRemovesThePoints() {
        UUID submission = UUID.randomUUID();
        leaderboard.apply(verdict(submission, "alice", easy, "accepted", 0, T0));

        leaderboard.apply(verdict(submission, "alice", easy, "wrong_answer", 1, T0.plusSeconds(60)));

        assertThat(score("alice")).isZero();
    }

    @Test
    void solvingTheSameProblemTwiceCountsItOnce() {
        leaderboard.apply(verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, T0));
        leaderboard.apply(verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, T0.plusSeconds(30)));
        leaderboard.apply(verdict(UUID.randomUUID(), "alice", hard, "accepted", 0, T0.plusSeconds(60)));

        assertThat(score("alice")).isEqualTo(400);
        assertThat(jdbc.sql("SELECT solved_count FROM user_scores WHERE user_id = 'alice'").query(Integer.class).single())
                .isEqualTo(2);
    }

    @Test
    void verdictForAnUnknownOrDraftProblemIsRejected() {
        assertThatThrownBy(() -> leaderboard.apply(verdict(UUID.randomUUID(), "alice", UUID.randomUUID(), "accepted", 0, T0)))
                .isInstanceOf(InvalidVerdictException.class);

        jdbc.sql("UPDATE problems SET status = 'draft' WHERE id = :id").param("id", easy).update();
        assertThatThrownBy(() -> leaderboard.apply(verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, T0)))
                .isInstanceOf(InvalidVerdictException.class)
                .hasMessageContaining("not published");
        assertThat(jdbc.sql("SELECT count(*) FROM user_scores").query(Integer.class).single()).isZero();
    }

    @Test
    void leaderboardRanksByScoreThenWhoGotThereFirst() throws Exception {
        leaderboard.apply(verdict(UUID.randomUUID(), "carol", hard, "accepted", 0, T0));                  // 300
        leaderboard.apply(verdict(UUID.randomUUID(), "bob", easy, "accepted", 0, T0.plusSeconds(10)));    // 100, later
        leaderboard.apply(verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, T0.plusSeconds(5)));   // 100, earlier
        leaderboard.apply(verdict(UUID.randomUUID(), "dave", easy, "wrong_answer", 0, T0));               // no solve

        mvc.perform(get("/leaderboard"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.entries", hasSize(3)))
                .andExpect(jsonPath("$.entries[0].user_id").value("carol"))
                .andExpect(jsonPath("$.entries[0].rank").value(1))
                .andExpect(jsonPath("$.entries[1].user_id").value("alice"))
                .andExpect(jsonPath("$.entries[1].rank").value(2))
                .andExpect(jsonPath("$.entries[2].user_id").value("bob"))
                .andExpect(jsonPath("$.entries[2].rank").value(3));
    }

    @Test
    void leaderboardSaysHowFreshItIs() throws Exception {
        mvc.perform(get("/leaderboard"))
                .andExpect(jsonPath("$.as_of").value(nullValue()))
                .andExpect(jsonPath("$.entries", hasSize(0)));

        Instant judged = Instant.now().minusSeconds(3);
        leaderboard.apply(verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, judged));

        String body = mvc.perform(get("/leaderboard")).andExpect(status().isOk())
                .andReturn().getResponse().getContentAsString();
        assertThat(body).contains("\"as_of\":\"" + judged.toString().substring(0, 19));
        double lag = Double.parseDouble(body.replaceAll(".*\"lag_seconds\":([0-9.]+).*", "$1"));
        assertThat(lag).isBetween(2.5, 30.0);
    }

    @Test
    void userStandingShowsRankScoreAndSolvedProblems() throws Exception {
        leaderboard.apply(verdict(UUID.randomUUID(), "carol", hard, "accepted", 0, T0));
        leaderboard.apply(verdict(UUID.randomUUID(), "alice", easy, "accepted", 0, T0));
        leaderboard.apply(verdict(UUID.randomUUID(), "dave", easy, "wrong_answer", 0, T0));

        mvc.perform(get("/leaderboard/users/alice"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.rank").value(2))
                .andExpect(jsonPath("$.score").value(100))
                .andExpect(jsonPath("$.solved_problem_ids[0]").value(easy.toString()));
        mvc.perform(get("/leaderboard/users/dave"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.rank").value(nullValue()))
                .andExpect(jsonPath("$.score").value(0));
        mvc.perform(get("/leaderboard/users/nobody"))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.error").value("not_found"));
    }

    // ---- helpers ----

    private UUID problem(String slug, int points) {
        UUID id = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb, status)
                VALUES (:id, :slug, :slug, 'Statement.', 'easy', :points, 1000, 256, 'published')
                """)
                .param("id", id).param("slug", slug).param("points", points)
                .update();
        return id;
    }

    private static VerdictEvent verdict(UUID submission, String user, UUID problem, String verdict, int rejudgeSeq, Instant at) {
        return new VerdictEvent(submission, user, problem, 1, verdict, rejudgeSeq, at);
    }

    private int score(String user) {
        return jdbc.sql("SELECT score FROM user_scores WHERE user_id = :u").param("u", user).query(Integer.class).single();
    }
}
