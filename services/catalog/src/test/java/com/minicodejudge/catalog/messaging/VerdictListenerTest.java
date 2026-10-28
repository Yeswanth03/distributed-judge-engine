package com.minicodejudge.catalog.messaging;

import static org.assertj.core.api.Assertions.assertThat;

import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.core.MessageBuilder;
import org.springframework.amqp.core.MessageProperties;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.postgresql.PostgreSQLContainer;
import org.testcontainers.rabbitmq.RabbitMQContainer;

/** The verdict consumer end to end: a real RabbitMQ broker and a real Postgres. */
@SpringBootTest(properties = {
        "spring.rabbitmq.listener.simple.auto-startup=true",
        "spring.rabbitmq.listener.simple.retry.initial-interval=50ms"})
@Testcontainers
class VerdictListenerTest {

    @Container
    @ServiceConnection
    static PostgreSQLContainer postgres = new PostgreSQLContainer("postgres:16-alpine");

    @Container
    @ServiceConnection
    static RabbitMQContainer rabbit = new RabbitMQContainer("rabbitmq:4-alpine");

    @Autowired
    RabbitTemplate rabbitTemplate;

    @Autowired
    JdbcClient jdbc;

    private UUID problemId;

    @BeforeEach
    void seedProblem() {
        jdbc.sql("TRUNCATE results, user_problem_status, user_scores, test_cases, testsets, problems").update();
        problemId = UUID.randomUUID();
        jdbc.sql("""
                INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb)
                VALUES (:id, 'two-sum', 'Two Sum', 'Statement.', 'easy', 100, 1000, 256)
                """)
                .param("id", problemId)
                .update();
    }

    @Test
    void publishedVerdictReachesTheLeaderboardOnceEvenIfDeliveredTwice() {
        String body = """
                {"submission_id":"%s","user_id":"alice","problem_id":"%s","testset_version":1,
                 "verdict":"accepted","rejudge_seq":0,"judged_at":"2026-11-01T10:00:00Z","cpu_time_ms":12}
                """.formatted(UUID.randomUUID(), problemId);

        publish("verdict.accepted", body);
        publish("verdict.accepted", body);

        waitUntil(() -> jdbc.sql("SELECT count(*) FROM results").query(Integer.class).single() == 1);
        waitUntil(() -> scoreOf("alice") == 100);
        assertThat(jdbc.sql("SELECT count(*) FROM results").query(Integer.class).single()).isEqualTo(1);
    }

    @Test
    void malformedVerdictIsDeadLettered() {
        publish("verdict.failed", "{\"user_id\":\"alice\"}");

        Message dead = rabbitTemplate.receive(RabbitConfig.DEAD_LETTER_QUEUE, Duration.ofSeconds(20).toMillis());

        assertThat(dead).isNotNull();
        assertThat(new String(dead.getBody(), StandardCharsets.UTF_8)).contains("alice");
        assertThat(jdbc.sql("SELECT count(*) FROM results").query(Integer.class).single()).isZero();
    }

    // ---- helpers ----

    private void publish(String routingKey, String json) {
        MessageProperties props = new MessageProperties();
        props.setContentType(MessageProperties.CONTENT_TYPE_JSON);
        rabbitTemplate.send(RabbitConfig.VERDICT_EXCHANGE, routingKey,
                MessageBuilder.withBody(json.getBytes(StandardCharsets.UTF_8)).andProperties(props).build());
    }

    private int scoreOf(String user) {
        return jdbc.sql("SELECT COALESCE((SELECT score FROM user_scores WHERE user_id = :u), -1)")
                .param("u", user).query(Integer.class).single();
    }

    private static void waitUntil(BooleanSupplier condition) {
        long deadline = System.nanoTime() + Duration.ofSeconds(20).toNanos();
        while (!condition.getAsBoolean()) {
            if (System.nanoTime() > deadline) {
                throw new AssertionError("condition not met within 20 s");
            }
            try {
                Thread.sleep(100);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new AssertionError(e);
            }
        }
    }
}
