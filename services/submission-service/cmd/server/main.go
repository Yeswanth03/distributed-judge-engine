// Command server runs the Submission Service: the HTTP API, the outbox relay, the verdict consumers,
// the SSE hub and the reaper, all in one process.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/config"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/consumer"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/handler"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/outbox"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/reaper"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/sse"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	// `server healthcheck` probes a running server's /healthz: the image is distroless, without curl.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(cfg.HTTPAddr))
	}
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)

	if err := run(cfg, log); err != nil {
		log.Error("service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, log *slog.Logger) error {
	// Cancelled on SIGINT/SIGTERM (OpenShift sends SIGTERM before killing the pod).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connectDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := repository.Migrate(ctx, pool); err != nil {
		return err
	}
	repo := repository.New(pool)
	log.Info("database ready")

	// Background work in two tiers, so shutdown can stop them in order: first the workers (relay, consumers,
	// reaper), then the broker connection they use.
	connCtx, stopConn := context.WithCancel(context.Background())
	defer stopConn()
	workCtx, stopWork := context.WithCancel(connCtx)
	defer stopWork()
	var connDone, workDone sync.WaitGroup

	// The broker is not needed to accept submissions: if it is down, they wait in the outbox. So we do not
	// wait for it here; the connection comes up (and comes back) in the background.
	mq := broker.NewConn(cfg.AMQPURL, log.With("component", "broker"), broker.DeclareTopology)
	connDone.Go(func() { mq.Run(connCtx) })
	publisher := broker.NewPublisher(mq, 10*time.Second)
	hub := sse.NewHub()

	relay := outbox.NewRelay(repo, publisher, outbox.Options{
		PollInterval:    cfg.OutboxPollInterval,
		BatchSize:       cfg.OutboxBatchSize,
		MaxRetries:      cfg.OutboxMaxRetries,
		Retention:       cfg.OutboxRetention,
		CleanupInterval: time.Hour,
		GaugeInterval:   5 * time.Second,
		MaxBackoff:      5 * time.Second,
	}, log.With("component", "outbox"))
	dbConsumer := &consumer.DBConsumer{
		Conn: mq, Store: repo, Queue: broker.VerdictQueue, Prefetch: 20, Workers: 4,
		Log: log.With("component", "verdict-consumer"),
	}
	sseConsumer := &consumer.SSEConsumer{
		Conn: mq, Hub: hub, Exchange: broker.VerdictExchange, Log: log.With("component", "sse-consumer"),
	}
	stuck := reaper.New(repo, publisher, hub, reaper.Options{
		Interval:       cfg.ReaperInterval,
		QueuedTimeout:  cfg.ReaperQueuedTimeout,
		RunningTimeout: cfg.ReaperRunningTimeout,
		Exchange:       broker.VerdictExchange,
		RoutingKey:     broker.StatusRoutingKey,
	}, log.With("component", "reaper"))
	for _, run := range []func(context.Context){relay.Run, dbConsumer.Run, sseConsumer.Run, stuck.Run} {
		workDone.Go(func() { run(workCtx) })
	}

	h := handler.New(repo, handler.Options{
		MaxCodeBytes:     cfg.MaxCodeBytes,
		AllowedLanguages: cfg.AllowedLanguages,
		CORSOrigin:       cfg.CORSOrigin,
		NudgeOutbox:      relay.Nudge,
		Hub:              hub,
		Readiness: []handler.ReadinessCheck{
			{Name: "database", Check: repo.Ping},
			{Name: "broker", Check: mq.Check},
		},
		Logger: log,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h.Routes(promhttp.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: SSE responses stay open until the verdict arrives.
	}
	// Shutdown waits for in-flight requests, and an SSE stream would otherwise keep it waiting until the
	// timeout. Closing the hub ends the streams; browsers reconnect to another replica.
	srv.RegisterOnShutdown(hub.Close)

	serveErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr, "profile", cfg.Profile)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	// Order matters: stop taking requests (and end SSE streams) first, then let the workers finish what they
	// hold (the relay its batch, the consumers their unacked messages), and only then close the broker
	// connection they use. Unpublished events stay PENDING; unacked messages go back to the queue.
	shutdownErr := srv.Shutdown(shutdownCtx)
	stopWork()
	workDone.Wait()
	stopConn()
	connDone.Wait()
	if shutdownErr != nil {
		return shutdownErr
	}
	log.Info("shutdown complete")
	return nil
}

// connectDB retries for a while, since in docker-compose and on OpenShift the database may start after us.
func connectDB(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err // malformed URL: retrying will not help
	}
	backoff := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if attempt == 10 {
			pool.Close()
			return nil, err
		}
		slog.Warn("database not reachable yet, retrying", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

func healthcheck(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	// JSON logs: one object per line, which OpenShift's log collection can parse.
	return slog.New(handler.NewLogHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}
