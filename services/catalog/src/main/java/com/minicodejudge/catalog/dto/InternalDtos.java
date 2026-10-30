package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import java.util.List;
import java.util.UUID;

/** Shapes for /internal/** (judge workers only). JSON field names are snake_case (see application.properties). */
public final class InternalDtos {

    private InternalDtos() {}

    /** One case as the judge runs it. Hidden cases are included; this is the only route that returns them. */
    public record InternalCase(
            @Schema(description = "Judging order, starting at 1", example = "1") int ordinal,
            @Schema(example = "2 7 11 15\n9") String input,
            @Schema(example = "0 1") String expectedOutput,
            @Schema(description = "true if users can see this case; lets hints mention a sample without revealing hidden ones")
            boolean isSample) {}

    /** Everything a judge worker needs for one published version: the cases plus the limits to enforce. */
    public record InternalTestset(
            UUID problemId,
            @Schema(description = "The published version these cases belong to", example = "1") int version,
            @Schema(description = "SHA-256 of the cases; record it with the verdict and use it to check a cached copy")
            String contentHash,
            @Schema(description = "CPU time limit per case; fixed once the problem is published", example = "1000")
            int timeLimitMs,
            @Schema(description = "Memory limit; fixed once the problem is published", example = "256")
            int memoryLimitMb,
            @Schema(description = "All cases, hidden and sample, in judging order") List<InternalCase> cases) {}
}
