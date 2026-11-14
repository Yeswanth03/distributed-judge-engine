package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalDefaults(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile != "local" || c.AccessTTL != 10*time.Minute || c.SessionTTL != 30*24*time.Hour || c.SigningKeyPEM != "" {
		t.Fatalf("unexpected defaults %+v", c)
	}
}

func TestOpenShiftNeedsConnectionsAndAKey(t *testing.T) {
	t.Setenv("APP_PROFILE", "openshift")
	_, err := Load()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"DATABASE_URL", "AMQP_URL", "JWT_PRIVATE_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

func TestSecretsFromFiles(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(key, []byte("  PEM-CONTENT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JWT_PRIVATE_KEY_FILE", key)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.SigningKeyPEM != "PEM-CONTENT" {
		t.Fatalf("got %q", c.SigningKeyPEM)
	}
}

func TestRules(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"access token too long": {"ACCESS_TOKEN_TTL": "2h"},
		"partial bootstrap":     {"BOOTSTRAP_ADMIN_EMAIL": "a@b.co"},
		"bad duration":          {"SESSION_TTL": "soon"},
		"unknown profile":       {"APP_PROFILE": "prod"},
	} {
		t.Run(name, func(t *testing.T) {
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSplitPEM(t *testing.T) {
	two := "-----BEGIN PUBLIC KEY-----\nA\n-----END PUBLIC KEY-----\n-----BEGIN PUBLIC KEY-----\nB\n-----END PUBLIC KEY-----\n"
	if got := splitPEM(two); len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
	if got := splitPEM(""); len(got) != 0 {
		t.Fatalf("expected none, got %v", got)
	}
}
