// Package config reads the User Service's settings from the environment (12-factor). The local profile has
// working defaults for docker-compose; the openshift profile requires every connection setting and the
// signing key to be set explicitly.
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

	// Tokens (docs/contracts.md section 7).
	JWTIssuer   string
	JWTAudience string
	AccessTTL   time.Duration
	SessionTTL  time.Duration
	// PEM of the ECDSA P-256 signing key. Empty means: use the key stored in the database (local profile only).
	SigningKeyPEM string
	// PEM public keys of earlier signing keys, kept in the JWKS until their tokens have expired.
	PreviousPublicKeysPEM []string

	MaxFailedLogins int
	LoginLockout    time.Duration

	// The first admin, created at startup if there is no admin yet. All three or none.
	BootstrapAdminEmail    string
	BootstrapAdminUsername string
	BootstrapAdminPassword string

	OutboxPollInterval time.Duration
	OutboxBatchSize    int
	OutboxMaxRetries   int
	OutboxRetention    time.Duration

	CORSOrigin      string
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

		DatabaseURL: conn("DATABASE_URL", "postgres://users:users@localhost:5432/users?sslmode=disable"),
		AMQPURL:     conn("AMQP_URL", "amqp://judge:judge@localhost:5672/"),

		JWTIssuer:   r.str("JWT_ISSUER", "judge-engine-user-service"),
		JWTAudience: r.str("JWT_AUDIENCE", "judge-engine"),
		AccessTTL:   r.duration("ACCESS_TOKEN_TTL", 10*time.Minute),
		SessionTTL:  r.duration("SESSION_TTL", 30*24*time.Hour),

		SigningKeyPEM:         r.fileOrValue("JWT_PRIVATE_KEY"),
		PreviousPublicKeysPEM: splitPEM(r.fileOrValue("JWT_PREVIOUS_PUBLIC_KEYS")),

		MaxFailedLogins: r.integer("LOGIN_MAX_FAILURES", 5),
		LoginLockout:    r.duration("LOGIN_LOCKOUT", 15*time.Minute),

		BootstrapAdminEmail:    r.str("BOOTSTRAP_ADMIN_EMAIL", ""),
		BootstrapAdminUsername: r.str("BOOTSTRAP_ADMIN_USERNAME", ""),
		BootstrapAdminPassword: r.fileOrValue("BOOTSTRAP_ADMIN_PASSWORD"),

		OutboxPollInterval: r.duration("OUTBOX_POLL_INTERVAL", 250*time.Millisecond),
		OutboxBatchSize:    r.integer("OUTBOX_BATCH_SIZE", 50),
		OutboxMaxRetries:   r.integer("OUTBOX_MAX_RETRIES", 10),
		OutboxRetention:    r.duration("OUTBOX_RETENTION", 24*time.Hour),

		CORSOrigin:      r.str("CORS_ORIGIN", ""),
		ShutdownTimeout: r.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}
	if !local && c.SigningKeyPEM == "" {
		r.errs = append(r.errs, "JWT_PRIVATE_KEY (or JWT_PRIVATE_KEY_FILE) is required outside the local profile")
	}
	if c.AccessTTL > time.Hour {
		r.errs = append(r.errs, "ACCESS_TOKEN_TTL must be at most 1h: an access token cannot be revoked before it expires")
	}
	if c.MaxFailedLogins < 1 || c.OutboxBatchSize < 1 || c.OutboxMaxRetries < 1 {
		r.errs = append(r.errs, "LOGIN_MAX_FAILURES, OUTBOX_BATCH_SIZE and OUTBOX_MAX_RETRIES must be positive")
	}
	set := 0
	for _, v := range []string{c.BootstrapAdminEmail, c.BootstrapAdminUsername, c.BootstrapAdminPassword} {
		if v != "" {
			set++
		}
	}
	if set != 0 && set != 3 {
		r.errs = append(r.errs, "BOOTSTRAP_ADMIN_EMAIL, BOOTSTRAP_ADMIN_USERNAME and BOOTSTRAP_ADMIN_PASSWORD go together")
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

// fileOrValue reads KEY, or the file named by KEY_FILE: secrets are usually mounted as files on OpenShift.
func (r *reader) fileOrValue(key string) string {
	if path := os.Getenv(key + "_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			r.errs = append(r.errs, fmt.Sprintf("%s_FILE: %v", key, err))
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return os.Getenv(key)
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
		r.errs = append(r.errs, fmt.Sprintf("%s: %q is not a positive duration like 10m or 720h", key, v))
	}
	return d
}

// splitPEM splits concatenated PEM blocks into one string each.
func splitPEM(all string) []string {
	var out []string
	for _, part := range strings.SplitAfter(all, "-----END PUBLIC KEY-----") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
