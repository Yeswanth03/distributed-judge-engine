package com.minicodejudge.catalog.repository;

import com.minicodejudge.catalog.dto.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.dto.ProblemDtos.UpdateProblemRequest;
import java.util.Optional;
import java.util.UUID;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** SQL for the problems table. No rules live here. */
@Repository
public class ProblemRepository {

    /** The columns the update rules look at: status, plus the fields that lock once a problem is published. */
    public record LockedFields(String status, String difficulty, int points, int timeLimitMs, int memoryLimitMb) {}

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
                SELECT status, difficulty, points, time_limit_ms, memory_limit_mb
                  FROM problems
                 WHERE id = :id
                   FOR UPDATE
                """)
                .param("id", id)
                .query((rs, rowNum) -> new LockedFields(
                        rs.getString("status"),
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
}
