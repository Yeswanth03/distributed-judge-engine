package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.dto.AdminDtos.AdminProblemPage;
import com.minicodejudge.catalog.dto.AdminDtos.AdminProblemSummary;
import com.minicodejudge.catalog.dto.AdminDtos.AdminProblemView;
import com.minicodejudge.catalog.dto.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.dto.ProblemDtos.ProblemPage;
import com.minicodejudge.catalog.dto.ProblemDtos.ProblemSummary;
import com.minicodejudge.catalog.dto.ProblemDtos.ProblemView;
import com.minicodejudge.catalog.dto.ProblemDtos.Sample;
import com.minicodejudge.catalog.dto.ProblemDtos.UpdateProblemRequest;
import com.minicodejudge.catalog.repository.ProblemRepository;
import com.minicodejudge.catalog.repository.ProblemRepository.LockedFields;
import com.minicodejudge.catalog.repository.ProblemRepository.PublishedProblem;
import com.minicodejudge.catalog.repository.TestsetRepository;
import java.util.List;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/** Problem rules. Controllers call this; this calls the repository. */
@Service
public class ProblemService {

    private final ProblemRepository repo;
    private final TestsetRepository testsets;

    public ProblemService(ProblemRepository repo, TestsetRepository testsets) {
        this.repo = repo;
        this.testsets = testsets;
    }

    /** New problems start as drafts (the column default); the UUID is the id other services use. */
    public UUID createProblem(CreateProblemRequest request) {
        UUID id = UUID.randomUUID();
        try {
            repo.insert(id, request);
        } catch (DuplicateKeyException e) {
            // The UNIQUE constraint on slug decides, so two concurrent creates cannot both win.
            throw new ConflictException("a problem with slug '" + request.slug() + "' already exists");
        }
        return id;
    }

    /**
     * Replaces the editable fields. Once a problem is published, difficulty, points and limits are locked because
     * verdicts and scores already depend on them; resending the same values is fine, changing them is a conflict.
     */
    @Transactional
    public void updateProblem(UUID id, UpdateProblemRequest request) {
        LockedFields current = repo.lockById(id).orElseThrow(() -> new NotFoundException("problem not found"));

        boolean lockedFieldChanged = !request.difficulty().equals(current.difficulty())
                || request.points() != current.points()
                || request.timeLimitMs() != current.timeLimitMs()
                || request.memoryLimitMb() != current.memoryLimitMb();
        if ("published".equals(current.status()) && lockedFieldChanged) {
            throw new ConflictException(
                    "difficulty, points and limits are locked once a problem is published; only title and statement can change");
        }
        repo.update(id, request);
    }

    // ---- public reads ----

    /**
     * A published problem with its sample cases. Drafts and unknown slugs give the same 404, so a draft's existence
     * never leaks. No transaction is needed: the version read here is published and therefore never changes, so the
     * samples always belong to it even if a newer version is published between the two queries.
     */
    public ProblemView getProblem(String slug) {
        PublishedProblem p = repo.findPublishedBySlug(slug)
                .orElseThrow(() -> new NotFoundException("problem not found"));
        List<Sample> samples = testsets.findSamples(p.id(), p.currentVersion());
        return new ProblemView(p.id(), p.slug(), p.title(), p.statementMd(), p.difficulty(), p.points(),
                p.timeLimitMs(), p.memoryLimitMb(), samples);
    }

    /** One page of published problems in slug order. Fetches one extra row to know whether another page exists. */
    public ProblemPage listProblems(@Nullable String difficulty, int limit, @Nullable String cursor) {
        List<ProblemSummary> rows = repo.listPublished(difficulty, cursor, limit + 1);
        if (rows.size() <= limit) {
            return new ProblemPage(rows, null);
        }
        List<ProblemSummary> page = List.copyOf(rows.subList(0, limit));
        return new ProblemPage(page, page.getLast().slug());
    }

    // ---- admin reads (drafts included) ----

    /** One page of all problems, drafts included, in slug order; optionally only one status. */
    public AdminProblemPage listForAdmin(@Nullable String status, int limit, @Nullable String cursor) {
        List<AdminProblemSummary> rows = repo.listAll(status, cursor, limit + 1);
        if (rows.size() <= limit) {
            return new AdminProblemPage(rows, null);
        }
        List<AdminProblemSummary> page = List.copyOf(rows.subList(0, limit));
        return new AdminProblemPage(page, page.getLast().slug());
    }

    public AdminProblemView getForAdmin(UUID id) {
        return repo.findForAdmin(id).orElseThrow(() -> new NotFoundException("problem not found"));
    }
}
