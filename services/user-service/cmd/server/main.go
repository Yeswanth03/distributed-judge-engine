// Command server runs the User Service: the HTTP API, the outbox relay that publishes user events, and the
// session janitor, all in one process.
package main

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/config"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/handler"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/outbox"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/password"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/repository"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/token"
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

	issuer, err := newIssuer(ctx, cfg, repo, log)
	if err != nil {
		return err
	}
	if err := bootstrapAdmin(ctx, cfg, repo, log); err != nil {
		return err
	}

	// Background work in two tiers, so shutdown can stop them in order: first the workers, then the broker
	// connection they use.
	connCtx, stopConn := context.WithCancel(context.Background())
	defer stopConn()
	workCtx, stopWork := context.WithCancel(connCtx)
	defer stopWork()
	var connDone, workDone sync.WaitGroup

	// The broker is not needed to serve requests: events wait in the outbox while it is down.
	mq := broker.NewConn(cfg.AMQPURL, log.With("component", "broker"), broker.DeclareTopology)
	connDone.Go(func() { mq.Run(connCtx) })
	publisher := broker.NewPublisher(mq, 10*time.Second)

	relay := outbox.NewRelay(repo, publisher, outbox.Options{
		PollInterval:    cfg.OutboxPollInterval,
		BatchSize:       cfg.OutboxBatchSize,
		MaxRetries:      cfg.OutboxMaxRetries,
		Retention:       cfg.OutboxRetention,
		CleanupInterval: time.Hour,
		GaugeInterval:   5 * time.Second,
		MaxBackoff:      5 * time.Second,
	}, log.With("component", "outbox"))
	workDone.Go(func() { relay.Run(workCtx) })
	workDone.Go(func() { sessionJanitor(workCtx, repo, log.With("component", "session-janitor")) })

	h := handler.New(repo, issuer, handler.Options{
		SessionTTL:      cfg.SessionTTL,
		MaxFailedLogins: cfg.MaxFailedLogins,
		LoginLockout:    cfg.LoginLockout,
		CORSOrigin:      cfg.CORSOrigin,
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
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
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

	log.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	// Stop taking requests first, then let the relay finish its batch, and only then close the broker
	// connection. Unpublished events stay PENDING for the next start.
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

// newIssuer signs with the configured key or, in the local profile when none is configured, with the key
// stored in the database, so every replica and restart agrees on it.
func newIssuer(ctx context.Context, cfg config.Config, repo *repository.Repository, log *slog.Logger) (*token.Issuer, error) {
	keyPEM := cfg.SigningKeyPEM
	if keyPEM == "" {
		var err error
		keyPEM, err = repo.SigningKey(ctx, func() (string, string, error) {
			key, err := token.GenerateKey()
			if err != nil {
				return "", "", err
			}
			kid, err := token.KeyID(&key.PublicKey)
			if err != nil {
				return "", "", err
			}
			encoded, err := token.EncodePrivateKey(key)
			return kid, encoded, err
		})
		if err != nil {
			return nil, fmt.Errorf("load signing key: %w", err)
		}
		log.Warn("no JWT_PRIVATE_KEY configured; using the key stored in the database (local profile only)")
	}
	key, err := token.ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY: %w", err)
	}
	var previous []*ecdsa.PublicKey
	for i, p := range cfg.PreviousPublicKeysPEM {
		pub, err := token.ParsePublicKey(p)
		if err != nil {
			return nil, fmt.Errorf("JWT_PREVIOUS_PUBLIC_KEYS #%d: %w", i+1, err)
		}
		previous = append(previous, pub)
	}
	issuer, err := token.NewIssuer(key, previous, cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTTL)
	if err != nil {
		return nil, err
	}
	kid, _ := token.KeyID(&key.PublicKey)
	log.Info("signing access tokens", "kid", kid, "previous_keys", len(previous), "ttl", cfg.AccessTTL.String())
	return issuer, nil
}

// bootstrapAdmin creates the first admin from the BOOTSTRAP_ADMIN_* settings when there is no admin yet. It
// never touches an existing account, so it is safe on every start and on every replica.
func bootstrapAdmin(ctx context.Context, cfg config.Config, repo *repository.Repository, log *slog.Logger) error {
	if cfg.BootstrapAdminEmail == "" {
		return nil
	}
	exists, err := repo.AnyAdmin(ctx)
	if err != nil || exists {
		return err
	}
	hash, err := password.Hash(cfg.BootstrapAdminPassword, password.Default)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	u := &model.User{ID: uuid.New(), Email: cfg.BootstrapAdminEmail, Username: cfg.BootstrapAdminUsername,
		DisplayName: cfg.BootstrapAdminUsername, PasswordHash: &hash, Role: model.RoleAdmin, Status: model.StatusActive,
		CreatedAt: now}
	err = repo.CreateUserWithSession(ctx, u, nil, nil)
	if errors.Is(err, repository.ErrEmailTaken) || errors.Is(err, repository.ErrUsernameTaken) {
		// Another replica just did it, or the account exists as a plain user: promote it by hand.
		log.Warn("bootstrap admin not created: the email or username is already taken")
		return nil
	}
	if err == nil {
		log.Info("bootstrap admin created", "user_id", u.ID)
	}
	return err
}

// sessionJanitor deletes sessions that expired or were revoked more than a week ago, with their tokens.
func sessionJanitor(ctx context.Context, repo *repository.Repository, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := repo.DeleteDeadSessions(ctx, time.Now().Add(-7*24*time.Hour))
			if err != nil {
				log.Error("deleting dead sessions failed", "error", err)
			} else if n > 0 {
				log.Info("deleted dead sessions", "count", n)
			}
		}
	}
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
