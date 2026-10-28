package com.minicodejudge.catalog.controller;

import com.minicodejudge.catalog.dto.ApiError;
import com.minicodejudge.catalog.service.ConflictException;
import com.minicodejudge.catalog.service.NotFoundException;
import com.minicodejudge.catalog.service.UnprocessableException;
import java.util.Locale;
import java.util.stream.Collectors;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.method.annotation.HandlerMethodValidationException;
import org.springframework.web.method.annotation.MethodArgumentTypeMismatchException;
import org.springframework.web.servlet.NoHandlerFoundException;
import org.springframework.web.servlet.resource.NoResourceFoundException;

@RestControllerAdvice
public class ApiExceptionHandler {

    @ExceptionHandler(NotFoundException.class)
    public ResponseEntity<ApiError> notFound(NotFoundException e) {
        return build(HttpStatus.NOT_FOUND, "not_found", e.getMessage());
    }

    /** A URL no endpoint matches, e.g. a typo such as /admin/problemz. */
    @ExceptionHandler({NoHandlerFoundException.class, NoResourceFoundException.class})
    public ResponseEntity<ApiError> noSuchEndpoint() {
        return build(HttpStatus.NOT_FOUND, "not_found", "no such endpoint");
    }

    @ExceptionHandler(ConflictException.class)
    public ResponseEntity<ApiError> conflict(ConflictException e) {
        return build(HttpStatus.CONFLICT, "conflict", e.getMessage());
    }

    @ExceptionHandler(UnprocessableException.class)
    public ResponseEntity<ApiError> unprocessable(UnprocessableException e) {
        return build(HttpStatus.UNPROCESSABLE_CONTENT, "validation_failed", e.getMessage());
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

    /** A query or path parameter breaks a rule declared on it, e.g. ?limit=500 or ?difficulty=brutal. */
    @ExceptionHandler(HandlerMethodValidationException.class)
    public ResponseEntity<ApiError> invalidParameter(HandlerMethodValidationException e) {
        String message = e.getParameterValidationResults().stream()
                .flatMap(r -> r.getResolvableErrors().stream()
                        .map(err -> r.getMethodParameter().getParameterName() + " " + err.getDefaultMessage()))
                .collect(Collectors.joining("; "));
        return build(HttpStatus.BAD_REQUEST, "bad_request", message);
    }

    /** A path or query value of the wrong type, e.g. /admin/problems/abc where a UUID is expected. */
    @ExceptionHandler(MethodArgumentTypeMismatchException.class)
    public ResponseEntity<ApiError> typeMismatch(MethodArgumentTypeMismatchException e) {
        return build(HttpStatus.BAD_REQUEST, "bad_request", "invalid value for " + e.getName());
    }

    private static ResponseEntity<ApiError> build(HttpStatus status, String error, String message) {
        return ResponseEntity.status(status).body(new ApiError(error, message));
    }

    /** Report field names the way clients send them (statementMd becomes statement_md). */
    private static String snake(String field) {
        return field.replaceAll("([a-z0-9])([A-Z])", "$1_$2").toLowerCase(Locale.ROOT);
    }
}
