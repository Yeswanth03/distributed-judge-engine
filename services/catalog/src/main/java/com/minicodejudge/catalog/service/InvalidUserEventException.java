package com.minicodejudge.catalog.service;

/** A user event that can never be applied (malformed or missing fields); it is dead-lettered, not retried forever. */
public class InvalidUserEventException extends RuntimeException {

    public InvalidUserEventException(String message) {
        super(message);
    }

    public InvalidUserEventException(String message, Throwable cause) {
        super(message, cause);
    }
}
