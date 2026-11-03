package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.Valid;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotEmpty;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
import java.util.List;
import java.util.UUID;
import org.jspecify.annotations.Nullable;

/** Request and response shapes for problems. JSON field names are snake_case (see application.properties). */
public final class ProblemDtos {

    private static final String IDENTIFIER = "^[A-Za-z_][A-Za-z0-9_]*$";
    private static final String TYPE = "^(int|float|bool|string)(\\[\\]){0,2}$";

    private ProblemDtos() {}

    public record FunctionParameter(
            @Schema(example = "nums")
            @NotBlank @Size(max = 100)
            @Pattern(regexp = IDENTIFIER, message = "must be a valid parameter name") String name,
            @Schema(description = "int, float, bool or string, optionally followed by [] or [][]", example = "int[]")
            @NotBlank @Pattern(regexp = TYPE, message = "must be a supported type") String type) {}

    /** What the judge calls and what users start from. Every test case is written against these. */
    public record FunctionDetails(
            @Schema(example = "twoSum") String functionName,
            List<FunctionParameter> parameters,
            @Schema(example = "int[]") String returnType,
            String starterCode) {}

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
            @NotNull @Min(16) @Max(1024) Integer memoryLimitMb,
            @Schema(description = "Method of class Solution the judge calls; locked once published", example = "twoSum")
            @NotBlank @Size(max = 100)
            @Pattern(regexp = IDENTIFIER, message = "must be a valid function name") String functionName,
            @Schema(description = "The function's arguments, in order; locked once published")
            @NotEmpty @Size(max = 10) List<@NotNull @Valid FunctionParameter> parameters,
            @Schema(description = "One of the types listed for parameters; locked once published", example = "int[]")
            @NotBlank @Pattern(regexp = TYPE, message = "must be a supported type") String returnType,
            @Schema(description = "Python shown to users; must define class Solution and the function; locked once published",
                    example = "class Solution:\n    def twoSum(self, nums: list[int], target: int) -> list[int]:\n        pass\n")
            @NotBlank @Size(max = 65_536) String starterCode) {

        @Schema(hidden = true)
        public FunctionDetails functionDetails() {
            return new FunctionDetails(functionName, parameters, returnType, starterCode);
        }
    }

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
            @NotNull @Min(16) @Max(1024) Integer memoryLimitMb,
            @Schema(description = "Method of class Solution the judge calls; locked once published", example = "twoSum")
            @NotBlank @Size(max = 100)
            @Pattern(regexp = IDENTIFIER, message = "must be a valid function name") String functionName,
            @Schema(description = "The function's arguments, in order; locked once published")
            @NotEmpty @Size(max = 10) List<@NotNull @Valid FunctionParameter> parameters,
            @Schema(description = "One of the types listed for parameters; locked once published", example = "int[]")
            @NotBlank @Pattern(regexp = TYPE, message = "must be a supported type") String returnType,
            @Schema(description = "Python shown to users; must define class Solution and the function; locked once published",
                    example = "class Solution:\n    def twoSum(self, nums: list[int], target: int) -> list[int]:\n        pass\n")
            @NotBlank @Size(max = 65_536) String starterCode) {

        @Schema(hidden = true)
        public FunctionDetails functionDetails() {
            return new FunctionDetails(functionName, parameters, returnType, starterCode);
        }
    }

    public record ProblemCreated(
            @Schema(description = "Problem id used in every other call and by other services") UUID id) {}

    // ---- public reads ----

    /** A visible example: only cases marked is_sample ever appear here. */
    public record Sample(
            @Schema(description = "JSON object of the function's arguments", example = "{\"nums\":[2,7,11,15],\"target\":9}")
            String input,
            @Schema(description = "JSON value the function must return", example = "[0,1]") String expectedOutput) {}

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
            @Schema(description = "Python to start from; null for a problem without function details")
            @Nullable String starterCode,
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
