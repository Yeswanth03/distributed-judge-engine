package com.minicodejudge.catalog.messaging;

import java.time.Instant;
import java.util.UUID;

/**
 * One judged submission, as the leaderboard needs it. Built from a verdict message by {@link VerdictMessageParser},
 * which is the only place that knows the message's wire format.
 */
public record VerdictEvent(
        UUID submissionId,
        String userId,
        UUID problemId,
        int testsetVersion,
        String verdict,
        int rejudgeSeq,
        Instant judgedAt) {

    public static final String ACCEPTED = "accepted";

    public boolean accepted() {
        return ACCEPTED.equals(verdict);
    }
}
