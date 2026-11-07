-- Context now comes from the Submission Service and Catalog instead of the request body.

-- The verdict event may carry the problem slug rather than Catalog's UUID, and an
-- on-demand request only learns the UUID after asking Catalog.
ALTER TABLE feedback_requests ALTER COLUMN problem_id TYPE TEXT USING problem_id::text;
ALTER TABLE feedback_requests ALTER COLUMN problem_id DROP NOT NULL;

-- What the judge reported (tests passed, error message, testset version), for the prompt.
ALTER TABLE feedback_requests ADD COLUMN IF NOT EXISTS judge_details JSONB;
-- Why a request ended as unavailable.
ALTER TABLE feedback_requests ADD COLUMN IF NOT EXISTS error_reason TEXT;

ALTER TABLE feedback_requests DROP CONSTRAINT IF EXISTS feedback_requests_status_check;

-- Records parked while the lookup did not exist are picked up by the background sweep.
UPDATE feedback_requests SET status = 'queued' WHERE status = 'awaiting_context';
UPDATE feedback_requests SET status = 'processing' WHERE status = 'generating';

ALTER TABLE feedback_requests
    ADD CONSTRAINT feedback_requests_status_check
    CHECK (status IN ('queued', 'processing', 'ready', 'unavailable'));

CREATE INDEX IF NOT EXISTS feedback_requests_pending
    ON feedback_requests (status, updated_at)
    WHERE status IN ('queued', 'processing');
