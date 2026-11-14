-- Public profile of each user, learned from the User Service's events (docs/contracts.md section 7), so the
-- leaderboard can show names. Every event carries the user's full public state; the highest version wins.

CREATE TABLE user_profiles (
    user_id       TEXT        PRIMARY KEY,
    username      TEXT        NOT NULL,
    display_name  TEXT        NOT NULL,
    status        TEXT        NOT NULL CHECK (status IN ('active', 'disabled')),
    version       BIGINT      NOT NULL CHECK (version >= 0),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
