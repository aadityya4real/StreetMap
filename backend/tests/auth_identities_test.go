package tests

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"streetmap.in/backend/internal/devseed"
	"streetmap.in/backend/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestAuthIdentityMigrationPreservesUsersAndOwnership(t *testing.T) {
	dsn := os.Getenv("STREETMAP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set STREETMAP_TEST_DATABASE_URL to a dedicated local database ending in _test")
	}
	if _, err := devseed.ValidateTarget("test", dsn); err != nil {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must target a local dedicated _test database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.Trim(parsed.Path, "/"), "_test") {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must target a dedicated database ending in _test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open dedicated migration test database")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("close dedicated migration test database")
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("connect to dedicated migration test database")
	}

	// Use a unique schema within the dedicated test database so lifecycle checks
	// never roll back or drop objects in its normal public schema.
	schema := fmt.Sprintf("streetmap_identity_test_%d", time.Now().UnixNano())
	gooseTable := schema + ".goose_db_version"
	goose.SetTableName(gooseTable)
	t.Cleanup(func() { goose.SetTableName(goose.DefaultTablename) })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("create isolated migration test schema")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, "SET search_path TO public"); err != nil {
			t.Error("restore test database search path")
		}
		if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("remove isolated migration test schema")
		}
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatal("select isolated migration test schema")
	}

	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("configure Goose PostgreSQL dialect")
	}
	if err := goose.UpToContext(ctx, db, ".", 2); err != nil {
		t.Fatal("apply baseline migrations in isolated schema")
	}

	const (
		seedAuthorID = "10000000-0000-4000-8000-000000000001"
		seedVoterID  = "10000000-0000-4000-8000-000000000002"
		seedReporter = "10000000-0000-4000-8000-000000000003"
	)
	for _, item := range []struct{ id, email, name string }{
		{seedAuthorID, "demo-author-one@streetmap.invalid", "StreetMap Demo Contributor One"},
		{seedVoterID, "demo-author-two@streetmap.invalid", "StreetMap Demo Contributor Two"},
		{seedReporter, "demo-reader@streetmap.invalid", "StreetMap Demo Reader"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1::uuid, $2, $3)`, item.id, item.email, item.name); err != nil {
			t.Fatal("insert pre-migration demo user fixture")
		}
	}
	var locationID, noteID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO locations (location_type, provider, provider_place_id, display_label)
		VALUES ('verified_place', 'test_provider', 'identity-migration-fixture', 'Identity migration fixture')
		RETURNING id::text`).Scan(&locationID); err != nil {
		t.Fatal("insert pre-migration location fixture")
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO notes (author_user_id, location_id, title, body, category)
		VALUES ($1::uuid, $2::uuid, 'Migration fixture', 'Fixture note body', 'local_tip')
		RETURNING id::text`, seedAuthorID, locationID).Scan(&noteID); err != nil {
		t.Fatal("insert pre-migration note fixture")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO votes (note_id, user_id) VALUES ($1::uuid, $2::uuid)`, noteID, seedVoterID); err != nil {
		t.Fatal("insert pre-migration vote fixture")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO reports (reporter_user_id, reported_note_id, reason)
		VALUES ($1::uuid, $2::uuid, 'Migration fixture report')`, seedReporter, noteID); err != nil {
		t.Fatal("insert pre-migration report fixture")
	}

	if err := goose.UpToContext(ctx, db, ".", 3); err != nil {
		t.Fatal("apply identity migration")
	}

	for _, item := range []struct{ id, email, name string }{
		{seedAuthorID, "demo-author-one@streetmap.invalid", "StreetMap Demo Contributor One"},
		{seedVoterID, "demo-author-two@streetmap.invalid", "StreetMap Demo Contributor Two"},
		{seedReporter, "demo-reader@streetmap.invalid", "StreetMap Demo Reader"},
	} {
		var email, name string
		if err := db.QueryRowContext(ctx, `SELECT email, display_name FROM users WHERE id = $1::uuid`, item.id).Scan(&email, &name); err != nil {
			t.Fatal("read preserved user after migration")
		}
		if email != item.email || name != item.name {
			t.Fatalf("migration changed existing user %s fields", item.id)
		}
	}
	assertNullableEmail(t, ctx, db, true)
	assertTableExists(t, ctx, db, schema, "auth_identities", true)
	assertIndexExists(t, ctx, db, schema, "auth_identities_by_user")

	var ownershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM notes WHERE id = $1::uuid AND author_user_id = $2::uuid) +
			(SELECT count(*) FROM votes WHERE note_id = $1::uuid AND user_id = $3::uuid) +
			(SELECT count(*) FROM reports WHERE reported_note_id = $1::uuid AND reporter_user_id = $4::uuid)`,
		noteID, seedAuthorID, seedVoterID, seedReporter).Scan(&ownershipCount); err != nil {
		t.Fatal("check ownership links after migration")
	}
	if ownershipCount != 3 {
		t.Fatalf("migration did not preserve all ownership relationships: got %d", ownershipCount)
	}

	// Existing demo accounts are deliberately left without identity mappings.
	var linkedDemoUsers int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_identities WHERE user_id IN ($1::uuid, $2::uuid, $3::uuid)`, seedAuthorID, seedVoterID, seedReporter).Scan(&linkedDemoUsers); err != nil {
		t.Fatal("check demo identity mappings")
	}
	if linkedDemoUsers != 0 {
		t.Fatalf("migration linked %d existing demo users", linkedDemoUsers)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_identities (user_id, issuer, subject)
		VALUES ($1::uuid, 'https://issuer-one.example', 'same-subject'),
		       ($1::uuid, 'https://issuer-two.example', 'same-subject'),
		       ($1::uuid, 'https://issuer-one.example', 'second-subject')`, seedAuthorID); err != nil {
		t.Fatal("insert multiple identity mappings")
	}
	assertRejected(t, "duplicate issuer and subject", func() error {
		_, err := db.ExecContext(ctx, `INSERT INTO auth_identities (user_id, issuer, subject) VALUES ($1::uuid, 'https://issuer-one.example', 'same-subject')`, seedVoterID)
		return err
	})
	assertRejected(t, "identity for missing user", func() error {
		_, err := db.ExecContext(ctx, `INSERT INTO auth_identities (user_id, issuer, subject) VALUES (gen_random_uuid(), 'https://issuer-three.example', 'unknown-user')`)
		return err
	})
	assertRejected(t, "identity without an issuer", func() error {
		_, err := db.ExecContext(ctx, `INSERT INTO auth_identities (user_id, issuer, subject) VALUES ($1::uuid, NULL, 'missing-issuer')`, seedVoterID)
		return err
	})
	assertRejected(t, "identity with a blank subject", func() error {
		_, err := db.ExecContext(ctx, `INSERT INTO auth_identities (user_id, issuer, subject) VALUES ($1::uuid, 'https://issuer-three.example', '   ')`, seedVoterID)
		return err
	})
	assertRejected(t, "delete referenced user", func() error {
		_, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1::uuid`, seedAuthorID)
		return err
	})

	for _, displayName := range []string{"Null Email One", "Null Email Two"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (email, display_name) VALUES (NULL, $1)`, displayName); err != nil {
			t.Fatalf("insert %q: nullable email was rejected", displayName)
		}
	}
	assertRejected(t, "duplicate non-null normalized email", func() error {
		_, err := db.ExecContext(ctx, `INSERT INTO users (email, display_name) VALUES (' DEMO-AUTHOR-ONE@STREETMAP.INVALID ', 'Duplicate Email')`)
		return err
	})

	// Rollback must fail clearly while NULL values exist and leave migration 3 intact.
	err = goose.DownContext(ctx, db, ".")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "cannot restore users.email not null") {
		t.Fatalf("rollback with NULL emails should fail clearly; got error=%v", err)
	}
	assertTableExists(t, ctx, db, schema, "auth_identities", true)
	assertNullableEmail(t, ctx, db, true)

	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE display_name IN ('Null Email One', 'Null Email Two')`); err != nil {
		t.Fatal("remove only nullable-email rollback fixtures")
	}
	if err := goose.DownContext(ctx, db, "."); err != nil {
		t.Fatal("rollback identity migration after NULL fixtures are removed")
	}
	assertTableExists(t, ctx, db, schema, "auth_identities", false)
	assertNullableEmail(t, ctx, db, false)

	if err := goose.UpToContext(ctx, db, ".", 3); err != nil {
		t.Fatal("reapply identity migration")
	}
	assertTableExists(t, ctx, db, schema, "auth_identities", true)
	assertNullableEmail(t, ctx, db, true)
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_identities`).Scan(&linkedDemoUsers); err != nil {
		t.Fatal("check identity table after migration reapply")
	}
	if linkedDemoUsers != 0 {
		t.Fatalf("reapplying migration unexpectedly created %d identity rows", linkedDemoUsers)
	}
}

func assertNullableEmail(t *testing.T, ctx context.Context, db *sql.DB, nullable bool) {
	t.Helper()
	var isNotNull bool
	if err := db.QueryRowContext(ctx, `
		SELECT a.attnotnull
		FROM pg_attribute a
		WHERE a.attrelid = 'users'::regclass AND a.attname = 'email' AND NOT a.attisdropped`).Scan(&isNotNull); err != nil {
		t.Fatal("inspect users.email nullability")
	}
	if isNotNull == nullable {
		t.Fatalf("users.email nullable = %t, want %t", !isNotNull, nullable)
	}
}

func assertTableExists(t *testing.T, ctx context.Context, db *sql.DB, schema, table string, exists bool) {
	t.Helper()
	var actual bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&actual); err != nil {
		t.Fatalf("inspect table %s", table)
	}
	if actual != exists {
		t.Fatalf("table %s exists = %t, want %t", table, actual, exists)
	}
}

func assertIndexExists(t *testing.T, ctx context.Context, db *sql.DB, schema, index string) {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+index).Scan(&exists); err != nil {
		t.Fatalf("inspect index %s", index)
	}
	if !exists {
		t.Fatalf("index %s does not exist", index)
	}
}

func assertRejected(t *testing.T, description string, operation func() error) {
	t.Helper()
	if err := operation(); err == nil {
		t.Fatalf("database accepted %s", description)
	}
}
