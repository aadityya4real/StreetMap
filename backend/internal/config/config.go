package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"streetmap.in/backend/internal/auth"
)

type Config struct {
	DatabaseURL           string
	HTTPAddr              string
	LogLevel              string
	DBMaxOpenConns        int
	DBMaxIdleConns        int
	DBConnMaxLifetime     time.Duration
	DBConnMaxIdleTime     time.Duration
	HTTPReadHeaderTimeout time.Duration
	HTTPReadTimeout       time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration
	ReadinessTimeout      time.Duration
	ShutdownTimeout       time.Duration
	Auth                  AuthConfig
}

type AuthConfig struct {
	Enabled              bool
	SupabaseURL          string
	Issuer               string
	Audience             string
	Algorithm            string
	JWKSCacheFor         time.Duration
	AuthenticationLeeway time.Duration
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	var cfg Config
	var err error

	cfg.DatabaseURL, _ = lookup("DATABASE_URL")
	if err := validateDatabaseURL(cfg.DatabaseURL); err != nil {
		return Config{}, err
	}

	cfg.HTTPAddr = valueOrDefault(lookup, "HTTP_ADDR", "127.0.0.1:8080")
	if err := validateHTTPAddr(cfg.HTTPAddr); err != nil {
		return Config{}, err
	}

	cfg.LogLevel = strings.ToLower(valueOrDefault(lookup, "LOG_LEVEL", "info"))
	if cfg.LogLevel != "debug" && cfg.LogLevel != "info" && cfg.LogLevel != "warn" && cfg.LogLevel != "error" {
		return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}

	if cfg.DBMaxOpenConns, err = intValue(lookup, "DB_MAX_OPEN_CONNS", 10, 1); err != nil {
		return Config{}, err
	}
	if cfg.DBMaxIdleConns, err = intValue(lookup, "DB_MAX_IDLE_CONNS", 5, 0); err != nil {
		return Config{}, err
	}
	if cfg.DBMaxIdleConns > cfg.DBMaxOpenConns {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}

	durationFields := []struct {
		name     string
		fallback time.Duration
		target   *time.Duration
	}{
		{"DB_CONN_MAX_LIFETIME", 30 * time.Minute, &cfg.DBConnMaxLifetime},
		{"DB_CONN_MAX_IDLE_TIME", 5 * time.Minute, &cfg.DBConnMaxIdleTime},
		{"HTTP_READ_HEADER_TIMEOUT", 5 * time.Second, &cfg.HTTPReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", 10 * time.Second, &cfg.HTTPReadTimeout},
		{"HTTP_WRITE_TIMEOUT", 15 * time.Second, &cfg.HTTPWriteTimeout},
		{"HTTP_IDLE_TIMEOUT", 60 * time.Second, &cfg.HTTPIdleTimeout},
		{"READINESS_TIMEOUT", 2 * time.Second, &cfg.ReadinessTimeout},
		{"SHUTDOWN_TIMEOUT", 10 * time.Second, &cfg.ShutdownTimeout},
	}
	for _, field := range durationFields {
		*field.target, err = durationValue(lookup, field.name, field.fallback)
		if err != nil {
			return Config{}, err
		}
	}
	if cfg.Auth, err = authConfig(lookup); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func authConfig(lookup func(string) (string, bool)) (AuthConfig, error) {
	keys := []string{
		"SUPABASE_PROJECT_URL", "SUPABASE_ISSUER", "SUPABASE_AUDIENCE", "SUPABASE_JWT_ALGORITHM",
		"SUPABASE_JWKS_CACHE_DURATION", "AUTH_CLOCK_SKEW",
	}
	values := make(map[string]string, len(keys))
	anySet := false
	for _, key := range keys {
		value, exists := lookup(key)
		if exists && strings.TrimSpace(value) != "" {
			anySet = true
		}
		values[key] = strings.TrimSpace(value)
	}
	if !anySet {
		return AuthConfig{}, nil
	}
	for _, key := range keys[:4] {
		if values[key] == "" {
			return AuthConfig{}, fmt.Errorf("%s is required when Supabase authentication is configured", key)
		}
	}
	if err := validateSupabaseURLs(values["SUPABASE_PROJECT_URL"], values["SUPABASE_ISSUER"]); err != nil {
		return AuthConfig{}, err
	}
	if strings.ContainsAny(values["SUPABASE_AUDIENCE"], " \t\r\n") {
		return AuthConfig{}, fmt.Errorf("SUPABASE_AUDIENCE must be one exact audience value")
	}
	switch values["SUPABASE_JWT_ALGORITHM"] {
	case "RS256", "ES256", "EdDSA":
	default:
		return AuthConfig{}, fmt.Errorf("SUPABASE_JWT_ALGORITHM must be RS256, ES256, or EdDSA")
	}

	cacheFor := 5 * time.Minute
	if values["SUPABASE_JWKS_CACHE_DURATION"] != "" {
		parsed, err := time.ParseDuration(values["SUPABASE_JWKS_CACHE_DURATION"])
		if err != nil || parsed <= 0 || parsed > 10*time.Minute {
			return AuthConfig{}, fmt.Errorf("SUPABASE_JWKS_CACHE_DURATION must be greater than 0 and at most 10m")
		}
		cacheFor = parsed
	}
	leeway := 30 * time.Second
	if values["AUTH_CLOCK_SKEW"] != "" {
		parsed, err := time.ParseDuration(values["AUTH_CLOCK_SKEW"])
		if err != nil || parsed < 0 || parsed > 2*time.Minute {
			return AuthConfig{}, fmt.Errorf("AUTH_CLOCK_SKEW must be between 0s and 2m")
		}
		leeway = parsed
	}
	return AuthConfig{
		Enabled:              true,
		SupabaseURL:          values["SUPABASE_PROJECT_URL"],
		Issuer:               values["SUPABASE_ISSUER"],
		Audience:             values["SUPABASE_AUDIENCE"],
		Algorithm:            values["SUPABASE_JWT_ALGORITHM"],
		JWKSCacheFor:         cacheFor,
		AuthenticationLeeway: leeway,
	}, nil
}

func validateSupabaseURLs(projectURL, issuer string) error {
	if err := auth.ValidateSupabaseProjectURL(projectURL); err != nil {
		return fmt.Errorf("SUPABASE_PROJECT_URL must be a canonical hosted Supabase URL: %w", err)
	}
	issuerURL, issuerErr := url.Parse(issuer)
	if issuerErr != nil || issuerURL.Scheme != "https" || issuerURL.Hostname() == "" || issuerURL.User != nil || issuerURL.RawQuery != "" || issuerURL.Fragment != "" || issuerURL.Path == "" {
		return fmt.Errorf("SUPABASE_ISSUER must be an HTTPS issuer URL")
	}
	expectedIssuer := strings.TrimRight(projectURL, "/") + "/auth/v1"
	if issuer != expectedIssuer {
		return fmt.Errorf("SUPABASE_ISSUER must match the configured project's Supabase Auth issuer")
	}
	return nil
}

func validateDatabaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || strings.Trim(parsed.Path, "/") == "" {
		return fmt.Errorf("DATABASE_URL must be a valid PostgreSQL URL with a host and database name")
	}
	return nil
}

func validateHTTPAddr(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("HTTP_ADDR must be a host:port address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return fmt.Errorf("HTTP_ADDR port must be between 0 and 65535")
	}
	return nil
}

func intValue(lookup func(string) (string, bool), key string, fallback, minimum int) (int, error) {
	value := valueOrDefault(lookup, key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum {
		return 0, fmt.Errorf("%s must be an integer of at least %d", key, minimum)
	}
	return parsed, nil
}

func durationValue(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	value := valueOrDefault(lookup, key, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func valueOrDefault(lookup func(string) (string, bool), key, fallback string) string {
	value, exists := lookup(key)
	if !exists || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
