-- Leaderboard read model, built from verdict events. Each level is recomputed from the one below it rather than
-- incremented, so duplicate deliveries and rejudges always converge to the right score:
--   results (latest verdict per submission) -> user_problem_status (solved?) -> user_scores (score, rank inputs)

-- Latest verdict per submission. A message only replaces the row when its rejudge_seq is higher.
CREATE TABLE results (
    submission_id    UUID        PRIMARY KEY,
    user_id          TEXT        NOT NULL CHECK (length(user_id) BETWEEN 1 AND 200),
    problem_id       UUID        NOT NULL REFERENCES problems (id),
    testset_version  INT         NOT NULL CHECK (testset_version > 0),
    verdict          TEXT        NOT NULL,
    rejudge_seq      INT         NOT NULL CHECK (rejudge_seq >= 0),
    judged_at        TIMESTAMPTZ NOT NULL,
    applied_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX results_user_problem ON results (user_id, problem_id);
-- Finds the most recently applied verdict, which drives as_of and lag_seconds.
CREATE INDEX results_applied_at ON results (applied_at DESC);

CREATE TABLE user_problem_status (
    user_id          TEXT        NOT NULL,
    problem_id       UUID        NOT NULL REFERENCES problems (id),
    solved           BOOLEAN     NOT NULL,
    first_solved_at  TIMESTAMPTZ,
    PRIMARY KEY (user_id, problem_id)
);

CREATE TABLE user_scores (
    user_id         TEXT        PRIMARY KEY,
    score           INT         NOT NULL,
    solved_count    INT         NOT NULL,
    -- When the user reached the current score: earlier ranks higher on equal score.
    last_solved_at  TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX user_scores_ranking ON user_scores (score DESC, last_solved_at ASC, user_id ASC);
