-- Domain table: one row per submission. status only moves forward (QUEUED -> RUNNING -> COMPLETED);
-- the repository's guarded UPDATEs enforce that, which is what makes duplicate events harmless.
CREATE TABLE submissions (
    id                 UUID         PRIMARY KEY,
    user_id            UUID         NOT NULL,
    problem_id         VARCHAR(80)  NOT NULL, -- Catalog problem UUID
    language           VARCHAR(32)  NOT NULL,
    code               TEXT         NOT NULL,
    status             VARCHAR(32)  NOT NULL DEFAULT 'QUEUED'
                       CHECK (status IN ('QUEUED', 'RUNNING', 'COMPLETED')),
    verdict            VARCHAR(32)
                       CHECK (verdict IN ('ACCEPTED', 'WRONG_ANSWER', 'TIME_LIMIT_EXCEEDED', 'MEMORY_LIMIT_EXCEEDED',
                                          'RUNTIME_ERROR', 'COMPILATION_ERROR', 'INTERNAL_ERROR')),
    execution_time_ms  INTEGER      NOT NULL DEFAULT 0,
    memory_kb          INTEGER      NOT NULL DEFAULT 0,
    test_cases_passed  INTEGER      NOT NULL DEFAULT 0,
    total_test_cases   INTEGER      NOT NULL DEFAULT 0,
    error_message      TEXT,
    testset_version    INTEGER,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CHECK ((status = 'COMPLETED') = (verdict IS NOT NULL))
);

-- A user's history, newest first, optionally for one problem (keyset pagination on created_at, id).
CREATE INDEX idx_submissions_user_created ON submissions (user_id, created_at DESC, id DESC);
CREATE INDEX idx_submissions_user_problem_created ON submissions (user_id, problem_id, created_at DESC, id DESC);

-- The reaper only looks at unfinished submissions.
CREATE INDEX idx_submissions_unfinished ON submissions (status, updated_at) WHERE status <> 'COMPLETED';

-- Transport table: messages written in the same transaction as the domain row, shipped to RabbitMQ by the relay.
CREATE TABLE outbox_events (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type  VARCHAR(64)  NOT NULL DEFAULT 'SUBMISSION',
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
