package com.minicodejudge.catalog.repository;

import com.minicodejudge.catalog.dto.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.dto.ProblemDtos.ProblemSummary;
import com.minicodejudge.catalog.dto.ProblemDtos.UpdateProblemRequest;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** SQL for the problems table. No rules live here. */
@Repository
public class ProblemRepository {

    /**
     * The columns the write rules look at: status and current version, plus the fields that lock once a problem
     * is published. currentVersion is null until the first publish.
     */
    public record LockedFields(
            String status, Integer currentVersion, String difficulty, int points, int timeLimitMs, int memoryLimitMb) {}

    private final JdbcClient jdbc;

    public ProblemRepository(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    public void insert(UUID id, CreateProblemRequest r) {
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb)
                VALUES (:id, :slug, :title, :statementMd, :difficulty, :points, :timeLimitMs, :memoryLimitMb)
                """)
                .param("id", id)
                .param("slug", r.slug())
                .param("title", r.title())
                .param("statementMd", r.statementMd())
                .param("difficulty", r.difficulty())
                .param("points", r.points())
                .param("timeLimitMs", r.timeLimitMs())
                .param("memoryLimitMb", r.memoryLimitMb())
                .update();
    }

    /** Row-locks the problem until the transaction ends, so concurrent writes to it run one after another. */
    public Optional<LockedFields> lockById(UUID id) {
        return jdbc.sql("""
                SELECT status, current_version, difficulty, points, time_limit_ms, memory_limit_mb
                  FROM problems
                 WHERE id = :id
                   FOR UPDATE
                """)
                .param("id", id)
                .query((rs, rowNum) -> new LockedFields(
                        rs.getString("status"),
                        rs.getObject("current_version", Integer.class),
                        rs.getString("difficulty"),
                        rs.getInt("points"),
                        rs.getInt("time_limit_ms"),
                        rs.getInt("memory_limit_mb")))
                .optional();
    }

    public void update(UUID id, UpdateProblemRequest r) {
        jdbc.sql("""
                UPDATE problems
                   SET title = :title, statement_md = :statementMd, difficulty = :difficulty, points = :points,
                       time_limit_ms = :timeLimitMs, memory_limit_mb = :memoryLimitMb, updated_at = now()
                 WHERE id = :id
                """)
                .param("id", id)
                .param("title", r.title())
                .param("statementMd", r.statementMd())
                .param("difficulty", r.difficulty())
                .param("points", r.points())
                .param("timeLimitMs", r.timeLimitMs())
                .param("memoryLimitMb", r.memoryLimitMb())
                .update();
    }

    /** Points the problem at a newly published test set version and makes it visible. */
    public void markPublished(UUID id, int version) {
        jdbc.sql("""
                UPDATE problems SET status = 'published', current_version = :version, updated_at = now()
                 WHERE id = :id
                """)
                .param("id", id)
                .param("version", version)
                .update();
    }

    // ---- public reads ----

    /** A published problem plus the version its samples come from (never null once published). */
    public record PublishedProblem(
            UUID id, String slug, String title, String statementMd, String difficulty,
            int points, int timeLimitMs, int memoryLimitMb, int currentVersion) {}

    public Optional<PublishedProblem> findPublishedBySlug(String slug) {
        return jdbc.sql("""
                SELECT id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb, current_version
                  FROM problems
                 WHERE slug = :slug AND status = 'published'
                """)
                .param("slug", slug)
                .query((rs, rowNum) -> new PublishedProblem(
                        rs.getObject("id", UUID.class),
                        rs.getString("slug"),
                        rs.getString("title"),
                        rs.getString("statement_md"),
                        rs.getString("difficulty"),
                        rs.getInt("points"),
                        rs.getInt("time_limit_ms"),
                        rs.getInt("memory_limit_mb"),
                        rs.getInt("current_version")))
                .optional();
    }

    /**
     * Published problems in slug order, starting after {@code afterSlug} (keyset paging: stable while problems
     * are added, and a single index range scan). Both filters are optional.
     */
    public List<ProblemSummary> listPublished(@Nullable String difficulty, @Nullable String afterSlug, int limit) {
        return jdbc.sql("""
                SELECT id, slug, title, difficulty, points
                  FROM problems
                 WHERE status = 'published'
                   AND (CAST(:difficulty AS TEXT) IS NULL OR difficulty = :difficulty)
                   AND (CAST(:after AS TEXT) IS NULL OR slug > :after)
                 ORDER BY slug
                 LIMIT :limit
                """)
                .param("difficulty", difficulty)
                .param("after", afterSlug)
                .param("limit", limit)
                .query((rs, rowNum) -> new ProblemSummary(
                        rs.getObject("id", UUID.class),
                        rs.getString("slug"),
                        rs.getString("title"),
                        rs.getString("difficulty"),
                        rs.getInt("points")))
                .list();
    }
}
