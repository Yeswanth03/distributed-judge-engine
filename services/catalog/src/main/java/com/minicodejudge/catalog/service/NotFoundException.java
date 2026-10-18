package com.minicodejudge.catalog.service;

import java.io.Serial;

/** The requested resource does not exist. Maps to 404. */
public class NotFoundException extends RuntimeException {

    @Serial
    private static final long serialVersionUID = 1L;

    public NotFoundException(String message) {
        super(message);
    }
}
