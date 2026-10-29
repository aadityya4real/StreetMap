package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(values(map[string]string{
		"DATABASE_URL": "postgres://streetmap:local-only@localhost:5432/streetmap?sslmode=disable",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" || cfg.DBMaxOpenConns != 10 || cfg.DBMaxIdleConns != 5 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.HTTPReadTimeout != 10*time.Second || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected timeout defaults: %+v", cfg)
	}
}

func TestLoadRejectsMissingOrInvalidRequiredConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"missing database url", map[string]string{}, "DATABASE_URL is required"},
		{"invalid scheme", map[string]string{"DATABASE_URL": "https://localhost/streetmap"}, "valid PostgreSQL URL"},
		{"missing database name", map[string]string{"DATABASE_URL": "postgres://localhost/"}, "valid PostgreSQL URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(values(tt.values))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("load error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidPoolAndDurationSettings(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":      "postgres://streetmap:local-only@localhost:5432/streetmap",
		"DB_MAX_OPEN_CONNS": "2",
		"DB_MAX_IDLE_CONNS": "3",
	}
	if _, err := load(values(base)); err == nil || !strings.Contains(err.Error(), "cannot exceed") {
		t.Fatalf("expected idle-pool validation error, got %v", err)
	}

	base["DB_MAX_IDLE_CONNS"] = "1"
	base["HTTP_READ_TIMEOUT"] = "0s"
	if _, err := load(values(base)); err == nil || !strings.Contains(err.Error(), "positive duration") {
		t.Fatalf("expected duration validation error, got %v", err)
	}
}

func values(source map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := source[key]
		return value, ok
	}
}
