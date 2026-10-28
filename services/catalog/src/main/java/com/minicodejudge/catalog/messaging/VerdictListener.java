package com.minicodejudge.catalog.messaging;

import com.minicodejudge.catalog.service.InvalidVerdictException;
import com.minicodejudge.catalog.service.LeaderboardService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.amqp.AmqpRejectAndDontRequeueException;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.stereotype.Component;

/**
 * Consumes verdicts into the leaderboard. Delivery is at-least-once, so the same verdict may arrive twice; the
 * service makes that harmless. The message is acknowledged only after apply() commits (acknowledge-mode auto).
 * Failures are retried (see application.properties); a message that can never succeed is rejected without
 * requeue and dead-lettered.
 */
@Component
public class VerdictListener {

    private static final Logger log = LoggerFactory.getLogger(VerdictListener.class);

    private final VerdictMessageParser parser;
    private final LeaderboardService leaderboard;

    public VerdictListener(VerdictMessageParser parser, LeaderboardService leaderboard) {
        this.parser = parser;
        this.leaderboard = leaderboard;
    }

    @RabbitListener(queues = RabbitConfig.VERDICT_QUEUE)
    public void onVerdict(Message message) {
        try {
            VerdictEvent event = parser.parse(message.getBody());
            boolean changed = leaderboard.apply(event);
            log.debug("verdict {} for submission {} (rejudge {}): {}", event.verdict(), event.submissionId(),
                    event.rejudgeSeq(), changed ? "applied" : "duplicate or stale, ignored");
        } catch (InvalidVerdictException e) {
            log.warn("dead-lettering verdict message: {}", e.getMessage());
            throw new AmqpRejectAndDontRequeueException(e.getMessage(), e);
        }
    }
}
