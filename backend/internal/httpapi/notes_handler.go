package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"streetmap.in/backend/internal/notes"
)

const (
	defaultNotesLimit = 20
	maxNotesLimit     = 50
	maxLabelLength    = 200
	maxCursorInput    = 1024
	maxNearbyRadiusM  = 5000
)

type noteSearchError struct {
	Field   string
	Message string
}

type noteContextResponse struct {
	Type       string   `json:"type"`
	LocationID string   `json:"location_id,omitempty"`
	Label      string   `json:"label,omitempty"`
	Latitude   *float64 `json:"latitude,omitempty"`
	Longitude  *float64 `json:"longitude,omitempty"`
}

type noteMatchResponse struct {
	Type          string `json:"type"`
	LocationLabel string `json:"location_label"`
	DistanceM     *int64 `json:"distance_m,omitempty"`
}

type publicNoteResponse struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Body             string            `json:"body"`
	Category         string            `json:"category"`
	Author           publicAuthor      `json:"author"`
	CreatedAt        string            `json:"created_at"`
	HelpfulVoteCount int64             `json:"helpful_vote_count"`
	Match            noteMatchResponse `json:"match"`
}

type publicAuthor struct {
	DisplayName string `json:"display_name"`
}

type notesPageResponse struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

type notesDataResponse struct {
	Context        noteContextResponse  `json:"context"`
	Exact          []publicNoteResponse `json:"exact"`
	ContextMatches []publicNoteResponse `json:"context_matches"`
	Nearby         []publicNoteResponse `json:"nearby"`
	Page           notesPageResponse    `json:"page"`
}

func (h Handler) listNotes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w)
		return
	}
	if h.noteReader == nil {
		h.logger.Error("public notes reader is not configured")
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.", h.logger)
		return
	}
	query, validationErr := parseNotesQuery(r.URL.RawQuery)
	if validationErr != nil {
		writeValidationError(w, validationErr, h.logger)
		return
	}
	contextKey, keyErr := notes.ContextKey(query.Context)
	if keyErr != nil {
		h.logger.Error("create notes query key", "error", keyErr)
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.", h.logger)
		return
	}
	if query.Cursor != nil && query.Cursor.ContextKey != contextKey {
		writeValidationError(w, &noteSearchError{Field: "cursor", Message: "must belong to this query"}, h.logger)
		return
	}
	page, err := h.noteReader.ListPublicNotes(r.Context(), query)
	if err != nil {
		if errors.Is(err, notes.ErrLocationContextNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "No public location context was found.", h.logger)
			return
		}
		h.logger.Error("list public notes failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.", h.logger)
		return
	}
	var nextCursor *string
	if page.NextCursor != nil {
		encoded, encodeErr := notes.EncodeCursor(*page.NextCursor)
		if encodeErr != nil {
			h.logger.Error("encode public notes cursor", "error", encodeErr)
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.", h.logger)
			return
		}
		nextCursor = &encoded
	}
	contextResponse := noteContextResponse{Type: query.Context.Type, LocationID: query.Context.LocationID, Label: query.Context.Label}
	if contextResponse.Label == "" {
		contextResponse.Label = page.ContextLabel
	}
	if query.Context.Type == notes.ContextMapCenter {
		latitude, longitude := query.Context.Latitude, query.Context.Longitude
		contextResponse.Latitude = &latitude
		contextResponse.Longitude = &longitude
	}
	response := notesDataResponse{
		Context:        contextResponse,
		Exact:          mapPublicNotes(page.Exact),
		ContextMatches: mapPublicNotes(page.ContextMatches),
		Nearby:         mapPublicNotes(page.Nearby),
		Page:           notesPageResponse{NextCursor: nextCursor, HasMore: page.HasMore},
	}
	writeSuccess(w, http.StatusOK, response, h.logger)
}

func parseNotesQuery(rawQuery string) (notes.Query, *noteSearchError) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return notes.Query{}, &noteSearchError{Field: "query", Message: "is malformed"}
	}
	allowed := map[string]bool{
		"location_id": true, "context_type": true, "label": true,
		"latitude": true, "longitude": true, "radius_m": true,
		"limit": true, "cursor": true,
	}
	for key, entries := range values {
		if !allowed[key] {
			return notes.Query{}, &noteSearchError{Field: key, Message: "is not supported"}
		}
		if len(entries) != 1 {
			return notes.Query{}, &noteSearchError{Field: key, Message: "must be supplied once"}
		}
	}
	get := func(key string) string { return values.Get(key) }

	limit := defaultNotesLimit
	if values.Has("limit") {
		parsed, parseErr := strconv.Atoi(get("limit"))
		if parseErr != nil || parsed < 1 || parsed > maxNotesLimit {
			return notes.Query{}, &noteSearchError{Field: "limit", Message: "must be an integer from 1 to 50"}
		}
		limit = parsed
	}
	query := notes.Query{Limit: limit}

	if values.Has("cursor") {
		rawCursor := get("cursor")
		if len(rawCursor) > maxCursorInput {
			return notes.Query{}, &noteSearchError{Field: "cursor", Message: "is too long"}
		}
		contextType := get("context_type")
		if values.Has("location_id") {
			contextType = notes.ContextVerifiedPlace
		}
		context, contextErr := parseNotesContext(values, contextType)
		if contextErr != nil {
			return notes.Query{}, contextErr
		}
		contextKey, keyErr := notes.ContextKey(context)
		if keyErr != nil {
			return notes.Query{}, &noteSearchError{Field: "cursor", Message: "is invalid"}
		}
		cursor, cursorErr := notes.DecodeCursor(rawCursor, contextKey)
		if cursorErr != nil || !cursorPhaseAllowed(context.Type, cursor.Phase) {
			return notes.Query{}, &noteSearchError{Field: "cursor", Message: "is invalid for this context"}
		}
		query.Context = context
		query.Cursor = cursor
		return query, nil
	}
	contextType := get("context_type")
	if values.Has("location_id") {
		contextType = notes.ContextVerifiedPlace
	}
	context, contextErr := parseNotesContext(values, contextType)
	if contextErr != nil {
		return notes.Query{}, contextErr
	}
	query.Context = context
	return query, nil
}

func parseNotesContext(values url.Values, contextType string) (notes.Context, *noteSearchError) {
	locationIDPresent := values.Has("location_id")
	contextTypePresent := values.Has("context_type")
	if locationIDPresent == contextTypePresent {
		return notes.Context{}, &noteSearchError{Field: "context_type", Message: "provide exactly one location context"}
	}
	context := notes.Context{Type: contextType}
	switch contextType {
	case notes.ContextVerifiedPlace:
		if !locationIDPresent || !validUUID(values.Get("location_id")) || hasAny(values, "label", "latitude", "longitude", "radius_m") {
			return notes.Context{}, &noteSearchError{Field: "location_id", Message: "must be a UUID for a verified place"}
		}
		context.LocationID = strings.ToLower(values.Get("location_id"))
	case notes.ContextBroadArea, notes.ContextUnverifiedPlace:
		if locationIDPresent || hasAny(values, "latitude", "longitude", "radius_m") {
			return notes.Context{}, &noteSearchError{Field: "context_type", Message: "has incompatible parameters"}
		}
		label, labelErr := parseLabel(values)
		if labelErr != nil {
			return notes.Context{}, labelErr
		}
		context.Label = label
	case notes.ContextMapCenter:
		if locationIDPresent {
			return notes.Context{}, &noteSearchError{Field: "context_type", Message: "has incompatible parameters"}
		}
		if values.Has("label") {
			label, labelErr := parseLabel(values)
			if labelErr != nil {
				return notes.Context{}, labelErr
			}
			context.Label = label
		}
		latitude, coordinateErr := parseCoordinate(values, "latitude", -90, 90)
		if coordinateErr != nil {
			return notes.Context{}, coordinateErr
		}
		longitude, coordinateErr := parseCoordinate(values, "longitude", -180, 180)
		if coordinateErr != nil {
			return notes.Context{}, coordinateErr
		}
		context.Latitude, context.Longitude = latitude, longitude
		context.RadiusM = maxNearbyRadiusM
		if values.Has("radius_m") {
			radius, parseErr := strconv.Atoi(values.Get("radius_m"))
			if parseErr != nil || radius < 1 || radius > maxNearbyRadiusM {
				return notes.Context{}, &noteSearchError{Field: "radius_m", Message: "must be an integer from 1 to 5000"}
			}
			context.RadiusM = radius
		}
	case notes.ContextCoordinate:
		if locationIDPresent || hasAny(values, "label", "radius_m") {
			return notes.Context{}, &noteSearchError{Field: "context_type", Message: "has incompatible parameters"}
		}
		latitude, coordinateErr := parseCoordinate(values, "latitude", -90, 90)
		if coordinateErr != nil {
			return notes.Context{}, coordinateErr
		}
		longitude, coordinateErr := parseCoordinate(values, "longitude", -180, 180)
		if coordinateErr != nil {
			return notes.Context{}, coordinateErr
		}
		context.Latitude, context.Longitude = latitude, longitude
	default:
		return notes.Context{}, &noteSearchError{Field: "context_type", Message: "must be a supported location context"}
	}
	return context, nil
}

func parseLabel(values url.Values) (string, *noteSearchError) {
	if !values.Has("label") {
		return "", &noteSearchError{Field: "label", Message: "is required"}
	}
	label := strings.TrimSpace(values.Get("label"))
	if label == "" || !utf8.ValidString(label) || utf8.RuneCountInString(label) > maxLabelLength {
		return "", &noteSearchError{Field: "label", Message: "must contain 1 to 200 characters"}
	}
	return label, nil
}

func parseCoordinate(values url.Values, field string, minimum, maximum float64) (float64, *noteSearchError) {
	if !values.Has(field) {
		return 0, &noteSearchError{Field: field, Message: "is required"}
	}
	coordinate, err := strconv.ParseFloat(values.Get(field), 64)
	if err != nil || math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || coordinate < minimum || coordinate > maximum {
		return 0, &noteSearchError{Field: field, Message: fmt.Sprintf("must be a finite value from %v to %v", minimum, maximum)}
	}
	return coordinate, nil
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, character := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func hasAny(values url.Values, keys ...string) bool {
	for _, key := range keys {
		if values.Has(key) {
			return true
		}
	}
	return false
}

func cursorPhaseAllowed(contextType string, phase notes.CursorPhase) bool {
	switch contextType {
	case notes.ContextVerifiedPlace:
		return phase == notes.PhaseExact || phase == notes.PhaseNearby
	case notes.ContextBroadArea, notes.ContextUnverifiedPlace:
		return phase == notes.PhaseContext
	case notes.ContextCoordinate:
		return phase == notes.PhaseExact
	case notes.ContextMapCenter:
		return phase == notes.PhaseNearby
	default:
		return false
	}
}

func mapPublicNotes(notesToMap []notes.Note) []publicNoteResponse {
	response := make([]publicNoteResponse, 0, len(notesToMap))
	for _, note := range notesToMap {
		match := noteMatchResponse{Type: note.MatchType, LocationLabel: note.LocationLabel}
		if note.DistanceM != nil {
			distance := int64(math.Round(*note.DistanceM/10) * 10)
			match.DistanceM = &distance
		}
		response = append(response, publicNoteResponse{
			ID: note.ID, Title: note.Title, Body: note.Body, Category: note.Category,
			Author:           publicAuthor{DisplayName: note.AuthorDisplayName},
			CreatedAt:        note.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"),
			HelpfulVoteCount: note.HelpfulVoteCount, Match: match,
		})
	}
	return response
}

func writeValidationError(w http.ResponseWriter, err *noteSearchError, logger *slog.Logger) {
	if err == nil {
		writeError(w, http.StatusBadRequest, "validation_error", "The request parameters are invalid.", logger)
		return
	}
	body := envelope{Error: &apiError{
		Code: "validation_error", Message: "One or more request parameters are invalid.",
		Fields: map[string]string{err.Field: err.Message},
	}}
	writeJSON(w, http.StatusBadRequest, body, logger)
}
