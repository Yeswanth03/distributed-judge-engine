package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotEmpty;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;
import java.util.List;

/** Request and response shapes for test sets. JSON field names are snake_case (see application.properties). */
public final class TestsetDtos {

    /**
     * Per-field cap, so one upload cannot push huge inputs into the judge workers' memory. 4 MiB fits n = 2·10^5
     * numbers, the size at which an O(n²) brute force no longer fits a 1 s limit.
     */
    public static final int MAX_CASE_FIELD_CHARS = 4 * 1024 * 1024;

    private TestsetDtos() {}

    /**
     * One test case, as JSON text: input holds the function's arguments, expected_output its return value.
     * is_sample defaults to false: a case is hidden unless marked otherwise, so forgetting the flag never exposes it.
     */
    public record CaseRequest(
            @Schema(description = "JSON object with one value per parameter", example = "{\"nums\":[2,7,11,15],\"target\":9}")
            @NotNull @Size(max = MAX_CASE_FIELD_CHARS) String input,
            @Schema(description = "JSON value of the problem's return_type", example = "[0,1]")
            @NotNull @Size(max = MAX_CASE_FIELD_CHARS) String expectedOutput,
            // No defaultValue here: Swagger UI prefers it over the example, which would make the example body
            // all-hidden and fail the "at least one sample" rule. The default is stated in the description instead.
            @Schema(description = "true = shown on the problem page; false or absent (the default) = hidden from users",
                    example = "true")
            Boolean isSample) {

        @Schema(hidden = true)
        public boolean sample() {
            return Boolean.TRUE.equals(isSample);
        }
    }

    public record TestsetRequest(
            @Schema(description = "1 to 200 cases, judged in this order; at least one must be a sample")
            @NotEmpty @Size(max = 200) List<@NotNull @Valid CaseRequest> cases) {}

    public record TestsetCreated(
            @Schema(description = "The new draft version; pass it to publish", example = "1") int version,
            @Schema(description = "SHA-256 fingerprint of the cases; identical cases give an identical hash")
            String contentHash) {}
}
