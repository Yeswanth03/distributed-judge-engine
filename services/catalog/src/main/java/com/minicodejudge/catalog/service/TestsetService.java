package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.dto.AdminDtos.TestsetVersion;
import com.minicodejudge.catalog.dto.InternalDtos.InternalCase;
import com.minicodejudge.catalog.dto.InternalDtos.InternalTestset;
import com.minicodejudge.catalog.dto.ProblemDtos.FunctionDetails;
import com.minicodejudge.catalog.dto.TestsetDtos.CaseRequest;
import com.minicodejudge.catalog.dto.TestsetDtos.TestsetCreated;
import com.minicodejudge.catalog.dto.TestsetDtos.TestsetRequest;
import com.minicodejudge.catalog.repository.ProblemRepository;
import com.minicodejudge.catalog.repository.ProblemRepository.JudgeLimits;
import com.minicodejudge.catalog.repository.ProblemRepository.LockedFields;
import com.minicodejudge.catalog.repository.TestsetRepository;
import com.minicodejudge.catalog.repository.TestsetRepository.TestsetInfo;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
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

    /**
     * Checks the cases against the problem's function and stores them as the next draft version, all or nothing.
     * The problem's row lock serialises uploads.
     */
    @Transactional
    public TestsetCreated upload(UUID problemId, TestsetRequest request) {
        LockedFields problem = problems.lockById(problemId)
                .orElseThrow(() -> new NotFoundException("problem not found"));
        FunctionDetails function = requireFunction(problem);
        if (request.cases().stream().noneMatch(CaseRequest::sample)) {
            throw new UnprocessableException("cases must include at least one sample case");
        }

        List<CaseRequest> cases = new ArrayList<>(request.cases().size());
        for (CaseRequest c : request.cases()) {
            cases.add(FunctionRules.canonical(function, c, cases.size()));
        }

        int version = testsets.nextVersion(problemId);
        String hash = contentHash(cases);
        testsets.insertTestset(problemId, version, hash);
        int ordinal = 1;
        for (CaseRequest c : cases) {
            testsets.insertCase(problemId, version, ordinal++, c);
        }
        return new TestsetCreated(version, hash);
    }

    /**
     * Publishes a draft version and points the problem at it, in one transaction so readers never see half of it.
     * Versions only move forward: publishing one at or below the current version is a conflict. The cases are checked
     * again, because a draft problem's function can change after they were uploaded.
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
        FunctionDetails function = requireFunction(problem);
        List<InternalCase> cases = testsets.findAllCases(problemId, version);
        for (int i = 0; i < cases.size(); i++) {
            FunctionRules.check(function, cases.get(i).input(), cases.get(i).expectedOutput(), i);
        }
        testsets.markPublished(problemId, version);
        problems.markPublished(problemId, version);
    }

    /** Every version of a problem's test set, newest first, drafts included, marking the current one. */
    public List<TestsetVersion> listVersions(UUID problemId) {
        JudgeLimits problem = problems.findJudgeLimits(problemId)
                .orElseThrow(() -> new NotFoundException("problem not found"));
        return testsets.listVersions(problemId, problem.currentVersion());
    }

    /**
     * Every case of one published version plus the limits, for a judge worker. Without a version, the problem's
     * current one is used. Drafts are never served: the judge only runs data that can no longer change.
     * No transaction is needed: the version resolved here is published, so its cases are immutable.
     */
    public InternalTestset forJudge(UUID problemId, @Nullable Integer requestedVersion) {
        JudgeLimits problem = problems.findJudgeLimits(problemId)
                .orElseThrow(() -> new NotFoundException("problem not found"));
        Integer version = requestedVersion != null ? requestedVersion : problem.currentVersion();
        if (version == null) {
            throw new NotFoundException("problem has no published test set");
        }
        TestsetInfo info = testsets.findInfo(problemId, version)
                .orElseThrow(() -> new NotFoundException("test set version " + version + " not found"));
        if (!"published".equals(info.status())) {
            throw new NotFoundException("test set version " + version + " is not published");
        }
        return new InternalTestset(problemId, version, info.contentHash(), problem.timeLimitMs(),
                problem.memoryLimitMb(),
                problem.function() == null ? null : problem.function().functionName(),
                problem.function() == null ? null : problem.function().parameters(),
                problem.function() == null ? null : problem.function().returnType(),
                testsets.findAllCases(problemId, version));
    }

    private static FunctionDetails requireFunction(LockedFields problem) {
        if (problem.function() == null) {
            throw new UnprocessableException("add the problem's function details before uploading or publishing test cases");
        }
        return problem.function();
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
