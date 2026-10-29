package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	maxNearbySupplement  = 20
	maxNearbyRadius      = 5000
	maxQueryDuration     = 3 * time.Second
	exactSparseThreshold = 3
)

var ErrLocationContextNotFound = errors.New("location context not found")

type DBTX interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type PostgresReader struct {
	db DBTX
}

func NewPostgresReader(db DBTX) *PostgresReader {
	return &PostgresReader{db: db}
}

func (reader *PostgresReader) ListPublicNotes(ctx context.Context, query Query) (Page, error) {
	ctx, cancel := context.WithTimeout(ctx, maxQueryDuration)
	defer cancel()

	switch query.Context.Type {
	case ContextVerifiedPlace:
		return reader.listVerifiedPlace(ctx, query)
	case ContextBroadArea:
		return reader.listLabeledContext(ctx, query, "broad_area", MatchBroadArea)
	case ContextUnverifiedPlace:
		return reader.listLabeledContext(ctx, query, "unverified_place_reference", MatchUnverifiedReference)
	case ContextCoordinate:
		return reader.listCoordinate(ctx, query)
	case ContextMapCenter:
		return reader.listMapCenter(ctx, query)
	default:
		return Page{}, errors.New("unsupported note context")
	}
}

func (reader *PostgresReader) listVerifiedPlace(ctx context.Context, query Query) (Page, error) {
	var label string
	var latitude, longitude sql.NullFloat64
	err := reader.db.QueryRowContext(ctx, `
		SELECT display_label, ST_Y(coordinates::geometry), ST_X(coordinates::geometry)
		FROM locations
		WHERE id = $1::uuid AND location_type = 'verified_place'`, query.Context.LocationID).Scan(&label, &latitude, &longitude)
	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, ErrLocationContextNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("read verified location context: %w", err)
	}

	var exactCount int
	if err := reader.db.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE location_id = $1::uuid AND status = 'published'`, query.Context.LocationID).Scan(&exactCount); err != nil {
		return Page{}, fmt.Errorf("count exact public notes: %w", err)
	}
	page := Page{ContextLabel: label, Exact: []Note{}, ContextMatches: []Note{}, Nearby: []Note{}}
	cursor := query.Cursor
	phase := PhaseExact
	if cursor != nil {
		phase = cursor.Phase
	}
	if phase == PhaseExact {
		afterCreatedAt, afterID := keysetValues(cursor)
		rows, err := reader.readSimpleNotes(ctx, exactLocationSQL, query.Context.LocationID, query.Limit+1, afterCreatedAt, afterID)
		if err != nil {
			return Page{}, err
		}
		moreExact := len(rows) > query.Limit
		if moreExact {
			rows = rows[:query.Limit]
		}
		for i := range rows {
			rows[i].MatchType = MatchExactLocation
		}
		page.Exact = rows
		if moreExact {
			page.HasMore = true
			page.NextCursor = newCursor(query, PhaseExact, &rows[len(rows)-1], nearbyCount(cursor))
			return page, nil
		}
		if exactCount >= exactSparseThreshold {
			return page, nil
		}
	}

	if exactCount >= exactSparseThreshold && phase != PhaseNearby {
		return page, nil
	}

	usedNearby := nearbyCount(cursor)
	remaining := query.Limit - len(page.Exact)
	allowedNearby := min(remaining, maxNearbySupplement-usedNearby)
	if allowedNearby < 0 {
		allowedNearby = 0
	}
	nearLimit := allowedNearby
	if nearLimit == 0 && usedNearby < maxNearbySupplement {
		nearLimit = 1 // Probe whether a next page exists after the exact bucket.
	}
	if nearLimit == 0 {
		return page, nil
	}
	nearbyRows, err := reader.readNearby(ctx, query, query.Context.LocationID, nearLimit+1, cursor, latitude, longitude)
	if err != nil {
		return Page{}, err
	}
	moreNearby := len(nearbyRows) > allowedNearby
	if allowedNearby == 0 {
		moreNearby = len(nearbyRows) > 0
		nearbyRows = nil
	} else if moreNearby {
		nearbyRows = nearbyRows[:allowedNearby]
	}
	page.Nearby = nearbyRows
	for i := range page.Nearby {
		page.Nearby[i].MatchType = MatchNearby
	}
	page.HasMore = moreNearby
	if moreNearby {
		if len(nearbyRows) == 0 {
			page.NextCursor = newStartCursor(query, PhaseNearby, usedNearby)
		} else {
			page.NextCursor = newNearbyCursor(query, nearbyRows[len(nearbyRows)-1], usedNearby+len(nearbyRows))
		}
	}
	if usedNearby+len(nearbyRows) >= maxNearbySupplement {
		page.HasMore = false
		page.NextCursor = nil
	}
	return page, nil
}

func (reader *PostgresReader) listLabeledContext(ctx context.Context, query Query, locationType, matchType string) (Page, error) {
	page := Page{Exact: []Note{}, ContextMatches: []Note{}, Nearby: []Note{}}
	var after *Cursor
	if query.Cursor != nil {
		after = query.Cursor
	}
	afterCreatedAt, afterID := keysetValues(after)
	rows, err := reader.readSimpleNotes(ctx, labeledContextSQL, locationType, query.Context.Label, query.Limit+1, afterCreatedAt, afterID)
	if err != nil {
		return Page{}, err
	}
	if len(rows) > query.Limit {
		rows = rows[:query.Limit]
		page.HasMore = true
		page.NextCursor = newCursor(query, PhaseContext, &rows[len(rows)-1], 0)
	}
	for i := range rows {
		rows[i].MatchType = matchType
	}
	page.ContextMatches = rows
	return page, nil
}

func (reader *PostgresReader) listCoordinate(ctx context.Context, query Query) (Page, error) {
	page := Page{Exact: []Note{}, ContextMatches: []Note{}, Nearby: []Note{}}
	afterCreatedAt, afterID := keysetValues(query.Cursor)
	rows, err := reader.readSimpleNotes(ctx, exactCoordinateSQL, query.Context.Longitude, query.Context.Latitude, afterCreatedAt, afterID, query.Limit+1)
	if err != nil {
		return Page{}, err
	}
	if len(rows) > query.Limit {
		rows = rows[:query.Limit]
		page.HasMore = true
		page.NextCursor = newCursor(query, PhaseExact, &rows[len(rows)-1], 0)
	}
	for i := range rows {
		rows[i].MatchType = MatchExactCoordinate
	}
	page.Exact = rows
	return page, nil
}

func (reader *PostgresReader) listMapCenter(ctx context.Context, query Query) (Page, error) {
	page := Page{Exact: []Note{}, ContextMatches: []Note{}, Nearby: []Note{}}
	rows, err := reader.readNearby(ctx, query, "", query.Limit+1, query.Cursor, sql.NullFloat64{}, sql.NullFloat64{})
	if err != nil {
		return Page{}, err
	}
	if len(rows) > query.Limit {
		rows = rows[:query.Limit]
		page.HasMore = true
		page.NextCursor = newNearbyCursor(query, rows[len(rows)-1], 0)
	}
	page.Nearby = rows
	for i := range page.Nearby {
		page.Nearby[i].MatchType = MatchNearby
	}
	return page, nil
}

func (reader *PostgresReader) readSimpleNotes(ctx context.Context, statement string, args ...any) ([]Note, error) {
	rows, err := reader.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query public notes: %w", err)
	}
	defer rows.Close()
	return scanNotes(rows)
}

func (reader *PostgresReader) readNearby(ctx context.Context, query Query, excludeLocationID string, limit int, cursor *Cursor, locationLatitude, locationLongitude sql.NullFloat64) ([]Note, error) {
	var afterDistance, afterCreatedAt, afterID any
	if cursor != nil && cursor.Phase == PhaseNearby && cursor.NoteID != "" {
		afterDistance = cursor.DistanceM
		afterCreatedAt = cursor.CreatedAt
		afterID = cursor.NoteID
	}
	var originLocation any
	var excludedLocation any
	if excludeLocationID != "" {
		originLocation = excludeLocationID
		excludedLocation = excludeLocationID
	}
	var latitude, longitude any
	if locationLatitude.Valid && locationLongitude.Valid {
		latitude = locationLatitude.Float64
		longitude = locationLongitude.Float64
	} else {
		latitude = query.Context.Latitude
		longitude = query.Context.Longitude
	}
	radius := query.Context.RadiusM
	if radius == 0 {
		radius = maxNearbyRadius
	}
	rows, err := reader.db.QueryContext(ctx, nearbySQL,
		originLocation, latitude, longitude, radius, excludedLocation,
		afterDistance, afterCreatedAt, afterID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query nearby public notes: %w", err)
	}
	defer rows.Close()
	return scanNotes(rows)
}

func scanNotes(rows *sql.Rows) ([]Note, error) {
	notes := make([]Note, 0)
	for rows.Next() {
		var note Note
		var distance sql.NullFloat64
		if err := rows.Scan(
			&note.ID, &note.Title, &note.Body, &note.Category, &note.AuthorDisplayName,
			&note.CreatedAt, &note.HelpfulVoteCount, &note.LocationLabel, &distance,
		); err != nil {
			return nil, fmt.Errorf("scan public note: %w", err)
		}
		if distance.Valid {
			note.DistanceM = &distance.Float64
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public notes: %w", err)
	}
	return notes, nil
}

func nearbyCount(cursor *Cursor) int {
	if cursor == nil {
		return 0
	}
	return cursor.NearbyCount
}

func keysetValues(cursor *Cursor) (any, any) {
	if cursor == nil || cursor.NoteID == "" {
		return nil, nil
	}
	return cursor.CreatedAt, cursor.NoteID
}

func newCursor(query Query, phase CursorPhase, note *Note, count int) *Cursor {
	key, _ := ContextKey(query.Context)
	return &Cursor{ContextKey: key, Phase: phase, CreatedAt: note.CreatedAt, NoteID: note.ID, NearbyCount: count}
}

func newNearbyCursor(query Query, note Note, count int) *Cursor {
	key, _ := ContextKey(query.Context)
	distance := 0.0
	if note.DistanceM != nil {
		distance = *note.DistanceM
	}
	return &Cursor{ContextKey: key, Phase: PhaseNearby, CreatedAt: note.CreatedAt, DistanceM: distance, NoteID: note.ID, NearbyCount: count}
}

func newStartCursor(query Query, phase CursorPhase, count int) *Cursor {
	key, _ := ContextKey(query.Context)
	return &Cursor{ContextKey: key, Phase: phase, NearbyCount: count}
}

const exactLocationSQL = `
	SELECT n.id::text, n.title, n.body, n.category, u.display_name, n.created_at,
		(SELECT count(*) FROM votes v WHERE v.note_id = n.id AND v.vote_type = 'helpful')::bigint,
		l.display_label, NULL::double precision
	FROM notes n
	JOIN users u ON u.id = n.author_user_id
	JOIN locations l ON l.id = n.location_id
	WHERE n.status = 'published' AND n.location_id = $1::uuid
		AND ($3::timestamptz IS NULL OR (n.created_at, n.id) < ($3::timestamptz, $4::uuid))
	ORDER BY n.created_at DESC, n.id DESC
	LIMIT $2`

const labeledContextSQL = `
	SELECT n.id::text, n.title, n.body, n.category, u.display_name, n.created_at,
		(SELECT count(*) FROM votes v WHERE v.note_id = n.id AND v.vote_type = 'helpful')::bigint,
		l.display_label, NULL::double precision
	FROM notes n
	JOIN users u ON u.id = n.author_user_id
	JOIN locations l ON l.id = n.location_id
	WHERE n.status = 'published' AND l.location_type = $1
		AND lower(btrim(l.display_label)) = lower(btrim($2))
		AND ($4::timestamptz IS NULL OR (n.created_at, n.id) < ($4::timestamptz, $5::uuid))
	ORDER BY n.created_at DESC, n.id DESC
	LIMIT $3`

const exactCoordinateSQL = `
	SELECT n.id::text, n.title, n.body, n.category, u.display_name, n.created_at,
		(SELECT count(*) FROM votes v WHERE v.note_id = n.id AND v.vote_type = 'helpful')::bigint,
		l.display_label, NULL::double precision
	FROM notes n
	JOIN users u ON u.id = n.author_user_id
	JOIN locations l ON l.id = n.location_id
	WHERE n.status = 'published' AND l.location_type = 'coordinate_only'
		AND ST_Y(l.coordinates::geometry) = $2::double precision
		AND ST_X(l.coordinates::geometry) = $1::double precision
		AND ($3::timestamptz IS NULL OR (n.created_at, n.id) < ($3::timestamptz, $4::uuid))
	ORDER BY n.created_at DESC, n.id DESC
	LIMIT $5`

const nearbySQL = `
	WITH origin(point) AS (
		SELECT coordinates FROM locations
		WHERE $1::uuid IS NOT NULL AND id = $1::uuid AND location_type = 'verified_place'
		UNION ALL
		SELECT ST_SetSRID(ST_MakePoint($3::double precision, $2::double precision), 4326)::geography
		WHERE $1::uuid IS NULL
	), candidates AS (
		SELECT n.id::text AS id, n.title, n.body, n.category, u.display_name AS author_display_name,
			n.created_at, (SELECT count(*) FROM votes v WHERE v.note_id = n.id AND v.vote_type = 'helpful')::bigint AS vote_count,
			l.display_label AS location_label, ST_Distance(l.coordinates, origin.point) AS distance_m
		FROM notes n
		JOIN users u ON u.id = n.author_user_id
		JOIN locations l ON l.id = n.location_id
		CROSS JOIN origin
		WHERE n.status = 'published'
			AND l.coordinates IS NOT NULL
			AND l.location_type IN ('verified_place', 'coordinate_only')
			AND ($5::uuid IS NULL OR n.location_id <> $5::uuid)
			AND ST_DWithin(l.coordinates, origin.point, $4::double precision)
	)
	SELECT id, title, body, category, author_display_name, created_at, vote_count, location_label, distance_m
	FROM candidates
	WHERE $6::double precision IS NULL
		OR distance_m > $6::double precision
		OR (distance_m = $6::double precision AND (created_at, id) < ($7::timestamptz, $8::text))
	ORDER BY distance_m ASC, created_at DESC, id DESC
	LIMIT $9`
