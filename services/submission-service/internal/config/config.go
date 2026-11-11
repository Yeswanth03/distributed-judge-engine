// Package config reads the service's settings from environment variables.
//
// Like Catalog's Spring profiles: APP_PROFILE=local (the default) fills in values that work against the
// docker-compose stack; APP_PROFILE=openshift has no fallbacks for connection settings, so a missing Secret
// stops startup instead of quietly connecting to localhost.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Profile  string
	HTTPAddr string
	LogLevel string

	DatabaseURL string
	AMQPURL     string

	OutboxPollInterval time.Duration
	OutboxBatchSize    int
	OutboxMaxRetries   int
	OutboxRetention    time.Duration // how long PUBLISHED rows are kept

	ReaperInterval       time.Duration
	ReaperQueuedTimeout  time.Duration // generous: a contest burst can leave jobs queued for a while
	ReaperRunningTimeout time.Duration

	RunsPerMinute int           // per user
	RunTimeout    time.Duration // a run with no result after this is FAILED
	RunRetention  time.Duration // how long runs are kept

	MaxCodeBytes     int
	AllowedLanguages []string
	CORSOrigin       string // empty disables CORS headers (the gateway serves the frontend)

	ShutdownTimeout time.Duration
}

// Load reads the configuration, collecting every problem into one error.
func Load() (Config, error) {
	r := reader{}
	profile := r.str("APP_PROFILE", "local")
	local := profile == "local"
	if !local && profile != "openshift" {
		return Config{}, fmt.Errorf("APP_PROFILE must be local or openshift, got %q", profile)
	}
	// Connection settings have defaults only in the local profile.
	conn := func(key, localDefault string) string {
		if local {
			return r.str(key, localDefault)
		}
		return r.required(key)
	}

	c := Config{
		Profile:  profile,
		HTTPAddr: r.str("HTTP_ADDR", ":8080"),
		LogLevel: r.str("LOG_LEVEL", "info"),

		DatabaseURL: conn("DATABASE_URL", "postgres://submission:submission@localhost:5432/submission?sslmode=disable"),
		AMQPURL:     conn("AMQP_URL", "amqp://judge:judge@localhost:5672/"),

		OutboxPollInterval: r.duration("OUTBOX_POLL_INTERVAL", 250*time.Millisecond),
		OutboxBatchSize:    r.integer("OUTBOX_BATCH_SIZE", 50),
		OutboxMaxRetries:   r.integer("OUTBOX_MAX_RETRIES", 10),
		OutboxRetention:    r.duration("OUTBOX_RETENTION", 24*time.Hour),

		ReaperInterval:       r.duration("REAPER_INTERVAL", 30*time.Second),
		ReaperQueuedTimeout:  r.duration("REAPER_QUEUED_TIMEOUT", 30*time.Minute),
		ReaperRunningTimeout: r.duration("REAPER_RUNNING_TIMEOUT", 5*time.Minute),

		RunsPerMinute: r.integer("RUNS_PER_MINUTE", 10),
		RunTimeout:    r.duration("RUN_TIMEOUT", 2*time.Minute),
		RunRetention:  r.duration("RUN_RETENTION", 24*time.Hour),

		MaxCodeBytes:     r.integer("MAX_CODE_BYTES", 64*1024),
		AllowedLanguages: r.list("ALLOWED_LANGUAGES", "python"),
		CORSOrigin:       r.str("CORS_ORIGIN", ""),

		ShutdownTimeout: r.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}
	if c.OutboxBatchSize < 1 || c.OutboxMaxRetries < 1 || c.MaxCodeBytes < 1 || c.RunsPerMinute < 1 {
		r.errs = append(r.errs, "OUTBOX_BATCH_SIZE, OUTBOX_MAX_RETRIES, MAX_CODE_BYTES and RUNS_PER_MINUTE must be positive")
	}
	if len(c.AllowedLanguages) == 0 {
		r.errs = append(r.errs, "ALLOWED_LANGUAGES must name at least one language")
	}
	if len(r.errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %s", strings.Join(r.errs, "; "))
	}
	return c, nil
}

type reader struct {
	errs []string
}

func (r *reader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (r *reader) required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		r.errs = append(r.errs, key+" is required")
	}
	return v
}

func (r *reader) integer(key string, def int) int {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Sprintf("%s: %q is not an integer", key, v))
	}
	return n
}

func (r *reader) duration(key string, def time.Duration) time.Duration {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		r.errs = append(r.errs, fmt.Sprintf("%s: %q is not a positive duration like 250ms or 5m", key, v))
	}
	return d
}

func (r *reader) list(key, def string) []string {
	var out []string
	for _, part := range strings.Split(r.str(key, def), ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
