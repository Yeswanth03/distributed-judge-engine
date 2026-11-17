package com.minicodejudge.catalog.messaging;

import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.Gauge;
import io.micrometer.core.instrument.MeterRegistry;
import io.micrometer.core.instrument.Timer;
import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.atomic.AtomicReference;
import org.springframework.stereotype.Component;

/**
 * What the leaderboard's consumers do, for Prometheus (scraped from /actuator/prometheus):
 * catalog_verdicts_total{outcome}, catalog_verdict_apply_seconds, catalog_leaderboard_lag_seconds and
 * catalog_user_events_total{outcome}. A dead-lettered message's final count lives in RabbitMQ (the DLQ depth);
 * "invalid" here counts every rejected attempt, and a message is attempted up to 5 times.
 */
@Component
public class MessagingMetrics {

    private final Counter verdictsApplied;
    private final Counter verdictsIgnored;
    private final Counter verdictsInvalid;
    private final Timer applyTimer;
    private final Counter userEventsStored;
    private final Counter userEventsIgnored;
    private final Counter userEventsInvalid;
    private final AtomicReference<Double> lagSeconds = new AtomicReference<>(0.0);

    public MessagingMetrics(MeterRegistry registry) {
        verdictsApplied = verdicts(registry, "applied", "verdicts that changed the leaderboard");
        verdictsIgnored = verdicts(registry, "ignored", "duplicate or stale verdicts (rejudge_seq not higher)");
        verdictsInvalid = verdicts(registry, "invalid", "attempts rejected as invalid; each message up to 5 times");
        applyTimer = Timer.builder("catalog.verdict.apply")
                .description("Time to apply one verdict: validate, lock, upsert and recompute, in one transaction")
                .publishPercentileHistogram()
                .register(registry);
        userEventsStored = users(registry, "stored", "user events that changed a stored profile");
        userEventsIgnored = users(registry, "ignored", "duplicate or older user events (version not higher)");
        userEventsInvalid = users(registry, "invalid", "attempts rejected as invalid; each message up to 5 times");
        Gauge.builder("catalog.leaderboard.lag", lagSeconds, AtomicReference::get)
                .description("For the last applied verdict: seconds from being judged to reaching the leaderboard")
                .baseUnit("seconds")
                .register(registry);
    }

    private static Counter verdicts(MeterRegistry registry, String outcome, String description) {
        return Counter.builder("catalog.verdicts").tag("outcome", outcome).description(description).register(registry);
    }

    private static Counter users(MeterRegistry registry, String outcome, String description) {
        return Counter.builder("catalog.user.events").tag("outcome", outcome).description(description).register(registry);
    }

    Timer applyTimer() {
        return applyTimer;
    }

    void verdictApplied(Instant judgedAt) {
        verdictsApplied.increment();
        lagSeconds.set(Math.max(0, Duration.between(judgedAt, Instant.now()).toMillis()) / 1000.0);
    }

    void verdictIgnored() {
        verdictsIgnored.increment();
    }

    void verdictInvalid() {
        verdictsInvalid.increment();
    }

    void userEvent(boolean stored) {
        (stored ? userEventsStored : userEventsIgnored).increment();
    }

    void userEventInvalid() {
        userEventsInvalid.increment();
    }
}
