package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.ApiError;
import com.minicodejudge.catalog.dto.ProblemDtos.ProblemPage;
import com.minicodejudge.catalog.dto.ProblemDtos.ProblemView;
import com.minicodejudge.catalog.service.ProblemService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.Parameter;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.ExampleObject;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.security.SecurityRequirements;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.Pattern;
import org.jspecify.annotations.Nullable;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/** Public reads, reached through the gateway. Only published problems and their sample cases are visible. */
@RestController
@RequestMapping("/problems")
@Tag(name = "Problems", description = "Public reads. No role needed; only published problems are visible.")
@SecurityRequirements // public: overrides the global admin-role requirement, so Swagger shows no padlock
public class ProblemController {

    private final ProblemService service;

    public ProblemController(ProblemService service) {
        this.service = service;
    }

    @GetMapping
    @Operation(summary = "List published problems",
            description = "Sorted by slug. Pass next_cursor from one page as cursor to get the next; "
                    + "next_cursor is null on the last page.")
    @ApiResponse(responseCode = "200", description = "One page of problems")
    @ApiResponse(responseCode = "400", description = "Invalid difficulty or limit", content = @Content(
            schema = @Schema(implementation = ApiError.class),
            examples = @ExampleObject(value = "{\"error\":\"bad_request\",\"message\":\"limit must be less than or equal to 100\"}")))
    public ProblemPage list(
            @Parameter(description = "Only problems of this difficulty", schema = @Schema(allowableValues = {"easy", "medium", "hard"}))
            @RequestParam(name = "difficulty", required = false)
            @Pattern(regexp = "easy|medium|hard", message = "must be easy, medium or hard") @Nullable String difficulty,
            @Parameter(description = "Page size, 1 to 100")
            @RequestParam(name = "limit", defaultValue = "20") @Min(1) @Max(100) int limit,
            @Parameter(description = "next_cursor from the previous page; omit for the first page")
            @RequestParam(name = "cursor", required = false) @Nullable String cursor) {
        return service.listProblems(difficulty, limit, cursor);
    }

    @GetMapping("/{slug}")
    @Operation(summary = "Get a published problem",
            description = "Statement, limits and sample cases of the published version. Hidden cases are never returned.")
    @ApiResponse(responseCode = "200", description = "The problem")
    @ApiResponse(responseCode = "404", description = "No published problem with this slug (drafts count as missing)",
            content = @Content(schema = @Schema(implementation = ApiError.class),
                    examples = @ExampleObject(value = "{\"error\":\"not_found\",\"message\":\"problem not found\"}")))
    public ProblemView get(@Parameter(example = "two-sum") @PathVariable("slug") String slug) {
        return service.getProblem(slug);
    }
}
