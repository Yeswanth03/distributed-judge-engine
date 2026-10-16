-- Problems only. Test sets and the leaderboard arrive in later migrations.

CREATE TABLE problems (
    id               UUID PRIMARY KEY,
    slug             TEXT        NOT NULL UNIQUE,
    title            TEXT        NOT NULL,
    statement_md     TEXT        NOT NULL,
    difficulty       TEXT        NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    points           INT         NOT NULL CHECK (points > 0),
    time_limit_ms    INT         NOT NULL CHECK (time_limit_ms > 0),
    memory_limit_mb  INT         NOT NULL CHECK (memory_limit_mb > 0),
    status           TEXT        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
