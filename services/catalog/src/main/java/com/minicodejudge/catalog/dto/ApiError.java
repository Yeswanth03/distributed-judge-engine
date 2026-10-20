package com.minicodejudge.catalog.dto;

import io.swagger.v3.oas.annotations.media.Schema;

/** The one error shape for the whole service: {"error": "...", "message": "..."}. */
public record ApiError(
        @Schema(description = "Machine-readable code", example = "not_found") String error,
        @Schema(description = "Human-readable explanation", example = "problem not found") String message) {}
