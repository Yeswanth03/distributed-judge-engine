package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.api.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.repo.ProblemRepository;
import java.util.UUID;
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
        repo.insert(id, request);
        return id;
    }
}
