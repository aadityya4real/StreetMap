package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/httpapi"
	"streetmap.in/backend/internal/notes"
	"streetmap.in/backend/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestAuthenticatedNoteCreationIsIdempotentRateLimitedAndPublic(t *testing.T) {
	db := openNoteCreationTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	userID := insertCreationTestUser(t, ctx, db)
	locationIDs := make([]string, 0, 4)
	t.Cleanup(func() {
		cleanupCreatedNoteFixtures(t, userID, locationIDs, db)
	})
	verifiedLocationID := insertCreationTestVerifiedLocation(t, ctx, db)
	locationIDs = append(locationIDs, verifiedLocationID)
	var unverifiedLocationID string

	creator, err := notes.NewPostgresCreator(db, notes.CreationPolicy{MaxPerWindow: 100, Window: time.Hour})
	if err != nil {
		t.Fatal("configure isolated integration-test creator")
	}
	reader := notes.NewPostgresReader(db)
	publicHandler := httpapi.NewHandler(nil, reader, nil, time.Second, nil, creator)

	verified := notes.CreateCommand{
		Location: notes.CreateLocation{Type: notes.CreateLocationVerified, LocationID: verifiedLocationID},
		Title:    "Verified place contribution", Body: "A verified location detail.", Category: "road_condition",
	}
	verifiedResult := createIntegrationNote(t, ctx, creator, userID, testIdempotencyKey(1), verified)
	if verifiedResult.Note.MatchType != notes.MatchExactLocation {
		t.Fatalf("verified note match type = %q", verifiedResult.Note.MatchType)
	}
	var authorID, status, category string
	if err := db.QueryRowContext(ctx, `SELECT author_user_id::text, status, category FROM notes WHERE id = $1::uuid`, verifiedResult.Note.ID).Scan(&authorID, &status, &category); err != nil {
		t.Fatal("inspect created note identity and publication state")
	}
	if authorID != userID || status != "published" || category != "road_condition" {
		t.Fatalf("created note author/status/category = %s/%s/%s", authorID, status, category)
	}
	assertPublicGETContainsNote(t, publicHandler, "/api/v1/notes?location_id="+url.QueryEscape(verifiedLocationID), verifiedResult.Note.ID, "exact")

	for index, test := range []struct {
		name    string
		command notes.CreateCommand
		query   string
		group   string
	}{
		{
			name:    "unverified label remains context match",
			command: notes.CreateCommand{Location: notes.CreateLocation{Type: notes.CreateLocationUnverified, Label: "URL label venue"}, Title: "Unverified reference detail", Body: "A label-derived contribution.", Category: "safety"},
			query:   "context_type=place_page_unverified&label=URL+label+venue", group: "context_matches",
		},
		{
			name:    "broad label remains context match",
			command: notes.CreateCommand{Location: notes.CreateLocation{Type: notes.CreateLocationBroad, Label: "Test neighbourhood"}, Title: "Broad area detail", Body: "A neighbourhood contribution.", Category: "other"},
			query:   "context_type=broad_area&label=Test+neighbourhood", group: "context_matches",
		},
		{
			name:    "coordinate context remains exact coordinate",
			command: notes.CreateCommand{Location: notes.CreateLocation{Type: notes.CreateLocationCoordinates, Latitude: floatPointer(27.5), Longitude: floatPointer(76.5), UserConfirmed: boolPointer(true)}, Title: "Confirmed coordinate detail", Body: "A user-confirmed coordinate contribution.", Category: "shortcut"},
			query:   "context_type=coordinate&latitude=27.5&longitude=76.5", group: "exact",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := createIntegrationNote(t, ctx, creator, userID, testIdempotencyKey(index+2), test.command)
			if result.Note.MatchType == notes.MatchExactLocation && test.group != "exact" {
				t.Fatal("non-verified context was represented as a verified exact match")
			}
			if result.Note.MatchType == "" {
				t.Fatal("created note is missing match provenance")
			}
			var locationID string
			if err := db.QueryRowContext(ctx, `SELECT location_id::text FROM notes WHERE id = $1::uuid`, result.Note.ID).Scan(&locationID); err != nil {
				t.Fatal("read created location reference")
			}
			locationIDs = append(locationIDs, locationID)
			if test.command.Location.Type == notes.CreateLocationUnverified {
				unverifiedLocationID = locationID
			}
			assertPublicGETContainsNote(t, publicHandler, "/api/v1/notes?"+test.query, result.Note.ID, test.group)
		})
	}

	t.Run("unverified database row cannot be claimed as verified", func(t *testing.T) {
		_, err := creator.CreateNote(ctx, userID, testIdempotencyKey(9), notes.CreateCommand{
			Location: notes.CreateLocation{Type: notes.CreateLocationVerified, LocationID: unverifiedLocationID},
			Title:    "Misclassified reference", Body: "Must not be accepted as verified.", Category: "local_tip",
		})
		if !errors.Is(err, notes.ErrLocationNotFound) {
			t.Fatalf("unverified location claim error = %v", err)
		}
	})

	t.Run("idempotency replay and payload binding", func(t *testing.T) {
		key := testIdempotencyKey(10)
		handler := httpapi.NewHandler(nil, reader, nil, time.Second, integrationAuthenticator{userID: userID}, creator)
		body := fmt.Sprintf(`{"location":{"type":"verified","location_id":%q},"title":"HTTP idempotency test","body":"Original payload","category":"local_tip"}`, verifiedLocationID)
		first := postIntegrationNote(handler, key, body)
		if first.Code != http.StatusCreated {
			t.Fatalf("initial HTTP creation status=%d body=%s", first.Code, first.Body.String())
		}
		var firstPayload struct {
			Data struct {
				Note struct {
					ID string `json:"id"`
				} `json:"note"`
			} `json:"data"`
		}
		if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil || firstPayload.Data.Note.ID == "" {
			t.Fatalf("decode initial HTTP creation response (id present=%v)", firstPayload.Data.Note.ID != "")
		}
		replay := postIntegrationNote(handler, key, body)
		if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), firstPayload.Data.Note.ID) {
			t.Fatalf("idempotent HTTP retry status=%d body=%s", replay.Code, replay.Body.String())
		}
		changed := strings.Replace(body, "Original payload", "Different payload", 1)
		conflict := postIntegrationNote(handler, key, changed)
		if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"idempotency_conflict"`) {
			t.Fatalf("conflicting HTTP retry status=%d body=%s", conflict.Code, conflict.Body.String())
		}
	})

	t.Run("concurrent identical retries create one note", func(t *testing.T) {
		key := testIdempotencyKey(11)
		var wait sync.WaitGroup
		results := make([]notes.CreateResult, 2)
		errorsFound := make([]error, 2)
		for index := range results {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				results[index], errorsFound[index] = creator.CreateNote(ctx, userID, key, verified)
			}(index)
		}
		wait.Wait()
		for _, createErr := range errorsFound {
			if createErr != nil {
				t.Fatalf("concurrent creation failed: %v", createErr)
			}
		}
		if results[0].Note.ID == "" || results[0].Note.ID != results[1].Note.ID || results[0].Replayed == results[1].Replayed {
			t.Fatalf("concurrent calls did not return one create and one replay: %+v", results)
		}
	})

	t.Run("rate limit is per account and does not block a successful replay", func(t *testing.T) {
		limitedUser := insertCreationTestUser(t, ctx, db)
		defer cleanupCreatedNoteFixtures(t, limitedUser, nil, db)
		limitedCreator, err := notes.NewPostgresCreator(db, notes.CreationPolicy{MaxPerWindow: 1, Window: time.Hour})
		if err != nil {
			t.Fatal("configure limited creator")
		}
		key := testIdempotencyKey(12)
		first := createIntegrationNote(t, ctx, limitedCreator, limitedUser, key, verified)
		replay := createIntegrationNote(t, ctx, limitedCreator, limitedUser, key, verified)
		if !replay.Replayed || replay.Note.ID != first.Note.ID {
			t.Fatal("idempotent replay was rejected by the rate limit")
		}
		if _, err := limitedCreator.CreateNote(ctx, limitedUser, testIdempotencyKey(13), verified); err == nil {
			t.Fatal("second new note unexpectedly passed the one-note limit")
		} else {
			var rateErr *notes.RateLimitError
			if !errors.As(err, &rateErr) || rateErr.RetryAfter <= 0 {
				t.Fatalf("rate-limit error = %v", err)
			}
		}
	})

	t.Run("creator rechecks suspended account status", func(t *testing.T) {
		suspendedUser := insertCreationTestUser(t, ctx, db)
		defer cleanupCreatedNoteFixtures(t, suspendedUser, nil, db)
		if _, err := db.ExecContext(ctx, `UPDATE users SET account_status = 'suspended' WHERE id = $1::uuid`, suspendedUser); err != nil {
			t.Fatal("suspend isolated note creation test user")
		}
		if _, err := creator.CreateNote(ctx, suspendedUser, testIdempotencyKey(14), verified); !errors.Is(err, notes.ErrAccountUnavailable) {
			t.Fatalf("suspended account create error = %v", err)
		}
	})

	t.Run("location and note writes roll back together on a later database failure", func(t *testing.T) {
		const triggerName = "streetmap_note_creation_atomicity_test"
		const functionName = "streetmap_note_creation_atomicity_test_fn"
		var triggerExists, functionExists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = $1 AND NOT tgisinternal)`, triggerName).Scan(&triggerExists); err != nil {
			t.Fatal("check for stale atomicity-test trigger")
		}
		if err := db.QueryRowContext(ctx, `SELECT to_regprocedure($1) IS NOT NULL`, functionName+"()").Scan(&functionExists); err != nil {
			t.Fatal("check for stale atomicity-test function")
		}
		if triggerExists || functionExists {
			t.Fatal("atomicity-test database contains stale test DDL; refusing to overwrite it")
		}
		const failureKey = "00000000-0000-4000-8000-000000000015"
		if _, err := db.ExecContext(ctx, `CREATE FUNCTION streetmap_note_creation_atomicity_test_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.idempotency_key = '`+failureKey+`'::uuid THEN RAISE EXCEPTION 'intentional integration-test failure'; END IF; RETURN NEW; END; $$`); err != nil {
			t.Fatal("install temporary isolated atomicity-test function")
		}
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER streetmap_note_creation_atomicity_test BEFORE INSERT ON note_creation_requests FOR EACH ROW EXECUTE FUNCTION streetmap_note_creation_atomicity_test_fn()`); err != nil {
			_, _ = db.ExecContext(ctx, `DROP FUNCTION streetmap_note_creation_atomicity_test_fn()`)
			t.Fatal("install temporary isolated atomicity-test trigger")
		}
		t.Cleanup(func() {
			if _, err := db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS streetmap_note_creation_atomicity_test ON note_creation_requests`); err != nil {
				t.Error("remove temporary atomicity-test trigger")
			}
			if _, err := db.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS streetmap_note_creation_atomicity_test_fn()`); err != nil {
				t.Error("remove temporary atomicity-test function")
			}
		})

		command := notes.CreateCommand{Location: notes.CreateLocation{Type: notes.CreateLocationUnverified, Label: "Atomic rollback test label"}, Title: "Atomicity failure test", Body: "Both rows should roll back.", Category: "local_tip"}
		if _, err := creator.CreateNote(ctx, userID, failureKey, command); err == nil {
			t.Fatal("expected injected idempotency-ledger failure")
		}
		var locations, notesCount int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM locations WHERE location_type = 'unverified_place_reference' AND display_label = 'Atomic rollback test label'`).Scan(&locations); err != nil {
			t.Fatal("check rolled-back location")
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE title = 'Atomicity failure test'`).Scan(&notesCount); err != nil {
			t.Fatal("check rolled-back note")
		}
		if locations != 0 || notesCount != 0 {
			t.Fatalf("failed transaction left data behind: locations=%d notes=%d", locations, notesCount)
		}
	})
}

func openNoteCreationTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("STREETMAP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set STREETMAP_TEST_DATABASE_URL to the isolated loopback streetmap_test PostGIS database")
	}
	if err := validateTestDatabaseURL(dsn); err != nil {
		t.Fatal("STREETMAP_TEST_DATABASE_URL must use loopback and target streetmap_test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open isolated note creation test database")
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("close isolated note creation test database")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("connect to isolated note creation test database")
	}
	if err := migrations.Up(ctx, db); err != nil {
		t.Fatal("apply additive migrations to isolated note creation test database")
	}
	return db
}

func insertCreationTestUser(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var userID string
	identifier := time.Now().UTC().Format("20060102150405.000000000")
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email, display_name) VALUES ($1, 'Creation Integration User') RETURNING id::text`, identifier+"@streetmap.invalid").Scan(&userID); err != nil {
		t.Fatal("insert isolated note creation test user")
	}
	return userID
}

func insertCreationTestVerifiedLocation(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var locationID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO locations (location_type, provider, provider_place_id, display_label, coordinates)
		VALUES ('verified_place', 'streetmap-integration-test', gen_random_uuid()::text, 'Creation Integration Verified Place', ST_SetSRID(ST_MakePoint(77, 28), 4326)::geography)
		RETURNING id::text`).Scan(&locationID); err != nil {
		t.Fatal("insert isolated verified location fixture")
	}
	return locationID
}

func createIntegrationNote(t *testing.T, ctx context.Context, creator notes.Creator, userID, key string, command notes.CreateCommand) notes.CreateResult {
	t.Helper()
	result, err := creator.CreateNote(ctx, userID, key, command)
	if err != nil {
		t.Fatal("create note in isolated PostGIS integration test")
	}
	return result
}

func assertPublicGETContainsNote(t *testing.T, handler http.Handler, path, noteID, group string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("anonymous public note GET status=%d body=%s", response.Code, response.Body.String())
	}
	type noteReference struct {
		ID string `json:"id"`
	}
	var result struct {
		Data struct {
			Exact          []noteReference `json:"exact"`
			ContextMatches []noteReference `json:"context_matches"`
			Nearby         []noteReference `json:"nearby"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal("decode anonymous public notes response")
	}
	var groupNotes []noteReference
	switch group {
	case "exact":
		groupNotes = result.Data.Exact
	case "context_matches":
		groupNotes = result.Data.ContextMatches
	case "nearby":
		groupNotes = result.Data.Nearby
	}
	for _, note := range groupNotes {
		if note.ID == noteID {
			return
		}
	}
	t.Fatalf("new note %s was not visible in anonymous GET group %s", noteID, group)
}

func postIntegrationNote(handler http.Handler, key, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notes", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	request.Header.Set("Authorization", "Bearer test-only-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func cleanupCreatedNoteFixtures(t *testing.T, userID string, locationIDs []string, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Error("begin cleanup transaction for note creation test fixtures")
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_creation_requests WHERE user_id = $1::uuid`, userID); err != nil {
		t.Error("remove note creation idempotency fixtures")
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE author_user_id = $1::uuid`, userID); err != nil {
		t.Error("remove note creation fixtures")
		return
	}
	for _, locationID := range locationIDs {
		if locationID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id = $1::uuid`, locationID); err != nil {
			t.Error("remove note creation location fixture")
			return
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID); err != nil {
		t.Error("remove note creation user fixture")
		return
	}
	if err := tx.Commit(); err != nil {
		t.Error("commit cleanup transaction for note creation test fixtures")
	}
}

func testIdempotencyKey(sequence int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence)
}

func floatPointer(value float64) *float64 { return &value }

func boolPointer(value bool) *bool { return &value }

type integrationAuthenticator struct{ userID string }

func (authenticator integrationAuthenticator) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: authenticator.userID}, nil
}
