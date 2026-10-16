package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.ApiError;
import com.minicodejudge.catalog.service.ConflictException;
import java.util.Locale;
import java.util.stream.Collectors;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@RestControllerAdvice
public class ApiExceptionHandler {

    @ExceptionHandler(ConflictException.class)
    public ResponseEntity<ApiError> conflict(ConflictException e) {
        return build(HttpStatus.CONFLICT, "conflict", e.getMessage());
    }

    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ResponseEntity<ApiError> invalid(MethodArgumentNotValidException e) {
        String message = e.getBindingResult().getFieldErrors().stream()
                .map(f -> snake(f.getField()) + " " + f.getDefaultMessage())
                .collect(Collectors.joining("; "));
        return build(HttpStatus.UNPROCESSABLE_CONTENT, "validation_failed", message);
    }

    @ExceptionHandler(HttpMessageNotReadableException.class)
    public ResponseEntity<ApiError> unreadable() {
        return build(HttpStatus.BAD_REQUEST, "bad_request", "malformed request body");
    }

    private static ResponseEntity<ApiError> build(HttpStatus status, String error, String message) {
        return ResponseEntity.status(status).body(new ApiError(error, message));
    }

    /** Report field names the way clients send them (statementMd becomes statement_md). */
    private static String snake(String field) {
        return field.replaceAll("([a-z0-9])([A-Z])", "$1_$2").toLowerCase(Locale.ROOT);
    }
}
