package com.minicodejudge.catalog.dto;

/** The one error shape for the whole service: {"error": "...", "message": "..."}. */
public record ApiError(String error, String message) {}
