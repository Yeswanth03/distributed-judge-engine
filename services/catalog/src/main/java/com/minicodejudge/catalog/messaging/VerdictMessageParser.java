package com.minicodejudge.catalog.messaging;

import com.minicodejudge.catalog.service.InvalidVerdictException;
import java.time.Instant;
import java.util.Locale;
import java.util.UUID;
import org.jspecify.annotations.Nullable;
import org.springframework.stereotype.Component;
import tools.jackson.core.JacksonException;
import tools.jackson.databind.json.JsonMapper;

/**
 * The only class that knows the verdict message's wire format (snake_case JSON). The format is a team contract
 * still being agreed: if it changes, only this class changes. Fields the leaderboard does not use are ignored.
 */
@Component
public class VerdictMessageParser {

    /** The fields the leaderboard reads; boxed so a missing field can be reported by name. */
    record VerdictMessage(
            @Nullable UUID submissionId,
            @Nullable String userId,
            @Nullable UUID problemId,
            @Nullable Integer testsetVersion,
            @Nullable String verdict,
            @Nullable Integer rejudgeSeq,
            @Nullable Instant judgedAt) {}

    private final JsonMapper json;

    public VerdictMessageParser(JsonMapper json) {
        this.json = json;
    }

    public VerdictEvent parse(byte[] body) {
        VerdictMessage m;
        try {
            m = json.readValue(body, VerdictMessage.class);
        } catch (JacksonException e) {
            throw new InvalidVerdictException("unreadable verdict message: " + e.getOriginalMessage(), e);
        }
        if (m == null) {
            throw new InvalidVerdictException("empty verdict message");
        }
        String userId = required(m.userId(), "user_id").strip();
        String verdict = required(m.verdict(), "verdict").strip().toLowerCase(Locale.ROOT);
        int version = required(m.testsetVersion(), "testset_version");
        int rejudgeSeq = required(m.rejudgeSeq(), "rejudge_seq");
        if (userId.isEmpty() || userId.length() > 200) {
            throw new InvalidVerdictException("user_id must be 1 to 200 characters");
        }
        if (verdict.isEmpty()) {
            throw new InvalidVerdictException("verdict must not be blank");
        }
        if (version < 1 || rejudgeSeq < 0) {
            throw new InvalidVerdictException("testset_version must be at least 1 and rejudge_seq at least 0");
        }
        return new VerdictEvent(required(m.submissionId(), "submission_id"), userId,
                required(m.problemId(), "problem_id"), version, verdict, rejudgeSeq, required(m.judgedAt(), "judged_at"));
    }

    private static <T> T required(@Nullable T value, String field) {
        if (value == null) {
            throw new InvalidVerdictException(field + " is missing");
        }
        return value;
    }
}
