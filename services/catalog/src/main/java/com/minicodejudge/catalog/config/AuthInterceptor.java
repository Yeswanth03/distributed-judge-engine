package com.minicodejudge.catalog.config;

import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import org.springframework.web.servlet.HandlerInterceptor;

/**
 * Trusts the gateway for user identity: it verifies the login token and forwards X-User-Role.
 * That is only safe if this service is NOT reachable except through the gateway, and the gateway strips
 * any X-User-* headers a client sends. Internal calls from the judge use a shared service token instead.
 */
@Component
public class AuthInterceptor implements HandlerInterceptor {

    private final byte[] internalToken;

    public AuthInterceptor(@Value("${catalog.internal-token}") String internalToken) {
        this.internalToken = internalToken.getBytes(StandardCharsets.UTF_8);
    }

    @Override
    public boolean preHandle(HttpServletRequest request, HttpServletResponse response, Object handler) throws Exception {
        String path = request.getRequestURI();

        if (path.startsWith("/admin/")) {
            if (!"admin".equalsIgnoreCase(request.getHeader("X-User-Role"))) {
                deny(response, 403, "forbidden", "admin role required");
                return false;
            }
        } else if (path.startsWith("/internal/")) {
            String presented = request.getHeader("X-Service-Token");
            boolean ok = presented != null
                    && MessageDigest.isEqual(presented.getBytes(StandardCharsets.UTF_8), internalToken);
            if (!ok) {
                deny(response, 401, "unauthorized", "valid service token required");
                return false;
            }
        }
        return true;
    }

    private static void deny(HttpServletResponse response, int status, String error, String message) throws Exception {
        response.setStatus(status);
        response.setContentType("application/json");
        response.getWriter().write("{\"error\":\"" + error + "\",\"message\":\"" + message + "\"}");
    }
}
