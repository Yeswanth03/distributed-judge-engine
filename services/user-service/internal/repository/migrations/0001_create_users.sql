-- Accounts. Emails and usernames are unique ignoring case. password_hash is NULL for an account that only signs
-- in with Google or GitHub. version goes up on every change and travels with each event, so consumers can drop
-- stale events delivered out of order.
CREATE TABLE users (
    id              UUID         PRIMARY KEY,
    email           TEXT         NOT NULL,
    username        TEXT         NOT NULL,
    display_name    TEXT         NOT NULL,
    password_hash   TEXT,
    role            VARCHAR(16)  NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    status          VARCHAR(16)  NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    email_verified  BOOLEAN      NOT NULL DEFAULT FALSE,
    failed_logins   INTEGER      NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    version         BIGINT       NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));
CREATE UNIQUE INDEX users_username_key ON users (lower(username));
-- The admin listing pages through users newest first.
CREATE INDEX idx_users_created ON users (created_at DESC, id DESC);

-- One row per signed-in device.
CREATE TABLE sessions (
    id            UUID         PRIMARY KEY,
    user_id       UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_agent    TEXT         NOT NULL DEFAULT '',
    ip            TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ  NOT NULL,
    revoked_at    TIMESTAMPTZ,
    revoke_reason TEXT
);
CREATE INDEX idx_sessions_user ON sessions (user_id, created_at DESC);

-- Every refresh token ever issued for a session, stored as a SHA-256 hash. Each works once: refreshing marks it
-- used and issues the next. A token presented again after use means it was copied, so the session is revoked.
CREATE TABLE refresh_tokens (
    token_hash  BYTEA        PRIMARY KEY,
    session_id  UUID         NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    used_at     TIMESTAMPTZ
);
CREATE INDEX idx_refresh_tokens_session ON refresh_tokens (session_id);

-- Signing keys, only used when no key is configured (the local profile), so every replica and every restart
-- signs with the same key and tokens keep working.
CREATE TABLE signing_keys (
    kid          TEXT         PRIMARY KEY,
    private_pem  TEXT         NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Transport table, same design as the Submission Service: events written in the same transaction as the
-- change, shipped to RabbitMQ by the relay.
CREATE TABLE outbox_events (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_id    UUID         NOT NULL,
    event_type      VARCHAR(64)  NOT NULL,
    payload         JSONB        NOT NULL,
    status          VARCHAR(32)  NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PUBLISHED', 'FAILED')),
    retry_count     INTEGER      NOT NULL DEFAULT 0,
    last_error      TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ
);
CREATE INDEX idx_outbox_pending ON outbox_events (created_at) WHERE status = 'PENDING';
CREATE INDEX idx_outbox_published ON outbox_events (published_at) WHERE status = 'PUBLISHED';
