package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.dto.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.dto.ProblemDtos.UpdateProblemRequest;
import com.minicodejudge.catalog.repository.ProblemRepository;
import com.minicodejudge.catalog.repository.ProblemRepository.LockedFields;
import java.util.UUID;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/** Problem rules. Controllers call this; this calls the repository. */
@Service
public class ProblemService {

    private final ProblemRepository repo;

    public ProblemService(ProblemRepository repo) {
        this.repo = repo;
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
}
