package com.minicodejudge.catalog.service;

import java.io.Serial;

/** The request is well-formed but breaks a rule that field annotations cannot express. Maps to 422. */
public class UnprocessableException extends RuntimeException {

    @Serial
    private static final long serialVersionUID = 1L;

    public UnprocessableException(String message) {
        super(message);
    }
}
