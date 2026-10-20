package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotEmpty;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;
import java.util.List;

/** Request and response shapes for test sets. JSON field names are snake_case (see application.properties). */
public final class TestsetDtos {

    /** Per-field cap, so one upload cannot push huge inputs into the judge workers' memory. */
    public static final int MAX_CASE_FIELD_CHARS = 65_536;

    private TestsetDtos() {}

    /**
     * One test case. Input and expected output may be empty (a program can print nothing) but not missing.
     * is_sample defaults to false: a case is hidden unless marked otherwise, so forgetting the flag never exposes it.
     */
    public record CaseRequest(
            @Schema(description = "Exactly what the program reads on stdin; may be empty", example = "2 7 11 15\n9")
            @NotNull @Size(max = MAX_CASE_FIELD_CHARS) String input,
            @Schema(description = "Exactly what the program must print; may be empty", example = "0 1")
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
