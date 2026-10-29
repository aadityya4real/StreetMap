package tests

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"streetmap.in/backend/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMigrationsApplyToDedicatedPostgresDatabase(t *testing.T) {
	dsn := os.Getenv("STREETMAP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set STREETMAP_TEST_DATABASE_URL to run PostgreSQL migration integration test")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must be a valid PostgreSQL URL")
	}
	if !strings.HasSuffix(strings.Trim(parsed.Path, "/"), "_test") {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must point to a dedicated database whose name ends in _test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	if err := migrations.Up(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	for _, table := range []string{"users", "locations", "notes", "votes", "reports"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("migration did not create table %s", table)
		}
	}

	var postgisEnabled bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'postgis')`).Scan(&postgisEnabled); err != nil {
		t.Fatalf("check PostGIS extension: %v", err)
	}
	if !postgisEnabled {
		t.Error("migration did not enable PostGIS")
	}
}
