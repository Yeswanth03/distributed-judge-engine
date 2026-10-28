package com.minicodejudge.catalog.service;

import java.io.Serial;

/** A verdict message that can never be applied (malformed, or about an unknown problem). Goes to the dead-letter queue. */
public class InvalidVerdictException extends RuntimeException {

    @Serial
    private static final long serialVersionUID = 1L;

    public InvalidVerdictException(String message) {
        super(message);
    }

    public InvalidVerdictException(String message, Throwable cause) {
        super(message, cause);
    }
}
