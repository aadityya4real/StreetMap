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
	if cfg.Auth.Enabled {
		t.Fatal("authentication should remain optional when Supabase configuration is absent")
	}
}

func TestLoadSupabaseAuthConfiguration(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":           "postgres://streetmap:local-only@localhost:5432/streetmap?sslmode=disable",
		"SUPABASE_PROJECT_URL":   "https://test-project.supabase.co",
		"SUPABASE_ISSUER":        "https://test-project.supabase.co/auth/v1",
		"SUPABASE_AUDIENCE":      "streetmap-api-test",
		"SUPABASE_JWT_ALGORITHM": "RS256",
	}
	cfg, err := load(values(base))
	if err != nil {
		t.Fatalf("load optional auth config: %v", err)
	}
	if !cfg.Auth.Enabled || cfg.Auth.SupabaseURL != base["SUPABASE_PROJECT_URL"] || cfg.Auth.Issuer != base["SUPABASE_ISSUER"] || cfg.Auth.Audience != base["SUPABASE_AUDIENCE"] || cfg.Auth.Algorithm != "RS256" {
		t.Fatalf("unexpected auth configuration: %+v", cfg.Auth)
	}
	if cfg.Auth.JWKSCacheFor != 5*time.Minute || cfg.Auth.AuthenticationLeeway != 30*time.Second {
		t.Fatalf("unexpected auth defaults: %+v", cfg.Auth)
	}

	base["SUPABASE_JWKS_CACHE_DURATION"] = "2m"
	base["AUTH_CLOCK_SKEW"] = "0s"
	cfg, err = load(values(base))
	if err != nil {
		t.Fatalf("load explicit auth timing config: %v", err)
	}
	if cfg.Auth.JWKSCacheFor != 2*time.Minute || cfg.Auth.AuthenticationLeeway != 0 {
		t.Fatalf("unexpected explicit auth timings: %+v", cfg.Auth)
	}
}

func TestLoadRejectsPartialOrUnsafeSupabaseAuthConfiguration(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://streetmap:local-only@localhost:5432/streetmap?sslmode=disable",
	}
	tests := []struct {
		name string
		set  map[string]string
		want string
	}{
		{"partial configuration", map[string]string{"SUPABASE_PROJECT_URL": "https://test-project.supabase.co"}, "SUPABASE_ISSUER is required"},
		{"non-HTTPS project URL", map[string]string{"SUPABASE_PROJECT_URL": "http://test-project.supabase.co", "SUPABASE_ISSUER": "https://test-project.supabase.co/auth/v1", "SUPABASE_AUDIENCE": "streetmap-api-test", "SUPABASE_JWT_ALGORITHM": "RS256"}, "HTTPS origin"},
		{"issuer mismatch", map[string]string{"SUPABASE_PROJECT_URL": "https://test-project.supabase.co", "SUPABASE_ISSUER": "https://other-project.supabase.co/auth/v1", "SUPABASE_AUDIENCE": "streetmap-api-test", "SUPABASE_JWT_ALGORITHM": "RS256"}, "must match"},
		{"unsupported algorithm", map[string]string{"SUPABASE_PROJECT_URL": "https://test-project.supabase.co", "SUPABASE_ISSUER": "https://test-project.supabase.co/auth/v1", "SUPABASE_AUDIENCE": "streetmap-api-test", "SUPABASE_JWT_ALGORITHM": "HS256"}, "must be RS256"},
		{"multiple audiences", map[string]string{"SUPABASE_PROJECT_URL": "https://test-project.supabase.co", "SUPABASE_ISSUER": "https://test-project.supabase.co/auth/v1", "SUPABASE_AUDIENCE": "aud-one aud-two", "SUPABASE_JWT_ALGORITHM": "RS256"}, "one exact audience"},
		{"overlong JWKS cache", map[string]string{"SUPABASE_PROJECT_URL": "https://test-project.supabase.co", "SUPABASE_ISSUER": "https://test-project.supabase.co/auth/v1", "SUPABASE_AUDIENCE": "streetmap-api-test", "SUPABASE_JWT_ALGORITHM": "RS256", "SUPABASE_JWKS_CACHE_DURATION": "11m"}, "at most 10m"},
		{"excessive clock skew", map[string]string{"SUPABASE_PROJECT_URL": "https://test-project.supabase.co", "SUPABASE_ISSUER": "https://test-project.supabase.co/auth/v1", "SUPABASE_AUDIENCE": "streetmap-api-test", "SUPABASE_JWT_ALGORITHM": "RS256", "AUTH_CLOCK_SKEW": "3m"}, "between 0s and 2m"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := make(map[string]string, len(base)+len(test.set))
			for key, value := range base {
				values[key] = value
			}
			for key, value := range test.set {
				values[key] = value
			}
			_, err := load(func(key string) (string, bool) {
				value, ok := values[key]
				return value, ok
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("load error = %v, want containing %q", err, test.want)
			}
		})
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
