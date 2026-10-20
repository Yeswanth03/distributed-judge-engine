package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.ApiError;
import com.minicodejudge.catalog.dto.TestsetDtos.TestsetCreated;
import com.minicodejudge.catalog.dto.TestsetDtos.TestsetRequest;
import com.minicodejudge.catalog.service.TestsetService;
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
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/** Admin writes for a problem's test sets. The admin role check lives in AuthInterceptor. */
@RestController
@RequestMapping("/admin/problems/{id}/testsets")
@Tag(name = "Admin: test sets",
        description = "Upload versioned test cases and publish them. Needs X-User-Role: admin.")
public class AdminTestsetController {

    private final TestsetService service;

    public AdminTestsetController(TestsetService service) {
        this.service = service;
    }

    @PostMapping
    @Operation(summary = "Upload a test set",
            description = "Stores the cases as the next draft version (1, 2, 3, ...). At least one case must have "
                    + "is_sample = true; is_sample defaults to false (hidden). Up to 200 cases, each input and "
                    + "expected_output at most 65,536 characters. Uploading to a published problem is allowed: "
                    + "that is how test data is fixed.")
    @ApiResponse(responseCode = "201", description = "Created; returns the new version and its content hash")
    @ApiResponse(responseCode = "400", description = "Malformed JSON body or id is not a UUID", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"malformed request body\"}")))
    @ApiResponse(responseCode = "403", description = "Missing admin role", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"forbidden\",\"message\":\"admin role required\"}")))
    @ApiResponse(responseCode = "404", description = "No problem with this id", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"not_found\",\"message\":\"problem not found\"}")))
    @ApiResponse(responseCode = "422", description = "No sample case, no cases, or a case field breaks a rule",
            content = @Content(schema = @Schema(implementation = ApiError.class), examples = @ExampleObject(
                    value = "{\"error\":\"validation_failed\",\"message\":\"cases must include at least one sample case\"}")))
    public ResponseEntity<TestsetCreated> upload(
            @Parameter(description = "Problem id returned by create") @PathVariable("id") UUID id,
            @Valid @RequestBody TestsetRequest body) {
        return ResponseEntity.status(HttpStatus.CREATED).body(service.upload(id, body));
    }

    @PostMapping("/{version}/publish")
    @Operation(summary = "Publish a test set version",
            description = "Makes the version current and the problem published, in one transaction. A published "
                    + "version can never change. Versions only move forward: publishing one that is already "
                    + "published, or older than the current one, is a conflict.")
    @ApiResponse(responseCode = "204", description = "Published", content = @Content)
    @ApiResponse(responseCode = "400", description = "id is not a UUID or version is not a number", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"invalid value for version\"}")))
    @ApiResponse(responseCode = "403", description = "Missing admin role", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"forbidden\",\"message\":\"admin role required\"}")))
    @ApiResponse(responseCode = "404", description = "No such problem or version", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"not_found\",\"message\":\"test set version 7 not found\"}")))
    @ApiResponse(responseCode = "409", description = "Already published, or older than the current version",
            content = @Content(schema = @Schema(implementation = ApiError.class), examples = @ExampleObject(
                    value = "{\"error\":\"conflict\",\"message\":\"test set version 1 is already published\"}")))
    public ResponseEntity<Void> publish(
            @Parameter(description = "Problem id returned by create") @PathVariable("id") UUID id,
            @Parameter(description = "Version returned by upload", example = "1") @PathVariable("version") int version) {
        service.publish(id, version);
        return ResponseEntity.noContent().build();
    }
}
