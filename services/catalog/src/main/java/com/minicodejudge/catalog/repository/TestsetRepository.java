package com.minicodejudge.catalog.repository;

import com.minicodejudge.catalog.dto.AdminDtos.TestsetVersion;
import com.minicodejudge.catalog.dto.InternalDtos.InternalCase;
import com.minicodejudge.catalog.dto.ProblemDtos.Sample;
import com.minicodejudge.catalog.dto.TestsetDtos.CaseRequest;
import java.sql.Timestamp;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
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

    /** Every version of a problem's test set, newest first, with case counts. */
    public List<TestsetVersion> listVersions(UUID problemId, @Nullable Integer currentVersion) {
        return jdbc.sql("""
                SELECT t.version, t.status, t.content_hash, t.created_at, t.published_at,
                       count(c.id)::int AS case_count,
                       (count(c.id) FILTER (WHERE c.is_sample))::int AS sample_count
                  FROM testsets t
                  LEFT JOIN test_cases c ON c.problem_id = t.problem_id AND c.testset_version = t.version
                 WHERE t.problem_id = :problemId
                 GROUP BY t.problem_id, t.version
                 ORDER BY t.version DESC
                """)
                .param("problemId", problemId)
                .query((rs, rowNum) -> {
                    int version = rs.getInt("version");
                    Timestamp publishedAt = rs.getTimestamp("published_at");
                    return new TestsetVersion(
                            version,
                            rs.getString("status"),
                            currentVersion != null && currentVersion == version,
                            rs.getString("content_hash"),
                            rs.getInt("case_count"),
                            rs.getInt("sample_count"),
                            rs.getTimestamp("created_at").toInstant(),
                            publishedAt == null ? null : publishedAt.toInstant());
                })
                .list();
    }

    /** The status and content hash of one version, or empty if the problem has no such version. */
    public record TestsetInfo(String status, String contentHash) {}

    public Optional<TestsetInfo> findInfo(UUID problemId, int version) {
        return jdbc.sql("SELECT status, content_hash FROM testsets WHERE problem_id = :problemId AND version = :version")
                .param("problemId", problemId)
                .param("version", version)
                .query((rs, rowNum) -> new TestsetInfo(rs.getString("status"), rs.getString("content_hash")))
                .optional();
    }

    /** Every case of one version, hidden and sample, in judging order. Only the internal route may expose these. */
    public List<InternalCase> findAllCases(UUID problemId, int version) {
        return jdbc.sql("""
                SELECT ordinal, input, expected_output, is_sample FROM test_cases
                 WHERE problem_id = :problemId AND testset_version = :version
                 ORDER BY ordinal
                """)
                .param("problemId", problemId)
                .param("version", version)
                .query((rs, rowNum) -> new InternalCase(
                        rs.getInt("ordinal"),
                        rs.getString("input"),
                        rs.getString("expected_output"),
                        rs.getBoolean("is_sample")))
                .list();
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
