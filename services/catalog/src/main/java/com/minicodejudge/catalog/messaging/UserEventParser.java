package com.minicodejudge.catalog.messaging;

import com.minicodejudge.catalog.service.InvalidUserEventException;
import org.jspecify.annotations.Nullable;
import org.springframework.stereotype.Component;
import tools.jackson.core.JacksonException;
import tools.jackson.databind.json.JsonMapper;

/**
 * The only class that knows the User Service's event format (snake_case JSON, docs/contracts.md section 7). Every
 * event type carries the full public state, so the type itself is not needed. Email is never sent.
 */
@Component
public class UserEventParser {

    record UserMessage(
            @Nullable String userId,
            @Nullable String username,
            @Nullable String displayName,
            @Nullable String status,
            @Nullable Long version) {}

    private final JsonMapper json;

    public UserEventParser(JsonMapper json) {
        this.json = json;
    }

    public UserEvent parse(byte[] body) {
        UserMessage m;
        try {
            m = json.readValue(body, UserMessage.class);
        } catch (JacksonException e) {
            throw new InvalidUserEventException("unreadable user event: " + e.getOriginalMessage(), e);
        }
        if (m == null) {
            throw new InvalidUserEventException("empty user event");
        }
        String userId = required(m.userId(), "user_id").strip();
        String username = required(m.username(), "username").strip();
        String status = required(m.status(), "status");
        long version = required(m.version(), "version");
        if (userId.isEmpty() || userId.length() > 200) {
            throw new InvalidUserEventException("user_id must be 1 to 200 characters");
        }
        if (username.isEmpty()) {
            throw new InvalidUserEventException("username must not be blank");
        }
        if (!status.equals("active") && !status.equals("disabled")) {
            throw new InvalidUserEventException("status must be active or disabled");
        }
        if (version < 0) {
            throw new InvalidUserEventException("version must be at least 0");
        }
        String displayName = m.displayName() == null ? "" : m.displayName().strip();
        return new UserEvent(userId, username, displayName, status, version);
    }

    private static <T> T required(@Nullable T value, String field) {
        if (value == null) {
            throw new InvalidUserEventException(field + " is missing");
        }
        return value;
    }
}
