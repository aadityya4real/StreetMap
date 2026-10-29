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

	"streetmap.in/backend/internal/notes"
	"streetmap.in/backend/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPublicNotesContextsAndNearbyPolicy(t *testing.T) {
	tx := openNotesTestTransaction(t)
	ctx := context.Background()
	reader := notes.NewPostgresReader(tx)
	userID := createTestUser(t, ctx, tx)

	verifiedID := createTestLocation(t, ctx, tx, "verified_place", "Cafe Example", 77, 28)
	nearbyID := createTestLocation(t, ctx, tx, "verified_place", "Nearby Market", 77.006, 28)
	farID := createTestLocation(t, ctx, tx, "verified_place", "Far Shop", 77.06, 28)
	broadID := createTestLocation(t, ctx, tx, "broad_area", "Downtown", 0, 0)
	otherBroadID := createTestLocation(t, ctx, tx, "broad_area", "Other Area", 0, 0)
	unverifiedID := createTestLocation(t, ctx, tx, "unverified_place_reference", "Cafe Example", 0, 0)
	coordinateID := createTestLocation(t, ctx, tx, "coordinate_only", "User pin", 77.25, 28.5)

	exactOlder := createTestNote(t, ctx, tx, userID, verifiedID, "Exact older", "published", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	exactNewer := createTestNote(t, ctx, tx, userID, verifiedID, "Exact newer", "published", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	nearNote := createTestNote(t, ctx, tx, userID, nearbyID, "Nearby", "published", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	createTestNote(t, ctx, tx, userID, farID, "Farther than five km", "published", time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC))
	createTestNote(t, ctx, tx, userID, nearbyID, "Hidden nearby", "hidden", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	createTestNote(t, ctx, tx, userID, broadID, "Broad note", "published", time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC))
	createTestNote(t, ctx, tx, userID, otherBroadID, "Other broad note", "published", time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC))
	createTestNote(t, ctx, tx, userID, unverifiedID, "URL label note", "published", time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC))
	coordinateNote := createTestNote(t, ctx, tx, userID, coordinateID, "Coordinate note", "published", time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC))
	if _, err := tx.ExecContext(ctx, `INSERT INTO votes (note_id, user_id) VALUES ($1, $2)`, nearNote, userID); err != nil {
		t.Fatalf("insert helpful vote fixture: %v", err)
	}

	t.Run("verified exact results precede and are separate from nearby", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextVerifiedPlace, LocationID: verifiedID}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Exact) != 2 || page.Exact[0].Title != "Exact newer" || page.Exact[1].Title != "Exact older" {
			t.Fatalf("unexpected exact notes: %+v", page.Exact)
		}
		if len(page.Nearby) != 1 || page.Nearby[0].Title != "Nearby" || page.Nearby[0].MatchType != notes.MatchNearby || page.Nearby[0].DistanceM == nil {
			t.Fatalf("unexpected nearby notes: %+v", page.Nearby)
		}
		if page.Nearby[0].LocationLabel != "Nearby Market" || *page.Nearby[0].DistanceM < 500 || *page.Nearby[0].DistanceM > 700 {
			t.Fatalf("nearby source or distance is wrong: %+v", page.Nearby[0])
		}
		if page.Nearby[0].HelpfulVoteCount != 1 {
			t.Fatalf("helpful vote count = %d, want 1", page.Nearby[0].HelpfulVoteCount)
		}
	})

	t.Run("sparse exact pages are keyset stable before nearby", func(t *testing.T) {
		query := notes.Query{Context: notes.Context{Type: notes.ContextVerifiedPlace, LocationID: verifiedID}, Limit: 1}
		first, err := reader.ListPublicNotes(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Exact) != 1 || first.Exact[0].ID != exactNewer || !first.HasMore || first.NextCursor == nil {
			t.Fatalf("unexpected first page: %+v", first)
		}
		query.Cursor = first.NextCursor
		second, err := reader.ListPublicNotes(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Exact) != 1 || second.Exact[0].ID != exactOlder || len(second.Nearby) != 0 || !second.HasMore || second.NextCursor == nil {
			t.Fatalf("unexpected second page: %+v", second)
		}
		query.Cursor = second.NextCursor
		third, err := reader.ListPublicNotes(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(third.Exact) != 0 || len(third.Nearby) != 1 || third.Nearby[0].Title != "Nearby" || third.HasMore {
			t.Fatalf("unexpected nearby page: %+v", third)
		}
	})

	t.Run("broad label matches only broad-area rows", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextBroadArea, Label: "  DOWNTOWN "}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 1 || page.ContextMatches[0].Title != "Broad note" || page.ContextMatches[0].MatchType != notes.MatchBroadArea || len(page.Nearby) != 0 {
			t.Fatalf("unexpected broad context results: %+v", page)
		}
	})

	t.Run("unverified label never matches a verified-place row", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextUnverifiedPlace, Label: "Cafe Example"}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 1 || page.ContextMatches[0].Title != "URL label note" || page.ContextMatches[0].MatchType != notes.MatchUnverifiedReference || len(page.Nearby) != 0 {
			t.Fatalf("unexpected unverified label results: %+v", page)
		}
	})

	t.Run("map center returns only nearby results within requested radius", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextMapCenter, Label: "Cafe Example", Latitude: 28, Longitude: 77, RadiusM: 5000}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Exact) != 0 {
			t.Fatalf("map-center request returned exact-place results: %+v", page.Exact)
		}
		foundNear := false
		for _, note := range page.Nearby {
			if note.ID == nearNote || note.ID == exactNewer || note.ID == exactOlder {
				foundNear = true
			}
			if note.Title == "Farther than five km" || note.Title == "Hidden nearby" {
				t.Fatalf("incorrect map-center candidate: %+v", note)
			}
		}
		if !foundNear {
			t.Fatalf("expected within-radius nearby note, got %+v", page.Nearby)
		}
	})

	t.Run("coordinate context matches persisted coordinate-only points exactly", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextCoordinate, Latitude: 28.5, Longitude: 77.25}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Exact) != 1 || page.Exact[0].ID != coordinateNote || page.Exact[0].MatchType != notes.MatchExactCoordinate || page.Exact[0].DistanceM != nil {
			t.Fatalf("unexpected coordinate results: %+v", page)
		}
	})

	t.Run("injection-like label is treated as data", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextBroadArea, Label: "' OR 1=1 --"}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 0 {
			t.Fatalf("injection-like label returned unrelated notes: %+v", page.ContextMatches)
		}
	})

}

func openNotesTestTransaction(t *testing.T) *sql.Tx {
	t.Helper()
	dsn := os.Getenv("STREETMAP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set STREETMAP_TEST_DATABASE_URL to run public notes integration tests")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.Trim(parsed.Path, "/"), "_test") {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must point to a dedicated database ending in _test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open notes test database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close notes test database: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping notes test database: %v", err)
	}
	if err := migrations.Up(ctx, db); err != nil {
		t.Fatalf("apply notes test migrations: %v", err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin fixture transaction: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			t.Errorf("rollback test fixtures: %v", err)
		}
	})
	return tx
}

func createTestUser(t *testing.T, ctx context.Context, tx *sql.Tx) string {
	t.Helper()
	var id string
	email := fmt.Sprintf("notes-api-%d@streetmap.test", time.Now().UnixNano())
	if err := tx.QueryRowContext(ctx, `INSERT INTO users (email, display_name) VALUES ($1, 'Public Name') RETURNING id::text`, email).Scan(&id); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return id
}

func createTestLocation(t *testing.T, ctx context.Context, tx *sql.Tx, locationType, label string, longitude, latitude float64) string {
	t.Helper()
	var id string
	var err error
	if locationType == "verified_place" {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO locations (location_type, provider, provider_place_id, display_label, coordinates)
			VALUES ($1, 'test-provider', gen_random_uuid()::text, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography)
			RETURNING id::text`, locationType, label, longitude, latitude).Scan(&id)
	} else if locationType == "coordinate_only" {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO locations (location_type, display_label, coordinates)
			VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography)
			RETURNING id::text`, locationType, label, longitude, latitude).Scan(&id)
	} else {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO locations (location_type, display_label)
			VALUES ($1, $2) RETURNING id::text`, locationType, label).Scan(&id)
	}
	if err != nil {
		t.Fatalf("insert %s test location: %v", locationType, err)
	}
	return id
}

func createTestNote(t *testing.T, ctx context.Context, tx *sql.Tx, userID, locationID, title, status string, createdAt time.Time) string {
	t.Helper()
	var id string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO notes (author_user_id, location_id, title, body, category, status, created_at)
		VALUES ($1, $2, $3, 'Fixture body for public notes integration test.', 'local_tip', $4, $5)
		RETURNING id::text`, userID, locationID, title, status, createdAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert test note: %v", err)
	}
	return id
}
