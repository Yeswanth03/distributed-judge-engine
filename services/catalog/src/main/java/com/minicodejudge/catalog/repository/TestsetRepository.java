package com.minicodejudge.catalog.repository;

import com.minicodejudge.catalog.dto.ProblemDtos.Sample;
import com.minicodejudge.catalog.dto.TestsetDtos.CaseRequest;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** SQL for the testsets and test_cases tables. No rules live here. */
@Repository
public class TestsetRepository {

    private final JdbcClient jdbc;

    public TestsetRepository(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    /** Call while holding the problem's row lock, so two uploads cannot both get the same number. */
    public int nextVersion(UUID problemId) {
        return jdbc.sql("SELECT COALESCE(MAX(version), 0) + 1 FROM testsets WHERE problem_id = :problemId")
                .param("problemId", problemId)
                .query(Integer.class)
                .single();
    }

    public void insertTestset(UUID problemId, int version, String contentHash) {
        jdbc.sql("INSERT INTO testsets (problem_id, version, content_hash) VALUES (:problemId, :version, :hash)")
                .param("problemId", problemId)
                .param("version", version)
                .param("hash", contentHash)
                .update();
    }

    public void insertCase(UUID problemId, int version, int ordinal, CaseRequest c) {
        jdbc.sql("""
                INSERT INTO test_cases (id, problem_id, testset_version, ordinal, input, expected_output, is_sample)
                VALUES (:id, :problemId, :version, :ordinal, :input, :expectedOutput, :sample)
                """)
                .param("id", UUID.randomUUID())
                .param("problemId", problemId)
                .param("version", version)
                .param("ordinal", ordinal)
                .param("input", c.input())
                .param("expectedOutput", c.expectedOutput())
                .param("sample", c.sample())
                .update();
    }

    /** The status of one version, or empty if the problem has no such version. */
    public Optional<String> findStatus(UUID problemId, int version) {
        return jdbc.sql("SELECT status FROM testsets WHERE problem_id = :problemId AND version = :version")
                .param("problemId", problemId)
                .param("version", version)
                .query(String.class)
                .optional();
    }

    /** The sample cases of one version, in judging order. Hidden cases are never selected here. */
    public List<Sample> findSamples(UUID problemId, int version) {
        return jdbc.sql("""
                SELECT input, expected_output FROM test_cases
                 WHERE problem_id = :problemId AND testset_version = :version AND is_sample
                 ORDER BY ordinal
                """)
                .param("problemId", problemId)
                .param("version", version)
                .query((rs, rowNum) -> new Sample(rs.getString("input"), rs.getString("expected_output")))
                .list();
    }

    public void markPublished(UUID problemId, int version) {
        jdbc.sql("""
                UPDATE testsets SET status = 'published', published_at = now()
                 WHERE problem_id = :problemId AND version = :version
                """)
                .param("problemId", problemId)
                .param("version", version)
                .update();
    }
}
