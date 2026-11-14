package com.minicodejudge.catalog.repository;

import com.minicodejudge.catalog.messaging.VerdictEvent;
import java.sql.Timestamp;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** SQL for the leaderboard read model (results, user_problem_status, user_scores). No rules live here. */
@Repository
public class LeaderboardRepository {

    private final JdbcClient jdbc;

    public LeaderboardRepository(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    // ---- writes (one verdict, inside the caller's transaction) ----

    /**
     * Holds a lock on this user until the transaction ends, so two verdicts for the same user (on any consumer
     * instance) recompute one after the other; otherwise each could miss the other's uncommitted result.
     */
    public void lockUser(String userId) {
        jdbc.sql("SELECT 1 FROM (SELECT pg_advisory_xact_lock(hashtextextended(:userId, 0))) locked")
                .param("userId", userId)
                .query(Integer.class)
                .single();
    }

    /**
     * Stores the verdict unless a newer rejudge of the submission is already there. Returns 1 when the row was
     * inserted or replaced (higher rejudge_seq), 0 for a duplicate (equal) or stale (lower) message.
     */
    public int upsertResult(VerdictEvent e) {
        return jdbc.sql("""
                INSERT INTO results (submission_id, user_id, problem_id, testset_version, verdict, rejudge_seq, judged_at)
                VALUES (:submissionId, :userId, :problemId, :testsetVersion, :verdict, :rejudgeSeq, :judgedAt)
                ON CONFLICT (submission_id) DO UPDATE
                   SET testset_version = EXCLUDED.testset_version, verdict = EXCLUDED.verdict,
                       rejudge_seq = EXCLUDED.rejudge_seq, judged_at = EXCLUDED.judged_at, applied_at = now()
                 WHERE results.rejudge_seq < EXCLUDED.rejudge_seq
                """)
                .param("submissionId", e.submissionId())
                .param("userId", e.userId())
                .param("problemId", e.problemId())
                .param("testsetVersion", e.testsetVersion())
                .param("verdict", e.verdict())
                .param("rejudgeSeq", e.rejudgeSeq())
                .param("judgedAt", Timestamp.from(e.judgedAt()))
                .update();
    }

    /** Recomputes whether the user has solved the problem, from all their stored results for it. */
    public void recomputeStatus(String userId, UUID problemId) {
        jdbc.sql("""
                INSERT INTO user_problem_status (user_id, problem_id, solved, first_solved_at)
                SELECT :userId, :problemId,
                       COALESCE(bool_or(verdict = 'accepted'), false),
                       min(judged_at) FILTER (WHERE verdict = 'accepted')
                  FROM results
                 WHERE user_id = :userId AND problem_id = :problemId
                ON CONFLICT (user_id, problem_id) DO UPDATE
                   SET solved = EXCLUDED.solved, first_solved_at = EXCLUDED.first_solved_at
                """)
                .param("userId", userId)
                .param("problemId", problemId)
                .update();
    }

    /** Recomputes the user's score from their solved problems; a user with none gets a zero row. */
    public void recomputeScore(String userId) {
        jdbc.sql("""
                INSERT INTO user_scores (user_id, score, solved_count, last_solved_at, updated_at)
                SELECT :userId, COALESCE(sum(p.points), 0), count(*), max(s.first_solved_at), now()
                  FROM user_problem_status s
                  JOIN problems p ON p.id = s.problem_id
                 WHERE s.user_id = :userId AND s.solved
                ON CONFLICT (user_id) DO UPDATE
                   SET score = EXCLUDED.score, solved_count = EXCLUDED.solved_count,
                       last_solved_at = EXCLUDED.last_solved_at, updated_at = EXCLUDED.updated_at
                """)
                .param("userId", userId)
                .update();
    }

    /** Only published problems can be judged, so only they may score. */
    public boolean problemPublished(UUID problemId) {
        return jdbc.sql("SELECT EXISTS (SELECT 1 FROM problems WHERE id = :id AND status = 'published')")
                .param("id", problemId)
                .query(Boolean.class)
                .single();
    }

    // ---- reads ----

    /** The most recently applied verdict: when it was judged and when it reached the leaderboard. */
    public record Freshness(Instant judgedAt, Instant appliedAt) {}

    public Optional<Freshness> latestApplied() {
        return jdbc.sql("SELECT judged_at, applied_at FROM results ORDER BY applied_at DESC LIMIT 1")
                .query((rs, rowNum) -> new Freshness(
                        rs.getTimestamp("judged_at").toInstant(),
                        rs.getTimestamp("applied_at").toInstant()))
                .optional();
    }

    public record Ranked(
            int rank, String userId, @Nullable String username, @Nullable String displayName, int score, int solvedCount) {}

    /**
     * Users with at least one solve, best first. Equal score: whoever reached it earlier ranks higher; users
     * equal on both share a rank. user_id only makes the listing order stable. Disabled accounts are left out
     * (their scores stay stored); names are null for a user Catalog hasn't heard about yet.
     */
    public List<Ranked> top(int limit) {
        return jdbc.sql("""
                SELECT s.user_id, p.username, p.display_name, s.score, s.solved_count,
                       RANK() OVER (ORDER BY s.score DESC, s.last_solved_at ASC) AS rank
                  FROM user_scores s
                  LEFT JOIN user_profiles p ON p.user_id = s.user_id
                 WHERE s.solved_count > 0
                   AND p.status IS DISTINCT FROM 'disabled'
                 ORDER BY s.score DESC, s.last_solved_at ASC, s.user_id ASC
                 LIMIT :limit
                """)
                .param("limit", limit)
                .query((rs, rowNum) -> new Ranked(
                        rs.getInt("rank"), rs.getString("user_id"), rs.getString("username"),
                        rs.getString("display_name"), rs.getInt("score"), rs.getInt("solved_count")))
                .list();
    }

    public record Standing(
            String userId, @Nullable String username, @Nullable String displayName, @Nullable Integer rank, int score,
            int solvedCount) {}

    /**
     * One user's score and rank, or empty if no verdict was ever received. The rank is null until their first solve,
     * and for a disabled account, which the leaderboard leaves out; disabled accounts never count against others.
     */
    public Optional<Standing> standing(String userId) {
        return jdbc.sql("""
                SELECT u.user_id, p.username, p.display_name, u.score, u.solved_count,
                       CASE WHEN u.solved_count = 0 OR p.status = 'disabled' THEN NULL ELSE
                         (1 + (SELECT count(*) FROM user_scores o
                                 LEFT JOIN user_profiles op ON op.user_id = o.user_id
                                WHERE o.solved_count > 0
                                  AND op.status IS DISTINCT FROM 'disabled'
                                  AND (o.score > u.score OR (o.score = u.score AND o.last_solved_at < u.last_solved_at))))::int
                       END AS rank
                  FROM user_scores u
                  LEFT JOIN user_profiles p ON p.user_id = u.user_id
                 WHERE u.user_id = :userId
                """)
                .param("userId", userId)
                .query((rs, rowNum) -> new Standing(
                        rs.getString("user_id"), rs.getString("username"), rs.getString("display_name"),
                        rs.getObject("rank", Integer.class), rs.getInt("score"), rs.getInt("solved_count")))
                .optional();
    }

    /** The problems the user has solved, in the order they were first solved. */
    public List<UUID> solvedProblemIds(String userId) {
        return jdbc.sql("""
                SELECT problem_id FROM user_problem_status
                 WHERE user_id = :userId AND solved
                 ORDER BY first_solved_at, problem_id
                """)
                .param("userId", userId)
                .query(UUID.class)
                .list();
    }
}
