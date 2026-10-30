package devseed

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"streetmap.in/backend/internal/notes"
	"streetmap.in/backend/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		databaseURL string
		wantError   bool
	}{
		{"local development database", "development", "postgres://streetmap:local-password@127.0.0.1:54329/streetmap?sslmode=disable", false},
		{"local test database", "test", "postgres://streetmap:local-password@localhost:54329/streetmap_test?sslmode=disable", false},
		{"production environment", "production", "postgres://streetmap:local-password@127.0.0.1:54329/streetmap?sslmode=disable", true},
		{"remote development database", "development", "postgres://streetmap:local-password@example.com:5432/streetmap?sslmode=disable", true},
		{"query parameter overrides local host", "development", "postgres://streetmap:local-password@127.0.0.1:54329/streetmap?host=example.com", true},
		{"wrong development database name", "development", "postgres://streetmap:local-password@127.0.0.1:54329/streetmap_test?sslmode=disable", true},
		{"test database without test suffix", "test", "postgres://streetmap:local-password@127.0.0.1:54329/streetmap?sslmode=disable", true},
		{"malformed URL", "development", "not-a-database-url", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateTarget(test.environment, test.databaseURL)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateTarget() error = %v, wantError %t", err, test.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "local-password") {
				t.Fatal("validation error exposed the database password")
			}
		})
	}
}

func TestSeedIsIdempotentAndCoversPublicNotesContexts(t *testing.T) {
	tx := openSeedTestTransaction(t)
	ctx := context.Background()
	baseline := tableCounts(t, ctx, tx)
	if err := Seed(ctx, tx); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	firstCounts := tableCounts(t, ctx, tx)
	if err := Seed(ctx, tx); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	secondCounts := tableCounts(t, ctx, tx)
	if firstCounts != secondCounts {
		t.Fatalf("second seed changed table counts: first=%v second=%v", firstCounts, secondCounts)
	}
	wantCounts := addCounts(baseline, [4]int{3, 9, 60, 7})
	if firstCounts != wantCounts {
		t.Fatalf("unexpected seed row counts: got %v, want %v", firstCounts, wantCounts)
	}

	reader := notes.NewPostgresReader(tx)
	t.Run("verified location pagination", func(t *testing.T) {
		query := notes.Query{Context: notes.Context{Type: notes.ContextVerifiedPlace, LocationID: seedLocations[0].ID}, Limit: 20}
		first, err := reader.ListPublicNotes(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Exact) != 20 || !first.HasMore || first.NextCursor == nil || len(first.Nearby) != 0 {
			t.Fatalf("unexpected first exact page: exact=%d nearby=%d hasMore=%t", len(first.Exact), len(first.Nearby), first.HasMore)
		}
		query.Cursor = first.NextCursor
		second, err := reader.ListPublicNotes(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Exact) != 20 || !second.HasMore || second.NextCursor == nil || second.Exact[0].MatchType != notes.MatchExactLocation {
			t.Fatalf("unexpected second exact page: %+v", second)
		}
		query.Cursor = second.NextCursor
		third, err := reader.ListPublicNotes(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(third.Exact) != 11 || third.HasMore || third.NextCursor != nil {
			t.Fatalf("unexpected third exact page: %+v", third)
		}
	})

	t.Run("sparse exact results precede nearby source and distance", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextVerifiedPlace, LocationID: seedLocations[7].ID}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Exact) != 2 || page.Exact[0].MatchType != notes.MatchExactLocation || len(page.Nearby) != 1 {
			t.Fatalf("unexpected exact/nearby groups: exact=%+v nearby=%+v", page.Exact, page.Nearby)
		}
		nearby := page.Nearby[0]
		if nearby.MatchType != notes.MatchNearby || nearby.LocationLabel != seedLocations[8].Label || nearby.DistanceM == nil || *nearby.DistanceM < 1000 || *nearby.DistanceM > 1200 {
			t.Fatalf("nearby source or distance was not preserved: %+v", nearby)
		}
	})

	t.Run("broad area and unverified reference remain separate", func(t *testing.T) {
		broad, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextBroadArea, Label: "  STREETMAP FICTIONAL TEST QUARTER "}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(broad.Exact) != 0 || len(broad.ContextMatches) != 1 || broad.ContextMatches[0].MatchType != notes.MatchBroadArea {
			t.Fatalf("unexpected broad-area results: %+v", broad)
		}
		unverified, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextUnverifiedPlace, Label: seedLocations[4].Label}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(unverified.Exact) != 0 || len(unverified.ContextMatches) != 1 || unverified.ContextMatches[0].MatchType != notes.MatchUnverifiedReference {
			t.Fatalf("unexpected unverified-place results: %+v", unverified)
		}
	})

	t.Run("map center supplies only nearby notes within five kilometres", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextMapCenter, Latitude: 0, Longitude: 0, RadiusM: 5000}, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Exact) != 0 || len(page.ContextMatches) != 0 || len(page.Nearby) != 50 || !page.HasMore || page.NextCursor == nil {
			t.Fatalf("unexpected map-center groups: exact=%d context=%d nearby=%d", len(page.Exact), len(page.ContextMatches), len(page.Nearby))
		}
		var sawNear, sawFar bool
		for _, item := range page.Nearby {
			if item.MatchType != notes.MatchNearby || item.DistanceM == nil || *item.DistanceM > 5000 {
				t.Fatalf("map-center result is not correctly marked nearby: %+v", item)
			}
			if item.LocationLabel == seedLocations[1].Label && *item.DistanceM > 1000 {
				sawNear = true
			}
			if item.LocationLabel == seedLocations[3].Label {
				t.Fatalf("out-of-radius note was returned: %+v", item)
			}
		}
		if !sawNear {
			t.Fatalf("expected nearer location with a distinct distance, sawNear=%t", sawNear)
		}
		nextPage, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextMapCenter, Latitude: 0, Longitude: 0, RadiusM: 5000}, Limit: 50, Cursor: page.NextCursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(nextPage.Nearby) < 4 {
			t.Fatalf("map-center second page did not contain the remaining seeded notes: %+v", nextPage)
		}
		for _, item := range nextPage.Nearby {
			if item.LocationLabel == seedLocations[2].Label && item.DistanceM != nil && *item.DistanceM > 3500 {
				sawFar = true
			}
			if item.LocationLabel == seedLocations[3].Label {
				t.Fatalf("out-of-radius note was returned on the next page: %+v", item)
			}
		}
		if !sawFar {
			t.Fatal("expected the farther in-radius source on the next page")
		}
	})

	t.Run("coordinate-only lookup returns exact coordinate notes", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextCoordinate, Latitude: 0.002, Longitude: 0.002}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Exact) != 1 || page.Exact[0].MatchType != notes.MatchExactCoordinate || page.Exact[0].LocationLabel != seedLocations[6].Label {
			t.Fatalf("unexpected coordinate results: %+v", page)
		}
	})

	if err := Remove(ctx, tx); err != nil {
		t.Fatalf("remove only seed records: %v", err)
	}
	if got := tableCounts(t, ctx, tx); got != baseline {
		t.Fatalf("seed removal changed pre-existing rows: got %v, want baseline %v", got, baseline)
	}
	if err := Remove(ctx, tx); err != nil {
		t.Fatalf("repeat seed removal: %v", err)
	}
}

func openSeedTestTransaction(t *testing.T) *sql.Tx {
	t.Helper()
	dsn := os.Getenv("STREETMAP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set STREETMAP_TEST_DATABASE_URL to a dedicated local database ending in _test")
	}
	if _, err := ValidateTarget("test", dsn); err != nil {
		t.Fatalf("unsafe seed test database: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.Trim(parsed.Path, "/"), "_test") {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must name a dedicated _test database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open dedicated seed test database")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close seed test database: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("connect to dedicated seed test database")
	}
	if err := migrations.Up(ctx, db); err != nil {
		t.Fatalf("apply test database migrations: %v", err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal("begin seed test transaction")
	}
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			t.Errorf("rollback seed test transaction: %v", err)
		}
	})
	return tx
}

func tableCounts(t *testing.T, ctx context.Context, tx *sql.Tx) [4]int {
	t.Helper()
	var counts [4]int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&counts[0]); err != nil {
		t.Fatal("count users")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM locations`).Scan(&counts[1]); err != nil {
		t.Fatal("count locations")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&counts[2]); err != nil {
		t.Fatal("count notes")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM votes`).Scan(&counts[3]); err != nil {
		t.Fatal("count votes")
	}
	return counts
}

func addCounts(left, right [4]int) [4]int {
	return [4]int{left[0] + right[0], left[1] + right[1], left[2] + right[2], left[3] + right[3]}
}
