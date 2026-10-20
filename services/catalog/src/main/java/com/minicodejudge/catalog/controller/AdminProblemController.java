package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.ApiError;
import com.minicodejudge.catalog.dto.ProblemDtos.CreateProblemRequest;
import com.minicodejudge.catalog.dto.ProblemDtos.ProblemCreated;
import com.minicodejudge.catalog.dto.ProblemDtos.UpdateProblemRequest;
import com.minicodejudge.catalog.service.ProblemService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.Parameter;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.ExampleObject;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.validation.Valid;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/** Admin writes for problems. The admin role check lives in AuthInterceptor. */
@RestController
@RequestMapping("/admin/problems")
@Tag(name = "Admin: problems", description = "Create and edit problems. Needs X-User-Role: admin.")
public class AdminProblemController {

    private final ProblemService service;

    public AdminProblemController(ProblemService service) {
        this.service = service;
    }

    @PostMapping
    @Operation(summary = "Create a problem",
            description = "Stores a new problem as a draft. It stays hidden until a test set version is published.")
    @ApiResponse(responseCode = "201", description = "Created; returns the problem id")
    @ApiResponse(responseCode = "400", description = "Malformed JSON body", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"malformed request body\"}")))
    @ApiResponse(responseCode = "403", description = "Missing admin role", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"forbidden\",\"message\":\"admin role required\"}")))
    @ApiResponse(responseCode = "409", description = "Slug already taken", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(
                    value = "{\"error\":\"conflict\",\"message\":\"a problem with slug 'two-sum' already exists\"}")))
    @ApiResponse(responseCode = "422", description = "A field breaks a rule", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(
                    value = "{\"error\":\"validation_failed\",\"message\":\"difficulty must be easy, medium or hard\"}")))
    public ResponseEntity<ProblemCreated> create(@Valid @RequestBody CreateProblemRequest body) {
        UUID id = service.createProblem(body);
        return ResponseEntity.status(HttpStatus.CREATED).body(new ProblemCreated(id));
    }

    @PutMapping("/{id}")
    @Operation(summary = "Replace a problem's editable fields",
            description = "Send every field. The slug cannot change. Once the problem is published, difficulty, "
                    + "points and limits are locked (resending the same values is fine); title and statement can change.")
    @ApiResponse(responseCode = "204", description = "Updated", content = @Content)
    @ApiResponse(responseCode = "400", description = "Malformed JSON body or id is not a UUID", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"invalid value for id\"}")))
    @ApiResponse(responseCode = "403", description = "Missing admin role", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"forbidden\",\"message\":\"admin role required\"}")))
    @ApiResponse(responseCode = "404", description = "No problem with this id", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"not_found\",\"message\":\"problem not found\"}")))
    @ApiResponse(responseCode = "409", description = "Locked field changed on a published problem", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"conflict\",\"message\":\"difficulty, points and limits "
                    + "are locked once a problem is published; only title and statement can change\"}")))
    @ApiResponse(responseCode = "422", description = "A field breaks a rule", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"validation_failed\",\"message\":\"points must be less "
                    + "than or equal to 1000\"}")))
    public ResponseEntity<Void> update(
            @Parameter(description = "Problem id returned by create") @PathVariable("id") UUID id,
            @Valid @RequestBody UpdateProblemRequest body) {
        service.updateProblem(id, body);
        return ResponseEntity.noContent().build();
    }
}
