package tests

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/devseed"
	"streetmap.in/backend/internal/identity"
	"streetmap.in/backend/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestPostgresIdentityResolutionAndProvisioning(t *testing.T) {
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

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open dedicated identity test database")
	}
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatal("connect to dedicated identity test database")
	}

	schema := fmt.Sprintf("streetmap_auth_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("create isolated identity test schema")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("remove isolated identity test schema")
		}
	})

	scopedURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse test database URL")
	}
	query := scopedURL.Query()
	query.Set("options", "-csearch_path="+schema+",public")
	scopedURL.RawQuery = query.Encode()
	db, err := sql.Open("pgx", scopedURL.String())
	if err != nil {
		t.Fatal("open schema-scoped identity test database")
	}
	db.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("connect to schema-scoped identity test database")
	}

	goose.SetTableName(schema + ".goose_db_version")
	t.Cleanup(func() { goose.SetTableName(goose.DefaultTablename) })
	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("configure Goose PostgreSQL dialect")
	}
	if err := goose.UpToContext(ctx, db, ".", 3); err != nil {
		t.Fatal("apply migrations in isolated identity test schema")
	}

	const demoUserID = "10000000-0000-4000-8000-000000000001"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1::uuid, 'demo@example.invalid', 'Demo User')`, demoUserID); err != nil {
		t.Fatal("insert demo user fixture")
	}
	var locationID, noteID string
	if err := db.QueryRowContext(ctx, `INSERT INTO locations (location_type, provider, provider_place_id, display_label) VALUES ('verified_place', 'fixture', 'identity-test', 'Identity test') RETURNING id::text`).Scan(&locationID); err != nil {
		t.Fatal("insert ownership fixture location")
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO notes (author_user_id, location_id, title, body, category) VALUES ($1::uuid, $2::uuid, 'Fixture note', 'Existing note ownership remains unchanged', 'local_tip') RETURNING id::text`, demoUserID, locationID).Scan(&noteID); err != nil {
		t.Fatal("insert ownership fixture note")
	}
	resolver := identity.NewPostgresResolver(db)
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_identities (user_id, issuer, subject) VALUES ($1::uuid, 'https://existing.test/auth/v1', 'already-linked')`, demoUserID); err != nil {
		t.Fatal("insert existing identity fixture")
	}
	if existing, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://existing.test/auth/v1", Subject: "already-linked"}); err != nil || existing.UserID != demoUserID {
		t.Fatalf("existing identity resolved to %+v, err=%v; want original internal user", existing, err)
	}

	t.Run("first login provisions once and ignores matching email", func(t *testing.T) {
		principal, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://issuer.test/auth/v1", Subject: "external-subject", DisplayName: "New Member"})
		if err != nil {
			t.Fatalf("resolve new identity: %v", err)
		}
		again, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://issuer.test/auth/v1", Subject: "external-subject", DisplayName: "Different Display Name"})
		if err != nil || again.UserID != principal.UserID {
			t.Fatalf("repeated resolution = %+v, %v; want same principal %+v", again, err, principal)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_identities WHERE issuer = $1 AND subject = $2 AND user_id = $3::uuid`, "https://issuer.test/auth/v1", "external-subject", principal.UserID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("identity rows = %d, err=%v; want one", count, err)
		}
		var localCount int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE email = 'demo@example.invalid'`).Scan(&localCount); err != nil || localCount != 1 {
			t.Fatalf("matching demo email rows = %d, err=%v; user was changed or merged", localCount, err)
		}
		var name string
		if err := db.QueryRowContext(ctx, `SELECT display_name FROM users WHERE id = $1::uuid`, principal.UserID).Scan(&name); err != nil || name != "New Member" {
			t.Fatalf("provisioned name = %q, err=%v", name, err)
		}
	})

	t.Run("same subject under another issuer is distinct", func(t *testing.T) {
		first, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://issuer-one.test/auth/v1", Subject: "shared-subject"})
		if err != nil {
			t.Fatal("resolve first issuer")
		}
		second, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://issuer-two.test/auth/v1", Subject: "shared-subject"})
		if err != nil || first.UserID == second.UserID {
			t.Fatalf("issuer-scoped identities collided: first=%+v second=%+v err=%v", first, second, err)
		}
	})

	t.Run("concurrent first login is idempotent", func(t *testing.T) {
		const workers = 12
		principals := make([]auth.Principal, workers)
		errors := make([]error, workers)
		var wait sync.WaitGroup
		for index := range workers {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				principals[index], errors[index] = resolver.Resolve(ctx, auth.Identity{Issuer: "https://concurrent.test/auth/v1", Subject: "one-subject", DisplayName: "Concurrent Member"})
			}(index)
		}
		wait.Wait()
		for _, err := range errors {
			if err != nil {
				t.Fatalf("concurrent resolution failed: %v", err)
			}
		}
		for _, principal := range principals[1:] {
			if principal.UserID != principals[0].UserID {
				t.Fatalf("concurrent identities resolved to multiple users: %+v", principals)
			}
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_identities WHERE issuer = $1 AND subject = $2`, "https://concurrent.test/auth/v1", "one-subject").Scan(&count); err != nil || count != 1 {
			t.Fatalf("concurrent identity mappings = %d, err=%v; want one", count, err)
		}
	})

	t.Run("safe fallback name and account status checks", func(t *testing.T) {
		fallback, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://fallback.test/auth/v1", Subject: "no-name", DisplayName: "\n"})
		if err != nil {
			t.Fatal("resolve identity without safe name")
		}
		var name string
		if err := db.QueryRowContext(ctx, `SELECT display_name FROM users WHERE id = $1::uuid`, fallback.UserID).Scan(&name); err != nil || name != "StreetMap member" {
			t.Fatalf("fallback name = %q, err=%v", name, err)
		}
		for _, status := range []string{"suspended", "deleted"} {
			principal, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://status.test/auth/v1", Subject: status})
			if err != nil {
				t.Fatalf("provision %s fixture: %v", status, err)
			}
			if _, err := db.ExecContext(ctx, `UPDATE users SET account_status = $1 WHERE id = $2::uuid`, status, principal.UserID); err != nil {
				t.Fatalf("set %s status: %v", status, err)
			}
			if _, err := resolver.Resolve(ctx, auth.Identity{Issuer: "https://status.test/auth/v1", Subject: status}); err != auth.ErrAccountUnavailable {
				t.Fatalf("resolve %s account error = %v", status, err)
			}
		}
	})

	var retained int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE id = $1::uuid AND author_user_id = $2::uuid`, noteID, demoUserID).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("existing demo note ownership count=%d err=%v", retained, err)
	}
}
