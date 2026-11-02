CREATE TABLE IF NOT EXISTS feedback_requests (
    request_id UUID PRIMARY KEY,
    submission_id UUID NOT NULL,
    user_id TEXT NOT NULL,
    problem_id UUID NOT NULL,
    verdict TEXT NOT NULL,
    rejudge_seq INTEGER NOT NULL DEFAULT 0,
    request_source TEXT NOT NULL CHECK (request_source IN ('failed_verdict', 'user_request')),
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'generating', 'ready', 'unavailable')),
    feedback TEXT,
    model_name TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    input_tokens INTEGER,
    output_tokens INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS feedback_requests_submission_latest
    ON feedback_requests (user_id, submission_id, created_at DESC);
