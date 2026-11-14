-- Trial runs ("Run" in the editor): code executed against sample cases or the caller's own inputs. Never scored,
-- and deleted after a while, so nothing else refers to them. result is the worker's result message as received.
CREATE TABLE runs (
    id             UUID         PRIMARY KEY,
    user_id        UUID         NOT NULL,
    problem_id     VARCHAR(80)  NOT NULL, -- Catalog problem UUID
    language       VARCHAR(32)  NOT NULL,
    status         VARCHAR(16)  NOT NULL DEFAULT 'QUEUED' CHECK (status IN ('QUEUED', 'COMPLETED', 'FAILED')),
    result         JSONB,
    error_message  TEXT,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CHECK ((status = 'COMPLETED') = (result IS NOT NULL))
);

-- The per-user rate limit counts a user's recent runs.
CREATE INDEX idx_runs_user_created ON runs (user_id, created_at DESC);

-- The janitor only looks at runs still waiting for a result.
CREATE INDEX idx_runs_queued ON runs (created_at) WHERE status = 'QUEUED';
