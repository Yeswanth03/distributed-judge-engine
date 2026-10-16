package com.minicodejudge.catalog.dto;

import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
import java.util.UUID;

/** Request and response shapes for problems. JSON field names are snake_case (see application.yml). */
public final class ProblemDtos {

    private ProblemDtos() {}

    public record CreateProblemRequest(
            @NotBlank @Size(max = 80)
            @Pattern(regexp = "^[a-z0-9]+(-[a-z0-9]+)*$", message = "must be lowercase words joined by hyphens")
            String slug,
            @NotBlank @Size(max = 200) String title,
            @NotBlank String statementMd,
            @NotBlank @Pattern(regexp = "easy|medium|hard", message = "must be easy, medium or hard") String difficulty,
            // Boxed so a missing number is reported as a named validation error, not an unreadable body.
            @NotNull @Min(1) @Max(1000) Integer points,
            @NotNull @Min(100) @Max(60000) Integer timeLimitMs,
            @NotNull @Min(16) @Max(1024) Integer memoryLimitMb) {}

    public record ProblemCreated(UUID id) {}
}
