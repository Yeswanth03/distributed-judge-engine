package com.minicodejudge.catalog.dto;

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
            @NotNull @Size(max = MAX_CASE_FIELD_CHARS) String input,
            @NotNull @Size(max = MAX_CASE_FIELD_CHARS) String expectedOutput,
            Boolean isSample) {

        public boolean sample() {
            return Boolean.TRUE.equals(isSample);
        }
    }

    public record TestsetRequest(@NotEmpty @Size(max = 200) List<@NotNull @Valid CaseRequest> cases) {}

    public record TestsetCreated(int version, String contentHash) {}
}
