package devseed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const seedVersion = "v1"
const primaryLocationNoteCount = 51

type Target struct {
	Environment string
	Database    string
	Host        string
}

type coordinate struct {
	Longitude float64
	Latitude  float64
}

type user struct {
	ID          string
	Email       string
	DisplayName string
}

type location struct {
	ID              string
	Type            string
	Provider        string
	ProviderPlaceID string
	Label           string
	Coordinates     *coordinate
}

type note struct {
	ID         string
	AuthorID   string
	LocationID string
	Title      string
	Body       string
	Category   string
	CreatedAt  time.Time
}

type vote struct {
	NoteID string
	UserID string
}

var seedUsers = []user{
	{ID: "10000000-0000-4000-8000-000000000001", Email: "demo-author-one@streetmap.invalid", DisplayName: "StreetMap Demo Contributor One"},
	{ID: "10000000-0000-4000-8000-000000000002", Email: "demo-author-two@streetmap.invalid", DisplayName: "StreetMap Demo Contributor Two"},
	{ID: "10000000-0000-4000-8000-000000000003", Email: "demo-reader@streetmap.invalid", DisplayName: "StreetMap Demo Reader"},
}

var seedLocations = []location{
	{ID: "20000000-0000-4000-8000-000000000001", Type: "verified_place", Provider: "streetmap_demo", ProviderPlaceID: "seed-v1-lantern-loop", Label: "Fictional Lantern Loop Test Cafe [DEMO]", Coordinates: &coordinate{Longitude: 0.025, Latitude: 0}},
	{ID: "20000000-0000-4000-8000-000000000002", Type: "verified_place", Provider: "streetmap_demo", ProviderPlaceID: "seed-v1-glass-garden", Label: "Fictional Glass Garden Test Kiosk [DEMO]", Coordinates: &coordinate{Longitude: 0.012, Latitude: 0}},
	{ID: "20000000-0000-4000-8000-000000000003", Type: "verified_place", Provider: "streetmap_demo", ProviderPlaceID: "seed-v1-paper-bridge", Label: "Fictional Paper Bridge Test Steps [DEMO]", Coordinates: &coordinate{Longitude: 0.034, Latitude: 0}},
	{ID: "20000000-0000-4000-8000-000000000004", Type: "verified_place", Provider: "streetmap_demo", ProviderPlaceID: "seed-v1-far-lookout", Label: "Fictional Faraway Test Lookout [DEMO]", Coordinates: &coordinate{Longitude: 0.06, Latitude: 0}},
	{ID: "20000000-0000-4000-8000-000000000005", Type: "unverified_place_reference", Label: "Fictional Hidden Courtyard Test Place [DEMO]"},
	{ID: "20000000-0000-4000-8000-000000000006", Type: "broad_area", Label: "StreetMap Fictional Test Quarter"},
	{ID: "20000000-0000-4000-8000-000000000007", Type: "coordinate_only", Label: "Fictional Demo Pin [DEMO]", Coordinates: &coordinate{Longitude: 0.002, Latitude: 0.002}},
	{ID: "20000000-0000-4000-8000-000000000008", Type: "verified_place", Provider: "streetmap_demo", ProviderPlaceID: "seed-v1-quiet-arcade", Label: "Fictional Quiet Arcade Test Bench [DEMO]", Coordinates: &coordinate{Longitude: 1, Latitude: 1}},
	{ID: "20000000-0000-4000-8000-000000000009", Type: "verified_place", Provider: "streetmap_demo", ProviderPlaceID: "seed-v1-quiet-arcade-neighbor", Label: "Fictional Quiet Arcade Test Steps [DEMO]", Coordinates: &coordinate{Longitude: 1.01, Latitude: 1}},
}

var seedNotes = buildSeedNotes()

var seedVotes = []vote{
	{NoteID: "30000000-0000-4000-8000-000000000001", UserID: seedUsers[1].ID},
	{NoteID: "30000000-0000-4000-8000-000000000001", UserID: seedUsers[2].ID},
	{NoteID: "30000000-0000-4000-8000-000000000002", UserID: seedUsers[0].ID},
	{NoteID: "30000000-0000-4000-8000-000000000002", UserID: seedUsers[2].ID},
	{NoteID: "30000000-0000-4000-8000-000000000003", UserID: seedUsers[0].ID},
	{NoteID: "30000000-0000-4000-8000-000000000003", UserID: seedUsers[1].ID},
	{NoteID: "30000000-0000-4000-8000-000000000003", UserID: seedUsers[2].ID},
}

// ValidateTarget confines this development-only writer to the local compose database
// (or a dedicated test database). The command also requires a separate confirmation flag.
func ValidateTarget(environment, databaseURL string) (Target, error) {
	environment = strings.TrimSpace(environment)
	if environment != "development" && environment != "test" {
		return Target{}, errors.New("STREETMAP_ENV must be development or test")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" {
		return Target{}, errors.New("DATABASE_URL must be a valid PostgreSQL URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return Target{}, errors.New("seed data is restricted to a loopback database host")
		}
	}
	query := parsed.Query()
	for _, key := range []string{"host", "hostaddr", "service", "servicefile"} {
		if query.Has(key) {
			return Target{}, errors.New("DATABASE_URL host overrides are not allowed for seed operations")
		}
	}
	database := strings.Trim(parsed.Path, "/")
	if environment == "development" && database != "streetmap" {
		return Target{}, errors.New("development seeding is restricted to the streetmap database")
	}
	if environment == "test" && !strings.HasSuffix(database, "_test") {
		return Target{}, errors.New("test seeding requires a database name ending in _test")
	}
	if database == "" {
		return Target{}, errors.New("DATABASE_URL must include a database name")
	}
	return Target{Environment: environment, Database: database, Host: host}, nil
}

// Seed inserts stable demo records without modifying rows that already exist.
// Callers own the transaction so all seed records are committed atomically.
func Seed(ctx context.Context, tx *sql.Tx) error {
	if err := lockSeedTransaction(ctx, tx); err != nil {
		return err
	}
	for _, item := range seedUsers {
		if err := ensureUser(ctx, tx, item); err != nil {
			return err
		}
	}
	for _, item := range seedLocations {
		if err := ensureLocation(ctx, tx, item); err != nil {
			return err
		}
	}
	for _, item := range seedNotes {
		if err := ensureNote(ctx, tx, item); err != nil {
			return err
		}
	}
	for _, item := range seedVotes {
		if err := ensureVote(ctx, tx, item); err != nil {
			return err
		}
	}
	return nil
}

// Remove deletes only the fixed v1 seed rows. It refuses to remove records with
// user-created dependencies and never cascades through user-owned data.
func Remove(ctx context.Context, tx *sql.Tx) error {
	if err := lockSeedTransaction(ctx, tx); err != nil {
		return err
	}
	for _, item := range seedUsers {
		if _, err := verifyUser(ctx, tx, item); err != nil {
			return err
		}
	}
	for _, item := range seedLocations {
		if _, err := verifyLocation(ctx, tx, item); err != nil {
			return err
		}
	}
	for _, item := range seedNotes {
		if _, err := verifyNote(ctx, tx, item); err != nil {
			return err
		}
	}
	for _, item := range seedVotes {
		if _, err := verifyVote(ctx, tx, item); err != nil {
			return err
		}
	}

	hasDependencies, err := hasNonSeedDependencies(ctx, tx)
	if err != nil {
		return fmt.Errorf("check seed record dependencies: %w", err)
	}
	if hasDependencies {
		return errors.New("seed records have non-seed references; remove was stopped to preserve user data")
	}
	userIDs, locationIDs, noteIDs := seedIDLists()
	if _, err := tx.ExecContext(ctx, `DELETE FROM votes WHERE note_id::text = ANY($1::text[]) AND user_id::text = ANY($2::text[])`, noteIDs, userIDs); err != nil {
		return fmt.Errorf("remove seed votes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE id::text = ANY($1::text[])`, noteIDs); err != nil {
		return fmt.Errorf("remove seed notes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id::text = ANY($1::text[])`, locationIDs); err != nil {
		return fmt.Errorf("remove seed locations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id::text = ANY($1::text[])`, userIDs); err != nil {
		return fmt.Errorf("remove seed users: %w", err)
	}
	return nil
}

func lockSeedTransaction(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(20261030, 9)`); err != nil {
		return fmt.Errorf("lock StreetMap seed %s: %w", seedVersion, err)
	}
	return nil
}

func ensureUser(ctx context.Context, tx *sql.Tx, item user) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1::uuid, $2, $3) ON CONFLICT (id) DO NOTHING`, item.ID, item.Email, item.DisplayName); err != nil {
		return fmt.Errorf("insert seed user %s: %w", item.ID, err)
	}
	if _, err := verifyUser(ctx, tx, item); err != nil {
		return err
	}
	return nil
}

func verifyUser(ctx context.Context, tx *sql.Tx, item user) (bool, error) {
	var email, displayName string
	err := tx.QueryRowContext(ctx, `SELECT email, display_name FROM users WHERE id = $1::uuid`, item.ID).Scan(&email, &displayName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verify seed user %s: %w", item.ID, err)
	}
	if email != item.Email || displayName != item.DisplayName {
		return false, fmt.Errorf("seed ID %s belongs to different user data; refusing to overwrite it", item.ID)
	}
	return true, nil
}

func ensureLocation(ctx context.Context, tx *sql.Tx, item location) error {
	var longitude, latitude any
	if item.Coordinates != nil {
		longitude, latitude = item.Coordinates.Longitude, item.Coordinates.Latitude
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO locations (id, location_type, provider, provider_place_id, display_label, coordinates)
		VALUES ($1::uuid, $2, NULLIF($3, ''), NULLIF($4, ''), $5,
			CASE WHEN $6::boolean THEN ST_SetSRID(ST_MakePoint($7::double precision, $8::double precision), 4326)::geography ELSE NULL END)
		ON CONFLICT (id) DO NOTHING`,
		item.ID, item.Type, item.Provider, item.ProviderPlaceID, item.Label, item.Coordinates != nil, longitude, latitude)
	if err != nil {
		return fmt.Errorf("insert seed location %s: %w", item.ID, err)
	}
	if _, err := verifyLocation(ctx, tx, item); err != nil {
		return err
	}
	return nil
}

func verifyLocation(ctx context.Context, tx *sql.Tx, item location) (bool, error) {
	var locationType, label string
	var provider, providerPlaceID sql.NullString
	var longitude, latitude sql.NullFloat64
	err := tx.QueryRowContext(ctx, `
		SELECT location_type, provider, provider_place_id, display_label,
			ST_X(coordinates::geometry), ST_Y(coordinates::geometry)
		FROM locations WHERE id = $1::uuid`, item.ID).Scan(&locationType, &provider, &providerPlaceID, &label, &longitude, &latitude)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verify seed location %s: %w", item.ID, err)
	}
	matches := locationType == item.Type && label == item.Label && nullableStringMatches(provider, item.Provider) && nullableStringMatches(providerPlaceID, item.ProviderPlaceID)
	if item.Coordinates == nil {
		matches = matches && !longitude.Valid && !latitude.Valid
	} else {
		matches = matches && longitude.Valid && latitude.Valid && longitude.Float64 == item.Coordinates.Longitude && latitude.Float64 == item.Coordinates.Latitude
	}
	if !matches {
		return false, fmt.Errorf("seed ID %s belongs to different location data; refusing to overwrite it", item.ID)
	}
	return true, nil
}

func ensureNote(ctx context.Context, tx *sql.Tx, item note) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO notes (id, author_user_id, location_id, title, body, category, status, created_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, 'published', $7)
		ON CONFLICT (id) DO NOTHING`, item.ID, item.AuthorID, item.LocationID, item.Title, item.Body, item.Category, item.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert seed note %s: %w", item.ID, err)
	}
	if _, err := verifyNote(ctx, tx, item); err != nil {
		return err
	}
	return nil
}

func verifyNote(ctx context.Context, tx *sql.Tx, item note) (bool, error) {
	var authorID, locationID, title, body, category, status string
	var createdAt time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT author_user_id::text, location_id::text, title, body, category, status, created_at
		FROM notes WHERE id = $1::uuid`, item.ID).Scan(&authorID, &locationID, &title, &body, &category, &status, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verify seed note %s: %w", item.ID, err)
	}
	if authorID != item.AuthorID || locationID != item.LocationID || title != item.Title || body != item.Body || category != item.Category || status != "published" || !createdAt.Equal(item.CreatedAt) {
		return false, fmt.Errorf("seed ID %s belongs to different note data; refusing to overwrite it", item.ID)
	}
	return true, nil
}

func ensureVote(ctx context.Context, tx *sql.Tx, item vote) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO votes (note_id, user_id) VALUES ($1::uuid, $2::uuid) ON CONFLICT (note_id, user_id) DO NOTHING`, item.NoteID, item.UserID)
	if err != nil {
		return fmt.Errorf("insert seed vote for note %s: %w", item.NoteID, err)
	}
	if _, err := verifyVote(ctx, tx, item); err != nil {
		return err
	}
	return nil
}

func verifyVote(ctx context.Context, tx *sql.Tx, item vote) (bool, error) {
	var voteType string
	err := tx.QueryRowContext(ctx, `SELECT vote_type FROM votes WHERE note_id = $1::uuid AND user_id = $2::uuid`, item.NoteID, item.UserID).Scan(&voteType)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verify seed vote for note %s: %w", item.NoteID, err)
	}
	if voteType != "helpful" {
		return false, fmt.Errorf("seed vote for note %s has unexpected data; refusing to overwrite it", item.NoteID)
	}
	return true, nil
}

func hasNonSeedDependencies(ctx context.Context, tx *sql.Tx) (bool, error) {
	userIDs, locationIDs, noteIDs := seedIDLists()
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM notes
			WHERE (author_user_id::text = ANY($1::text[]) AND id::text <> ALL($3::text[]))
			   OR (location_id::text = ANY($2::text[]) AND id::text <> ALL($3::text[]))
			UNION ALL
			SELECT 1 FROM votes
			WHERE (note_id::text = ANY($3::text[]) AND user_id::text <> ALL($1::text[]))
			   OR (user_id::text = ANY($1::text[]) AND note_id::text <> ALL($3::text[]))
			UNION ALL
			SELECT 1 FROM reports
			WHERE reporter_user_id::text = ANY($1::text[]) OR reported_note_id::text = ANY($3::text[])
		)`, userIDs, locationIDs, noteIDs).Scan(&exists)
	return exists, err
}

func nullableStringMatches(value sql.NullString, expected string) bool {
	if expected == "" {
		return !value.Valid
	}
	return value.Valid && value.String == expected
}

func seedIDLists() ([]string, []string, []string) {
	users := make([]string, 0, len(seedUsers))
	for _, item := range seedUsers {
		users = append(users, item.ID)
	}
	locations := make([]string, 0, len(seedLocations))
	for _, item := range seedLocations {
		locations = append(locations, item.ID)
	}
	notes := make([]string, 0, len(seedNotes))
	for _, item := range seedNotes {
		notes = append(notes, item.ID)
	}
	return users, locations, notes
}

func buildSeedNotes() []note {
	mainTitles := []struct {
		title    string
		category string
		body     string
	}{
		{"[DEMO] The quiet paper-lantern nook", "hidden_gem", "Fictional StreetMap demo only: a tiny lantern-lit seating nook is imagined behind the west arch."},
		{"[DEMO] A calmer path through the loop", "shortcut", "Fictional StreetMap demo only: follow the painted paving marks for a pretend shortcut around the courtyard."},
		{"[DEMO] Ask for the imaginary citrus tea", "local_tip", "Fictional StreetMap demo only: this sample tip describes an invented citrus tea, not a real menu item."},
		{"[DEMO] Sample bike rack by the north gate", "parking", "Fictional StreetMap demo only: the north-gate bike rack is part of this invented test setting."},
		{"[DEMO] A small reading shelf after the arch", "hidden_gem", "Fictional StreetMap demo only: picture a community reading shelf that exists only in this sample dataset."},
		{"[DEMO] Follow the blue tile to the courtyard", "shortcut", "Fictional StreetMap demo only: blue tiles mark a made-up pedestrian route in the fictional quarter."},
		{"[DEMO] The pretend garden opens at dawn", "local_tip", "Fictional StreetMap demo only: opening times here are illustrative and do not describe a real place."},
		{"[DEMO] Sample parking bay near the fountain", "parking", "Fictional StreetMap demo only: this invented parking detail is for interface testing, not driving guidance."},
		{"[DEMO] Tiny mosaic tucked under the awning", "hidden_gem", "Fictional StreetMap demo only: a fictional mosaic gives search testing a distinctive phrase."},
		{"[DEMO] Use the covered arcade in rain", "shortcut", "Fictional StreetMap demo only: the covered arcade is imaginary and is not a real route recommendation."},
		{"[DEMO] Window seat for a quiet pause", "local_tip", "Fictional StreetMap demo only: the window seat and its view are invented for this test note."},
		{"[DEMO] Sample accessible drop-off point", "parking", "Fictional StreetMap demo only: this is not verified accessibility or transport information."},
		{"[DEMO] Hidden painted star on the stair", "hidden_gem", "Fictional StreetMap demo only: the painted star is an invented landmark."},
		{"[DEMO] Cut through the paper garden", "shortcut", "Fictional StreetMap demo only: the paper garden route exists only in the seeded demo."},
		{"[DEMO] Try the fictional ginger biscuit", "local_tip", "Fictional StreetMap demo only: the ginger biscuit is not a real product or business claim."},
		{"[DEMO] Sample scooter parking corner", "parking", "Fictional StreetMap demo only: this invented corner is included to test the parking category."},
		{"[DEMO] A pocket courtyard behind the screen", "hidden_gem", "Fictional StreetMap demo only: a made-up pocket courtyard for testing note expansion."},
		{"[DEMO] Take the low-stair passage", "shortcut", "Fictional StreetMap demo only: this passage is imaginary and must not be used for navigation."},
		{"[DEMO] Look for the pretend blue teapot", "local_tip", "Fictional StreetMap demo only: this whimsical detail is intentionally fictional."},
		{"[DEMO] Sample loading bay note", "parking", "Fictional StreetMap demo only: this note makes pagination and search easy to exercise."},
		{"[DEMO] The lantern-patterned wall", "hidden_gem", "Fictional StreetMap demo only: the wall pattern is invented for StreetMap UI testing."},
	}
	notes := make([]note, 0, primaryLocationNoteCount+9)
	base := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	for i, item := range mainTitles {
		notes = append(notes, note{
			ID:       fmt.Sprintf("30000000-0000-4000-8000-%012d", i+1),
			AuthorID: seedUsers[i%2].ID, LocationID: seedLocations[0].ID,
			Title: item.title, Body: item.body, Category: item.category,
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}
	paginationDetails := []struct {
		phrase   string
		category string
		detail   string
	}{
		{"archway mosaic", "hidden_gem", "an imaginary blue mosaic beneath the arch"},
		{"courtyard footpath", "shortcut", "a made-up path across the paper garden"},
		{"citrus tea counter", "local_tip", "an invented tea counter with no real menu"},
		{"north gate bay", "parking", "a fictional parking bay included for interface testing"},
		{"lantern alcove", "hidden_gem", "an imagined alcove filled with paper lanterns"},
		{"painted stair route", "shortcut", "a pretend stair route that is not navigation advice"},
	}
	for i := len(mainTitles); i < primaryLocationNoteCount; i++ {
		detail := paginationDetails[(i-len(mainTitles))%len(paginationDetails)]
		notes = append(notes, note{
			ID:       fmt.Sprintf("30000000-0000-4000-8000-%012d", i+1),
			AuthorID: seedUsers[i%2].ID, LocationID: seedLocations[0].ID,
			Title:    fmt.Sprintf("[DEMO] %s sample %02d", detail.phrase, i+1),
			Body:     fmt.Sprintf("Fictional StreetMap demo only: %s; numbered sample %02d is included to test search and paginated loading.", detail.detail, i+1),
			Category: detail.category, CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}
	extra := []struct {
		locationIndex int
		title         string
		category      string
		body          string
	}{
		{1, "[DEMO] Glass Garden sample lookout", "hidden_gem", "Fictional StreetMap demo only: the Glass Garden is an invented nearby test location."},
		{2, "[DEMO] Paper Bridge sample passage", "shortcut", "Fictional StreetMap demo only: this made-up passage tests a second nearby distance."},
		{3, "[DEMO] Far lookout outside the radius", "local_tip", "Fictional StreetMap demo only: this note sits beyond five kilometres to test radius limits."},
		{4, "[DEMO] Unverified place reference sample", "local_tip", "Fictional StreetMap demo only: this URL-label reference is not a verified venue."},
		{5, "[DEMO] Fictional quarter sample tip", "local_tip", "Fictional StreetMap demo only: this broad-area note is not tied to a specific venue."},
		{6, "[DEMO] Coordinate-only sample pin", "hidden_gem", "Fictional StreetMap demo only: this note belongs to an exact user-pin test coordinate, not a business."},
		{7, "[DEMO] Quiet Arcade test bench note one", "hidden_gem", "Fictional StreetMap demo only: this verified test location has sparse exact notes so nearby results can supplement them."},
		{7, "[DEMO] Quiet Arcade test bench note two", "local_tip", "Fictional StreetMap demo only: exact notes remain separate from the neighboring test location."},
		{8, "[DEMO] Quiet Arcade nearby sample", "shortcut", "Fictional StreetMap demo only: this note belongs to a different fictional location about 1.1 km away."},
	}
	mainNoteCount := len(notes)
	for i, item := range extra {
		noteNumber := mainNoteCount + i + 1
		notes = append(notes, note{
			ID:       fmt.Sprintf("30000000-0000-4000-8000-%012d", noteNumber),
			AuthorID: seedUsers[(i+1)%2].ID, LocationID: seedLocations[item.locationIndex].ID,
			Title: item.title, Body: item.body, Category: item.category,
			CreatedAt: base.Add(time.Duration(noteNumber) * time.Hour),
		})
	}
	return notes
}
