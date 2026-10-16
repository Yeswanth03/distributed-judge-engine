package com.minicodejudge.catalog.service;

import java.io.Serial;

/** The request clashes with what is already stored (for example a slug that is taken). Maps to 409. */
public class ConflictException extends RuntimeException {

    @Serial
    private static final long serialVersionUID = 1L;

    public ConflictException(String message) {
        super(message);
    }
}
