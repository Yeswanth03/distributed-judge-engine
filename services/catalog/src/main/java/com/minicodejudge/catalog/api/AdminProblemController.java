package com.minicodejudge.catalog.api;

import com.minicodejudge.catalog.api.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.api.ProblemDtos.ProblemCreated;
import com.minicodejudge.catalog.service.ProblemService;
import jakarta.validation.Valid;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/** Admin writes for problems. The admin role check lives in AuthInterceptor. */
@RestController
@RequestMapping("/admin/problems")
public class AdminProblemController {

    private final ProblemService service;

    public AdminProblemController(ProblemService service) {
        this.service = service;
    }

    @PostMapping
    public ResponseEntity<ProblemCreated> create(@Valid @RequestBody CreateProblemRequest body) {
        UUID id = service.createProblem(body);
        return ResponseEntity.status(HttpStatus.CREATED).body(new ProblemCreated(id));
    }
}
