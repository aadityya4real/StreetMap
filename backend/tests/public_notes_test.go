package tests

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/url"
	"os"
	"slices"
	"sort"
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

	t.Run("identical labels preserve distinct location identities", func(t *testing.T) {
		firstLocation := createTestLocation(t, ctx, tx, "broad_area", "Shared Area Label", 0, 0)
		secondLocation := createTestLocation(t, ctx, tx, "broad_area", "  shared area label  ", 0, 0)
		firstNote := createTestNote(t, ctx, tx, userID, firstLocation, "First identity note", "published", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
		secondNote := createTestNote(t, ctx, tx, userID, secondLocation, "Second identity note", "published", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))

		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextBroadArea, Label: "Shared Area Label"}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 2 || page.ContextMatches[0].ID != secondNote || page.ContextMatches[1].ID != firstNote {
			t.Fatalf("same-label locations were merged, omitted, or misordered: %+v", page.ContextMatches)
		}
	})

	t.Run("broad label and map center return separate groups", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{
			Type: notes.ContextBroadArea, Label: "Downtown", Latitude: 28, Longitude: 77, RadiusM: 5000,
		}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 1 || page.ContextMatches[0].ID == "" || page.ContextMatches[0].MatchType != notes.MatchBroadArea {
			t.Fatalf("unexpected label context group: %+v", page.ContextMatches)
		}
		if len(page.Nearby) == 0 {
			t.Fatalf("expected map-center discoveries: %+v", page)
		}
		seen := map[string]bool{page.ContextMatches[0].ID: true}
		for _, note := range page.Nearby {
			if note.MatchType != notes.MatchNearby || note.DistanceM == nil || seen[note.ID] {
				t.Fatalf("nearby note was mislabeled or duplicated across groups: %+v", note)
			}
			seen[note.ID] = true
		}
	})

	t.Run("label matches remain when map center has no nearby notes", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{
			Type: notes.ContextBroadArea, Label: "Downtown", Latitude: 0, Longitude: 0, RadiusM: 5000,
		}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 1 || len(page.Nearby) != 0 {
			t.Fatalf("unexpected context-only results: %+v", page)
		}
	})

	t.Run("nearby notes remain when label has no matches", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{
			Type: notes.ContextUnverifiedPlace, Label: "No label match", Latitude: 28, Longitude: 77, RadiusM: 5000,
		}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 0 || len(page.Nearby) == 0 {
			t.Fatalf("unexpected nearby-only results: %+v", page)
		}
	})

	t.Run("place-page label remains separate from map-center results", func(t *testing.T) {
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{
			Type: notes.ContextUnverifiedPlace, Label: "Cafe Example", Latitude: 28, Longitude: 77, RadiusM: 5000,
		}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.ContextMatches) != 1 || page.ContextMatches[0].ID == "" || page.ContextMatches[0].MatchType != notes.MatchUnverifiedReference {
			t.Fatalf("unexpected unverified place group: %+v", page.ContextMatches)
		}
		if len(page.Nearby) == 0 || page.Nearby[0].MatchType != notes.MatchNearby {
			t.Fatalf("expected separate nearby results: %+v", page.Nearby)
		}
	})

	t.Run("pagination continues from label phase into nearby phase", func(t *testing.T) {
		query := notes.Query{Context: notes.Context{
			Type: notes.ContextBroadArea, Label: "Downtown", Latitude: 28, Longitude: 77, RadiusM: 5000,
		}, Limit: 1}
		first, err := reader.ListPublicNotes(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.ContextMatches) != 1 || len(first.Nearby) != 0 || !first.HasMore || first.NextCursor == nil || first.NextCursor.Phase != notes.PhaseNearby {
			t.Fatalf("unexpected context-to-nearby transition: %+v", first)
		}
		seen := map[string]bool{first.ContextMatches[0].ID: true}
		query.Cursor = first.NextCursor
		pages := 0
		for {
			page, err := reader.ListPublicNotes(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.ContextMatches) != 0 {
				t.Fatalf("context phase restarted after nearby phase: %+v", page)
			}
			for _, note := range page.Nearby {
				if seen[note.ID] || note.MatchType != notes.MatchNearby {
					t.Fatalf("nearby pagination duplicated or mislabeled note: %+v", note)
				}
				seen[note.ID] = true
			}
			pages++
			if !page.HasMore {
				break
			}
			if page.NextCursor == nil {
				t.Fatal("has_more page did not include a cursor")
			}
			query.Cursor = page.NextCursor
		}
		if pages == 0 || len(seen) < 2 {
			t.Fatalf("pagination did not return nearby notes: pages=%d seen=%v", pages, seen)
		}
	})

	t.Run("combined nearby results include the radius boundary in distance order", func(t *testing.T) {
		boundaryLabelID := createTestLocation(t, ctx, tx, "broad_area", "Boundary Area", 0, 0)
		createTestNote(t, ctx, tx, userID, boundaryLabelID, "Boundary area context", "published", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
		fixtures := []struct {
			label    string
			title    string
			distance float64
		}{
			{"Two kilometer point", "Boundary 2000m", 2000},
			{"Just inside point", "Boundary 4999m", 4999},
			{"At limit point", "Boundary 5000m", 5000},
			{"Outside limit point", "Boundary 5001m", 5001},
		}
		for index, fixture := range fixtures {
			var locationID string
			err := tx.QueryRowContext(ctx, `
				INSERT INTO locations (location_type, provider, provider_place_id, display_label, coordinates)
				VALUES ('verified_place', 'test-provider', gen_random_uuid()::text, $1,
					ST_Project(ST_SetSRID(ST_MakePoint(0::double precision, 0::double precision), 4326)::geography,
						$2::double precision, 0::double precision))
				RETURNING id::text`, fixture.label, fixture.distance).Scan(&locationID)
			if err != nil {
				t.Fatalf("insert %s boundary fixture: %v", fixture.label, err)
			}
			createTestNote(t, ctx, tx, userID, locationID, fixture.title, "published", time.Date(2026, 2, 1, index+1, 0, 0, 0, time.UTC))
		}

		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{
			Type: notes.ContextBroadArea, Label: "Boundary Area", Latitude: 0, Longitude: 0, RadiusM: 5000,
		}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		wantTitles := []string{"Boundary 2000m", "Boundary 4999m", "Boundary 5000m"}
		wantDistances := []float64{2000, 4999, 5000}
		if len(page.ContextMatches) != 1 || page.ContextMatches[0].MatchType != notes.MatchBroadArea {
			t.Fatalf("unexpected label context results: %+v", page.ContextMatches)
		}
		if len(page.Nearby) != len(wantTitles) {
			t.Fatalf("nearby radius filtering returned %d rows, want %d: %+v", len(page.Nearby), len(wantTitles), page.Nearby)
		}
		for index, nearbyNote := range page.Nearby {
			if nearbyNote.Title != wantTitles[index] || nearbyNote.DistanceM == nil || math.Abs(*nearbyNote.DistanceM-wantDistances[index]) > 0.1 {
				t.Fatalf("nearby results are not in distance order or have inaccurate distance at index %d: %+v", index, nearbyNote)
			}
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

	t.Run("nearby pagination is deterministic for dense equal-distance results", func(t *testing.T) {
		createdAt := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		wantIDs := make([]string, 0, 7)
		for index := 0; index < 7; index++ {
			var locationID string
			err := tx.QueryRowContext(ctx, `
				INSERT INTO locations (location_type, provider, provider_place_id, display_label, coordinates)
				VALUES ('verified_place', 'test-provider', gen_random_uuid()::text, $1,
					ST_Project(ST_SetSRID(ST_MakePoint(0::double precision, 0::double precision), 4326)::geography,
						1000::double precision, 90::double precision))
				RETURNING id::text`, fmt.Sprintf("Equal-distance location %d", index)).Scan(&locationID)
			if err != nil {
				t.Fatalf("insert equal-distance location: %v", err)
			}
			wantIDs = append(wantIDs, createTestNote(t, ctx, tx, userID, locationID, fmt.Sprintf("Equal-distance note %d", index), "published", createdAt))
		}
		sort.Sort(sort.Reverse(sort.StringSlice(wantIDs)))

		query := notes.Query{Context: notes.Context{Type: notes.ContextMapCenter, Latitude: 0, Longitude: 0, RadiusM: 1500}, Limit: 2}
		seen := make([]string, 0, len(wantIDs))
		for {
			page, err := reader.ListPublicNotes(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			for _, note := range page.Nearby {
				seen = append(seen, note.ID)
				if note.DistanceM == nil || math.Abs(*note.DistanceM-1000) > 0.1 {
					t.Fatalf("equal-distance fixture has unexpected distance: %+v", note)
				}
			}
			if !page.HasMore {
				break
			}
			if page.NextCursor == nil {
				t.Fatal("has_more page did not include a cursor")
			}
			query.Cursor = page.NextCursor
		}
		if !slices.Equal(seen, wantIDs) {
			t.Fatalf("dense pagination was not deterministic or skipped/repeated results: got %v, want %v", seen, wantIDs)
		}
	})

	t.Run("nearby query excludes locations without coordinates and empty results have no cursor", func(t *testing.T) {
		var locationID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO locations (location_type, provider, provider_place_id, display_label)
			VALUES ('verified_place', 'test-provider', gen_random_uuid()::text, 'Unlocated verified place')
			RETURNING id::text`).Scan(&locationID); err != nil {
			t.Fatalf("insert verified place without coordinates: %v", err)
		}
		createTestNote(t, ctx, tx, userID, locationID, "Unlocated note", "published", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextMapCenter, Latitude: -90, Longitude: -180, RadiusM: 1}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Nearby) != 0 || page.HasMore || page.NextCursor != nil {
			t.Fatalf("empty nearby result returned rows or a continuation cursor: %+v", page)
		}
	})

	t.Run("verified place with null coordinates retains exact notes without nearby results", func(t *testing.T) {
		var locationID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO locations (location_type, provider, provider_place_id, display_label)
			VALUES ('verified_place', 'test-provider', gen_random_uuid()::text, 'Verified place without point')
			RETURNING id::text`).Scan(&locationID); err != nil {
			t.Fatalf("insert verified place without coordinates: %v", err)
		}
		exactID := createTestNote(t, ctx, tx, userID, locationID, "Exact note without point", "published", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
		page, err := reader.ListPublicNotes(ctx, notes.Query{Context: notes.Context{Type: notes.ContextVerifiedPlace, LocationID: locationID}, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Exact) != 1 || page.Exact[0].ID != exactID || len(page.Nearby) != 0 {
			t.Fatalf("null point broke exact results or produced nearby results: %+v", page)
		}
	})

	t.Run("spatial index is present and valid", func(t *testing.T) {
		var valid bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_index i
				JOIN pg_class index_class ON index_class.oid = i.indexrelid
				JOIN pg_am access_method ON access_method.oid = index_class.relam
				WHERE index_class.relname = 'locations_coordinates_gist'
					AND access_method.amname = 'gist'
					AND i.indisvalid AND i.indisready
			)`).Scan(&valid); err != nil {
			t.Fatalf("inspect spatial index: %v", err)
		}
		if !valid {
			t.Fatal("locations_coordinates_gist is missing or invalid")
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
	if err := validateTestDatabaseURL(dsn); err != nil {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must use localhost or 127.0.0.1 and target streetmap_test")
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

func validateTestDatabaseURL(dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return fmt.Errorf("invalid PostgreSQL URL")
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") && host != "127.0.0.1" {
		return fmt.Errorf("test database host must be loopback")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || query.Has("host") || query.Has("hostaddr") || query.Has("service") || query.Has("servicefile") {
		return fmt.Errorf("test database URL must not override its loopback host")
	}
	databaseName := strings.Trim(parsed.Path, "/")
	if databaseName != "streetmap_test" {
		return fmt.Errorf("test database name must be streetmap_test")
	}
	return nil
}

func TestValidateTestDatabaseURL(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want bool
	}{
		{"localhost test database", "postgres://streetmap:local-only@localhost:5432/streetmap_test?sslmode=disable", true},
		{"IPv4 loopback test database", "postgresql://streetmap:local-only@127.0.0.1:5432/streetmap_test?sslmode=disable", true},
		{"localhost host is case insensitive", "postgres://streetmap:local-only@LOCALHOST:5432/streetmap_test", true},
		{"remote hostname", "postgres://streetmap:local-only@db.example.test:5432/streetmap_test", false},
		{"remote IP address", "postgres://streetmap:local-only@192.0.2.10:5432/streetmap_test", false},
		{"host override in query", "postgres://streetmap:local-only@localhost:5432/streetmap_test?host=db.example.test", false},
		{"host address override in query", "postgres://streetmap:local-only@localhost:5432/streetmap_test?hostaddr=192.0.2.10", false},
		{"empty host", "postgres:///streetmap_test", false},
		{"missing database name", "postgres://localhost:5432/", false},
		{"non-test database name", "postgres://localhost:5432/streetmap", false},
		{"different database with test suffix", "postgres://localhost:5432/another_test", false},
		{"malformed URL", "not a database URL", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateTestDatabaseURL(test.dsn)
			if (err == nil) != test.want {
				t.Fatalf("validateTestDatabaseURL() error = %v, wantValid %v", err, test.want)
			}
		})
	}
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
