package com.minicodejudge.catalog.repository;

import com.minicodejudge.catalog.messaging.UserEvent;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** SQL for user_profiles. No rules live here. */
@Repository
public class UserProfileRepository {

    private final JdbcClient jdbc;

    public UserProfileRepository(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    /**
     * Stores the event's state if it is newer than what we have. Returns 1 if stored, 0 for a duplicate or an older
     * event that arrived late: the version, not the delivery order, decides.
     */
    public int upsertIfNewer(UserEvent e) {
        return jdbc.sql("""
                INSERT INTO user_profiles (user_id, username, display_name, status, version)
                VALUES (:userId, :username, :displayName, :status, :version)
                ON CONFLICT (user_id) DO UPDATE
                   SET username = EXCLUDED.username, display_name = EXCLUDED.display_name,
                       status = EXCLUDED.status, version = EXCLUDED.version, updated_at = now()
                 WHERE user_profiles.version < EXCLUDED.version
                """)
                .param("userId", e.userId())
                .param("username", e.username())
                .param("displayName", e.displayName())
                .param("status", e.status())
                .param("version", e.version())
                .update();
    }
}
