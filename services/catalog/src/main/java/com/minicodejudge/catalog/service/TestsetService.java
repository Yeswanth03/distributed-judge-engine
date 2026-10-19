package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.dto.TestsetDtos.CaseRequest;
import com.minicodejudge.catalog.dto.TestsetDtos.TestsetCreated;
import com.minicodejudge.catalog.dto.TestsetDtos.TestsetRequest;
import com.minicodejudge.catalog.repository.ProblemRepository;
import com.minicodejudge.catalog.repository.ProblemRepository.LockedFields;
import com.minicodejudge.catalog.repository.TestsetRepository;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;
import java.util.List;
import java.util.UUID;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/** Test set rules: versions are uploaded as drafts, published once, and only ever move forward. */
@Service
public class TestsetService {

    private final ProblemRepository problems;
    private final TestsetRepository testsets;

    public TestsetService(ProblemRepository problems, TestsetRepository testsets) {
        this.problems = problems;
        this.testsets = testsets;
    }

    /** Stores the cases as the next draft version, all or nothing. The problem's row lock serialises uploads. */
    @Transactional
    public TestsetCreated upload(UUID problemId, TestsetRequest request) {
        problems.lockById(problemId).orElseThrow(() -> new NotFoundException("problem not found"));
        if (request.cases().stream().noneMatch(CaseRequest::sample)) {
            throw new UnprocessableException("cases must include at least one sample case");
        }

        int version = testsets.nextVersion(problemId);
        String hash = contentHash(request.cases());
        testsets.insertTestset(problemId, version, hash);
        int ordinal = 1;
        for (CaseRequest c : request.cases()) {
            testsets.insertCase(problemId, version, ordinal++, c);
        }
        return new TestsetCreated(version, hash);
    }

    /**
     * Publishes a draft version and points the problem at it, in one transaction so readers never see half of it.
     * Versions only move forward: publishing one at or below the current version is a conflict.
     */
    @Transactional
    public void publish(UUID problemId, int version) {
        LockedFields problem = problems.lockById(problemId)
                .orElseThrow(() -> new NotFoundException("problem not found"));
        String status = testsets.findStatus(problemId, version)
                .orElseThrow(() -> new NotFoundException("test set version " + version + " not found"));

        if ("published".equals(status)) {
            throw new ConflictException("test set version " + version + " is already published");
        }
        Integer current = problem.currentVersion();
        if (current != null && version <= current) {
            throw new ConflictException(
                    "version " + version + " is older than the published version " + current + "; upload a new version instead");
        }
        testsets.markPublished(problemId, version);
        problems.markPublished(problemId, version);
    }

    /**
     * SHA-256 over every case in order. Each field is length-prefixed, so different splits of the same text
     * (e.g. input "ab" + output "c" versus "a" + "bc") can never produce the same hash.
     */
    static String contentHash(List<CaseRequest> cases) {
        try {
            MessageDigest md = MessageDigest.getInstance("SHA-256");
            for (CaseRequest c : cases) {
                field(md, c.input());
                field(md, c.expectedOutput());
                md.update((byte) (c.sample() ? 1 : 0));
            }
            return HexFormat.of().formatHex(md.digest());
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 is required by every JVM", e);
        }
    }

    private static void field(MessageDigest md, String value) {
        byte[] bytes = value.getBytes(StandardCharsets.UTF_8);
        md.update(ByteBuffer.allocate(Integer.BYTES).putInt(bytes.length).array());
        md.update(bytes);
    }
}
