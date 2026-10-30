package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.AdminDtos.AdminProblemPage;
import com.minicodejudge.catalog.dto.AdminDtos.AdminProblemView;
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
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.Pattern;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/** Admin reads and writes for problems, drafts included. The admin role check lives in AuthInterceptor. */
@RestController
@RequestMapping("/admin/problems")
@Tag(name = "Admin: problems", description = "List, view, create and edit problems, drafts included. Needs X-User-Role: admin.")
public class AdminProblemController {

    private final ProblemService service;

    public AdminProblemController(ProblemService service) {
        this.service = service;
    }

    @GetMapping
    @Operation(summary = "List all problems",
            description = "Drafts included, with status and current version, sorted by slug. Pass next_cursor as "
                    + "cursor for the next page.")
    @ApiResponse(responseCode = "200", description = "One page of problems")
    @ApiResponse(responseCode = "400", description = "Invalid status or limit", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"status must be draft or published\"}")))
    @ApiResponse(responseCode = "403", description = "Missing admin role", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"forbidden\",\"message\":\"admin role required\"}")))
    public AdminProblemPage list(
            @Parameter(description = "Only problems with this status", schema = @Schema(allowableValues = {"draft", "published"}))
            @RequestParam(name = "status", required = false)
            @Pattern(regexp = "draft|published", message = "must be draft or published") @Nullable String status,
            @Parameter(description = "Page size, 1 to 100")
            @RequestParam(name = "limit", defaultValue = "50") @Min(1) @Max(100) int limit,
            @Parameter(description = "next_cursor from the previous page; omit for the first page")
            @RequestParam(name = "cursor", required = false) @Nullable String cursor) {
        return service.listForAdmin(status, limit, cursor);
    }

    @GetMapping("/{id}")
    @Operation(summary = "Get any problem", description = "Every field, including status and the current version, for drafts too.")
    @ApiResponse(responseCode = "200", description = "The problem")
    @ApiResponse(responseCode = "400", description = "id is not a UUID", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"invalid value for id\"}")))
    @ApiResponse(responseCode = "403", description = "Missing admin role", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"forbidden\",\"message\":\"admin role required\"}")))
    @ApiResponse(responseCode = "404", description = "No problem with this id", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"not_found\",\"message\":\"problem not found\"}")))
    public AdminProblemView get(@Parameter(description = "Problem id returned by create") @PathVariable("id") UUID id) {
        return service.getForAdmin(id);
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
