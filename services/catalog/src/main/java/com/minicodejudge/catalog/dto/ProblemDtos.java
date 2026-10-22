package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
import java.util.List;
import java.util.UUID;

/** Request and response shapes for problems. JSON field names are snake_case (see application.properties). */
public final class ProblemDtos {

    private ProblemDtos() {}

    public record CreateProblemRequest(
            @Schema(description = "URL name, set once and never changed: lowercase words joined by hyphens",
                    example = "two-sum")
            @NotBlank @Size(max = 80)
            @Pattern(regexp = "^[a-z0-9]+(-[a-z0-9]+)*$", message = "must be lowercase words joined by hyphens")
            String slug,
            @Schema(example = "Two Sum")
            @NotBlank @Size(max = 200) String title,
            @Schema(description = "Problem statement in Markdown",
                    example = "Given an array of integers and a target, return the indices of the two numbers that add up to the target.")
            @NotBlank String statementMd,
            @Schema(allowableValues = {"easy", "medium", "hard"}, example = "easy")
            @NotBlank @Pattern(regexp = "easy|medium|hard", message = "must be easy, medium or hard") String difficulty,
            // Boxed so a missing number is reported as a named validation error, not an unreadable body.
            @Schema(description = "Score for solving it; locked once published", example = "100")
            @NotNull @Min(1) @Max(1000) Integer points,
            @Schema(description = "CPU time limit per test case; locked once published", example = "1000")
            @NotNull @Min(100) @Max(60000) Integer timeLimitMs,
            @Schema(description = "Memory limit; locked once published", example = "256")
            @NotNull @Min(16) @Max(1024) Integer memoryLimitMb) {}

    /** Full replacement of the editable fields. The slug is set once at creation and is not part of an update. */
    public record UpdateProblemRequest(
            @Schema(example = "Two Sum")
            @NotBlank @Size(max = 200) String title,
            @Schema(description = "Problem statement in Markdown",
                    example = "Given an array of integers and a target, return the indices of the two numbers that add up to the target.")
            @NotBlank String statementMd,
            @Schema(description = "Locked once published", allowableValues = {"easy", "medium", "hard"}, example = "easy")
            @NotBlank @Pattern(regexp = "easy|medium|hard", message = "must be easy, medium or hard") String difficulty,
            @Schema(description = "Locked once published", example = "100")
            @NotNull @Min(1) @Max(1000) Integer points,
            @Schema(description = "Locked once published", example = "1000")
            @NotNull @Min(100) @Max(60000) Integer timeLimitMs,
            @Schema(description = "Locked once published", example = "256")
            @NotNull @Min(16) @Max(1024) Integer memoryLimitMb) {}

    public record ProblemCreated(
            @Schema(description = "Problem id used in every other call and by other services") UUID id) {}

    // ---- public reads ----

    /** A visible example: only cases marked is_sample ever appear here. */
    public record Sample(
            @Schema(example = "2 7 11 15\n9") String input,
            @Schema(example = "0 1") String expectedOutput) {}

    /** One published problem as users see it: no status, no version, no hidden cases. */
    public record ProblemView(
            @Schema(description = "Problem id; submissions refer to the problem by this") UUID id,
            @Schema(example = "two-sum") String slug,
            @Schema(example = "Two Sum") String title,
            @Schema(description = "Problem statement in Markdown") String statementMd,
            @Schema(allowableValues = {"easy", "medium", "hard"}, example = "easy") String difficulty,
            @Schema(example = "100") int points,
            @Schema(example = "1000") int timeLimitMs,
            @Schema(example = "256") int memoryLimitMb,
            @Schema(description = "Sample cases of the published version, in judging order") List<Sample> samples) {}

    /** One row of the problem list; no statement or samples, to keep pages light. */
    public record ProblemSummary(
            UUID id,
            @Schema(example = "two-sum") String slug,
            @Schema(example = "Two Sum") String title,
            @Schema(allowableValues = {"easy", "medium", "hard"}, example = "easy") String difficulty,
            @Schema(example = "100") int points) {}

    public record ProblemPage(
            @Schema(description = "Published problems, sorted by slug") List<ProblemSummary> items,
            @Schema(description = "Pass as cursor to get the next page; null on the last page", example = "two-sum")
            String nextCursor) {}
}
