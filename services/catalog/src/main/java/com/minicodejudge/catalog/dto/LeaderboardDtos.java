package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.jspecify.annotations.Nullable;

/** Leaderboard responses. JSON field names are snake_case (see application.properties). */
public final class LeaderboardDtos {

    private LeaderboardDtos() {}

    public record LeaderboardEntry(
            @Schema(description = "1 = best; users equal on score and on when they reached it share a rank", example = "1")
            int rank,
            @Schema(example = "user-42") String userId,
            @Schema(description = "From the User Service's events; null until Catalog has heard about the user", example = "ada")
            @Nullable String username,
            @Schema(description = "May be empty; null until Catalog has heard about the user", example = "Ada L.")
            @Nullable String displayName,
            @Schema(description = "Sum of the points of solved problems", example = "300") int score,
            @Schema(example = "3") int solvedCount) {}

    public record Leaderboard(
            @Schema(description = "The leaderboard reflects verdicts judged up to this time; null before the first verdict")
            @Nullable Instant asOf,
            @Schema(description = "How long the newest verdict took from being judged to reaching the leaderboard", example = "0.4")
            @Nullable Double lagSeconds,
            @Schema(description = "Users with at least one solve, best first; disabled accounts left out")
            List<LeaderboardEntry> entries) {}

    public record UserStanding(
            @Schema(example = "user-42") String userId,
            @Schema(example = "ada") @Nullable String username,
            @Schema(example = "Ada L.") @Nullable String displayName,
            @Schema(description = "null until the user's first solve, and for a disabled account", example = "7")
            @Nullable Integer rank,
            @Schema(example = "300") int score,
            @Schema(example = "3") int solvedCount,
            @Schema(description = "Solved problems, in the order they were first solved") List<UUID> solvedProblemIds,
            @Nullable Instant asOf,
            @Nullable Double lagSeconds) {}
}
