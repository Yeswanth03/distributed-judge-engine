package config

import (
	"strings"
	"testing"
	"time"
)

func TestLocalDefaults(t *testing.T) {
	t.Setenv("APP_PROFILE", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile != "local" || !strings.Contains(c.DatabaseURL, "localhost") || c.OutboxPollInterval != 250*time.Millisecond {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if len(c.AllowedLanguages) != 1 || c.AllowedLanguages[0] != "python" {
		t.Fatalf("languages = %v", c.AllowedLanguages)
	}
}

func TestOpenShiftRequiresConnectionSettings(t *testing.T) {
	t.Setenv("APP_PROFILE", "openshift")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("AMQP_URL", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") || !strings.Contains(err.Error(), "AMQP_URL is required") {
		t.Fatalf("err = %v, want both connection settings reported", err)
	}

	t.Setenv("DATABASE_URL", "postgres://db/submission")
	t.Setenv("AMQP_URL", "amqp://mq/")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidValuesAreReported(t *testing.T) {
	t.Setenv("APP_PROFILE", "local")
	t.Setenv("OUTBOX_POLL_INTERVAL", "fast")
	t.Setenv("OUTBOX_BATCH_SIZE", "ten")
	t.Setenv("ALLOWED_LANGUAGES", " python , cpp ,")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "OUTBOX_POLL_INTERVAL") || !strings.Contains(err.Error(), "OUTBOX_BATCH_SIZE") {
		t.Fatalf("err = %v", err)
	}

	t.Setenv("OUTBOX_POLL_INTERVAL", "")
	t.Setenv("OUTBOX_BATCH_SIZE", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.AllowedLanguages, "|") != "python|cpp" {
		t.Fatalf("languages = %q", c.AllowedLanguages)
	}
}

func TestUnknownProfile(t *testing.T) {
	t.Setenv("APP_PROFILE", "prod")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error")
	}
}
