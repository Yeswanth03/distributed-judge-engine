package com.minicodejudge.catalog.messaging;

import com.minicodejudge.catalog.repository.UserProfileRepository;
import com.minicodejudge.catalog.service.InvalidUserEventException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.amqp.AmqpRejectAndDontRequeueException;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.stereotype.Component;

/**
 * Keeps user_profiles in step with the User Service, so the leaderboard can show names and hide disabled accounts.
 * Delivery is at least once and may be out of order; the version on each event makes both harmless.
 */
@Component
public class UserListener {

    private static final Logger log = LoggerFactory.getLogger(UserListener.class);

    private final UserEventParser parser;
    private final UserProfileRepository profiles;

    public UserListener(UserEventParser parser, UserProfileRepository profiles) {
        this.parser = parser;
        this.profiles = profiles;
    }

    @RabbitListener(queues = RabbitConfig.USER_QUEUE)
    public void onUserEvent(Message message) {
        try {
            UserEvent event = parser.parse(message.getBody());
            boolean changed = profiles.upsertIfNewer(event) == 1;
            log.debug("user {} version {}: {}", event.userId(), event.version(), changed ? "stored" : "older or duplicate, ignored");
        } catch (InvalidUserEventException e) {
            log.warn("dead-lettering user event: {}", e.getMessage());
            throw new AmqpRejectAndDontRequeueException(e.getMessage(), e);
        }
    }
}
