package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.dto.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.repository.ProblemRepository;
import java.util.UUID;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Service;

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
}
