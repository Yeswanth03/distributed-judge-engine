ALTER TABLE feedback_requests
    DROP CONSTRAINT IF EXISTS feedback_requests_status_check;

ALTER TABLE feedback_requests
    ADD CONSTRAINT feedback_requests_status_check
    CHECK (status IN ('awaiting_context', 'queued', 'generating', 'ready', 'unavailable'));
