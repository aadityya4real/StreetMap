package notes

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const maxCreateDuration = 5 * time.Second

type CreationPolicy struct {
	MaxPerWindow int
	Window       time.Duration
}

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "note creation rate limit exceeded" }

type PostgresCreator struct {
	db     *sql.DB
	policy CreationPolicy
}

func NewPostgresCreator(db *sql.DB, policy CreationPolicy) (*PostgresCreator, error) {
	if db == nil {
		return nil, errors.New("note creator database is required")
	}
	if policy.MaxPerWindow < 1 || policy.MaxPerWindow > 100 || policy.Window < time.Minute || policy.Window > 24*time.Hour {
		return nil, errors.New("note creation policy must allow 1 to 100 notes per 1m to 24h window")
	}
	return &PostgresCreator{db: db, policy: policy}, nil
}

func (creator *PostgresCreator) CreateNote(ctx context.Context, userID, idempotencyKey string, raw CreateCommand) (CreateResult, error) {
	if creator == nil || creator.db == nil {
		return CreateResult{}, errors.New("note creator is not configured")
	}
	if !validUUID(userID) {
		return CreateResult{}, ErrAccountUnavailable
	}
	if !validUUID(idempotencyKey) {
		return CreateResult{}, &CreateValidationError{Field: "Idempotency-Key", Message: "must be a UUID"}
	}
	command, err := NormalizeCreateCommand(raw)
	if err != nil {
		return CreateResult{}, err
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return CreateResult{}, fmt.Errorf("encode canonical note creation payload: %w", err)
	}
	payloadHash := sha256.Sum256(payload)

	ctx, cancel := context.WithTimeout(ctx, maxCreateDuration)
	defer cancel()
	tx, err := creator.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CreateResult{}, fmt.Errorf("begin note creation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var accountStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT account_status
		FROM users
		WHERE id = $1::uuid
		FOR UPDATE`, userID).Scan(&accountStatus)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && accountStatus != "active") {
		return CreateResult{}, ErrAccountUnavailable
	}
	if err != nil {
		return CreateResult{}, fmt.Errorf("lock note author account: %w", err)
	}

	var priorHash []byte
	var priorNoteID string
	err = tx.QueryRowContext(ctx, `
		SELECT request_hash, note_id::text
		FROM note_creation_requests
		WHERE user_id = $1::uuid AND idempotency_key = $2::uuid`, userID, idempotencyKey).Scan(&priorHash, &priorNoteID)
	if err == nil {
		if !equalHash(priorHash, payloadHash[:]) {
			return CreateResult{}, ErrIdempotencyConflict
		}
		note, readErr := readCreatedNote(ctx, tx, priorNoteID, command.Location.Type)
		if readErr != nil {
			return CreateResult{}, readErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return CreateResult{}, fmt.Errorf("commit idempotent note result: %w", commitErr)
		}
		return CreateResult{Note: note, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, fmt.Errorf("lookup note idempotency request: %w", err)
	}

	if rateErr := creator.checkRateLimit(ctx, tx, userID); rateErr != nil {
		return CreateResult{}, rateErr
	}

	locationID, locationLabel, err := persistCreateLocation(ctx, tx, command.Location)
	if err != nil {
		return CreateResult{}, err
	}
	var noteID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO notes (author_user_id, location_id, title, body, category, status, created_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'published', statement_timestamp())
		RETURNING id::text`, userID, locationID, command.Title, command.Body, command.Category).Scan(&noteID)
	if err != nil {
		return CreateResult{}, fmt.Errorf("insert published note: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO note_creation_requests (user_id, idempotency_key, request_hash, note_id)
		VALUES ($1::uuid, $2::uuid, $3, $4::uuid)`, userID, idempotencyKey, payloadHash[:], noteID); err != nil {
		return CreateResult{}, fmt.Errorf("record note idempotency request: %w", err)
	}

	note, err := readCreatedNote(ctx, tx, noteID, command.Location.Type)
	if err != nil {
		return CreateResult{}, err
	}
	note.LocationLabel = locationLabel
	if err := tx.Commit(); err != nil {
		return CreateResult{}, fmt.Errorf("commit note creation: %w", err)
	}
	return CreateResult{Note: note}, nil
}

func (creator *PostgresCreator) checkRateLimit(ctx context.Context, tx *sql.Tx, userID string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM notes
		WHERE author_user_id = $1::uuid AND status = 'published'
			AND created_at >= statement_timestamp() - ($2::double precision * interval '1 second')`, userID, creator.policy.Window.Seconds()).Scan(&count); err != nil {
		return fmt.Errorf("count recent note creations: %w", err)
	}
	if count < creator.policy.MaxPerWindow {
		return nil
	}
	var oldestCreatedAt, databaseNow time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT created_at, statement_timestamp()
		FROM notes
		WHERE author_user_id = $1::uuid AND status = 'published'
			AND created_at >= statement_timestamp() - ($2::double precision * interval '1 second')
		ORDER BY created_at ASC
		OFFSET $3 LIMIT 1`, userID, creator.policy.Window.Seconds(), creator.policy.MaxPerWindow-1).Scan(&oldestCreatedAt, &databaseNow)
	if errors.Is(err, sql.ErrNoRows) {
		// A note may age out between the count and this query; that opens a slot.
		return nil
	}
	if err != nil {
		return fmt.Errorf("calculate note creation rate limit: %w", err)
	}
	retryAfter := oldestCreatedAt.Add(creator.policy.Window).Sub(databaseNow)
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return &RateLimitError{RetryAfter: retryAfter}
}

func persistCreateLocation(ctx context.Context, tx *sql.Tx, location CreateLocation) (string, string, error) {
	if location.Type == CreateLocationVerified {
		var label string
		err := tx.QueryRowContext(ctx, `
			SELECT display_label
			FROM locations
			WHERE id = $1::uuid AND location_type = 'verified_place'`, location.LocationID).Scan(&label)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrLocationNotFound
		}
		if err != nil {
			return "", "", fmt.Errorf("resolve verified note location: %w", err)
		}
		return location.LocationID, label, nil
	}

	var locationType string
	switch location.Type {
	case CreateLocationUnverified:
		locationType = "unverified_place_reference"
	case CreateLocationBroad:
		locationType = "broad_area"
	case CreateLocationCoordinates:
		locationType = "coordinate_only"
	default:
		return "", "", &CreateValidationError{Field: "location.type", Message: "must be a supported location type"}
	}

	var locationID string
	var err error
	if locationType == "coordinate_only" {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO locations (location_type, display_label, coordinates)
			VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3::double precision, $4::double precision), 4326)::geography)
			RETURNING id::text`, locationType, location.Label, *location.Longitude, *location.Latitude).Scan(&locationID)
	} else {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO locations (location_type, display_label)
			VALUES ($1, $2)
			RETURNING id::text`, locationType, location.Label).Scan(&locationID)
	}
	if err != nil {
		return "", "", fmt.Errorf("create note location: %w", err)
	}
	return locationID, location.Label, nil
}

func readCreatedNote(ctx context.Context, tx *sql.Tx, noteID, inputLocationType string) (Note, error) {
	var note Note
	err := tx.QueryRowContext(ctx, `
		SELECT n.id::text, n.title, n.body, n.category, u.display_name, n.created_at,
			(SELECT count(*) FROM votes v WHERE v.note_id = n.id AND v.vote_type = 'helpful')::bigint,
			l.display_label
		FROM notes n
	JOIN users u ON u.id = n.author_user_id
	JOIN locations l ON l.id = n.location_id
	WHERE n.id = $1::uuid`, noteID).Scan(
		&note.ID, &note.Title, &note.Body, &note.Category, &note.AuthorDisplayName,
		&note.CreatedAt, &note.HelpfulVoteCount, &note.LocationLabel,
	)
	if err != nil {
		return Note{}, fmt.Errorf("read created note: %w", err)
	}
	switch inputLocationType {
	case CreateLocationVerified:
		note.MatchType = MatchExactLocation
	case CreateLocationBroad:
		note.MatchType = MatchBroadArea
	case CreateLocationUnverified:
		note.MatchType = MatchUnverifiedReference
	case CreateLocationCoordinates:
		note.MatchType = MatchExactCoordinate
	}
	return note, nil
}

func equalHash(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}
