package com.minicodejudge.catalog.messaging;

import org.springframework.amqp.core.Binding;
import org.springframework.amqp.core.BindingBuilder;
import org.springframework.amqp.core.DirectExchange;
import org.springframework.amqp.core.Queue;
import org.springframework.amqp.core.QueueBuilder;
import org.springframework.amqp.core.TopicExchange;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

/**
 * Verdicts fan out through a topic exchange; every interested service binds its own durable queue. The catalog
 * takes all of them ("verdict.#"). A message that keeps failing is dead-lettered to catalog.verdicts.dlq,
 * where it waits for a person instead of blocking the queue.
 */
@Configuration
public class RabbitConfig {

    public static final String VERDICT_EXCHANGE = "verdicts";
    public static final String VERDICT_QUEUE = "catalog.verdicts";
    public static final String DEAD_LETTER_EXCHANGE = "verdicts.dlx";
    public static final String DEAD_LETTER_QUEUE = "catalog.verdicts.dlq";

    @Bean
    TopicExchange verdictExchange() {
        return new TopicExchange(VERDICT_EXCHANGE, true, false);
    }

    @Bean
    DirectExchange deadLetterExchange() {
        return new DirectExchange(DEAD_LETTER_EXCHANGE, true, false);
    }

    @Bean
    Queue verdictQueue() {
        return QueueBuilder.durable(VERDICT_QUEUE)
                .deadLetterExchange(DEAD_LETTER_EXCHANGE)
                .deadLetterRoutingKey(VERDICT_QUEUE)
                .build();
    }

    @Bean
    Queue deadLetterQueue() {
        return QueueBuilder.durable(DEAD_LETTER_QUEUE).build();
    }

    @Bean
    Binding verdictBinding(Queue verdictQueue, TopicExchange verdictExchange) {
        return BindingBuilder.bind(verdictQueue).to(verdictExchange).with("verdict.#");
    }

    @Bean
    Binding deadLetterBinding(Queue deadLetterQueue, DirectExchange deadLetterExchange) {
        return BindingBuilder.bind(deadLetterQueue).to(deadLetterExchange).with(VERDICT_QUEUE);
    }
}
