// Command server runs the Submission Service: the HTTP API, the outbox relay, the verdict consumers,
// the SSE hub and the reaper, all in one process.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/config"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/handler"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
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

	h := handler.New(repo, handler.Options{
		MaxCodeBytes:     cfg.MaxCodeBytes,
		AllowedLanguages: cfg.AllowedLanguages,
		CORSOrigin:       cfg.CORSOrigin,
		Readiness:        []handler.ReadinessCheck{{Name: "database", Check: repo.Ping}},
		Logger:           log,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h.Routes(promhttp.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: SSE responses stay open until the verdict arrives.
	}

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

	log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
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

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	// JSON logs: one object per line, which OpenShift's log collection can parse.
	return slog.New(handler.NewLogHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}
