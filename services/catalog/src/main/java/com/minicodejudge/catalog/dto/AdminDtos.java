package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.jspecify.annotations.Nullable;

/** Admin read shapes: unlike the public ones, they include drafts, status and versions. snake_case JSON. */
public final class AdminDtos {

    private AdminDtos() {}

    public record AdminProblemSummary(
            UUID id,
            @Schema(example = "two-sum") String slug,
            @Schema(example = "Two Sum") String title,
            @Schema(allowableValues = {"easy", "medium", "hard"}, example = "easy") String difficulty,
            @Schema(example = "100") int points,
            @Schema(allowableValues = {"draft", "published"}, example = "draft") String status,
            @Schema(description = "Published test set version; null until the first publish", example = "1")
            @Nullable Integer currentVersion,
            Instant updatedAt) {}

    public record AdminProblemPage(
            @Schema(description = "All problems, drafts included, sorted by slug") List<AdminProblemSummary> items,
            @Schema(description = "Pass as cursor to get the next page; null on the last page") @Nullable String nextCursor) {}

    public record AdminProblemView(
            UUID id,
            @Schema(example = "two-sum") String slug,
            @Schema(example = "Two Sum") String title,
            @Schema(description = "Problem statement in Markdown") String statementMd,
            @Schema(allowableValues = {"easy", "medium", "hard"}, example = "easy") String difficulty,
            @Schema(example = "100") int points,
            @Schema(example = "1000") int timeLimitMs,
            @Schema(example = "256") int memoryLimitMb,
            @Schema(allowableValues = {"draft", "published"}, example = "draft") String status,
            @Schema(description = "Published test set version; null until the first publish") @Nullable Integer currentVersion,
            Instant createdAt,
            Instant updatedAt) {}

    public record TestsetVersion(
            @Schema(example = "1") int version,
            @Schema(allowableValues = {"draft", "published"}, example = "draft") String status,
            @Schema(description = "true for the version the judge and the problem page use") boolean current,
            @Schema(description = "SHA-256 fingerprint of the cases") String contentHash,
            @Schema(example = "12") int caseCount,
            @Schema(example = "2") int sampleCount,
            Instant createdAt,
            @Schema(description = "null while a draft") @Nullable Instant publishedAt) {}
}
