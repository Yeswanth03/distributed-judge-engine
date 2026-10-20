package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
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
}
