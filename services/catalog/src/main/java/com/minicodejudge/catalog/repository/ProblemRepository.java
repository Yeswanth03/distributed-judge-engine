package com.minicodejudge.catalog.repository;

import com.minicodejudge.catalog.dto.ProblemDtos.CreateProblemRequest;
import java.util.UUID;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** SQL for the problems table. No rules live here. */
@Repository
public class ProblemRepository {

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
}
