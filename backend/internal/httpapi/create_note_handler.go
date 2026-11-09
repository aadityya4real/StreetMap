package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/notes"
)

const maxCreateNoteRequestBytes = 32 << 10

type createNoteRequest struct {
	Location *createLocationRequest `json:"location"`
	Title    string                 `json:"title"`
	Body     string                 `json:"body"`
	Category string                 `json:"category"`
}

type createLocationRequest struct {
	Type          string   `json:"type"`
	LocationID    string   `json:"location_id,omitempty"`
	Label         string   `json:"label,omitempty"`
	Latitude      *float64 `json:"latitude,omitempty"`
	Longitude     *float64 `json:"longitude,omitempty"`
	UserConfirmed *bool    `json:"user_confirmed,omitempty"`
}

type createdNoteData struct {
	Note publicNoteResponse `json:"note"`
}

func (h Handler) createNote(w http.ResponseWriter, r *http.Request) {
	principal, ok := AuthenticatedPrincipal(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "A valid bearer access token is required.", h.logger)
		return
	}
	if h.noteCreator == nil {
		writeError(w, http.StatusServiceUnavailable, "note_creation_unavailable", "Note creation is temporarily unavailable.", h.logger)
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "The request must use application/json.", h.logger)
		return
	}
	idempotencyKeys := r.Header.Values("Idempotency-Key")
	if len(idempotencyKeys) != 1 || !validUUID(strings.TrimSpace(idempotencyKeys[0])) {
		writeCreateValidationError(w, &noteSearchError{Field: "Idempotency-Key", Message: "must be one UUID value"}, h.logger)
		return
	}
	idempotencyKey := strings.ToLower(strings.TrimSpace(idempotencyKeys[0]))

	r.Body = http.MaxBytesReader(w, r.Body, maxCreateNoteRequestBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The request body exceeds the allowed size.", h.logger)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body must be valid note JSON.", h.logger)
		return
	}
	if !utf8.Valid(body) {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body must be valid UTF-8 JSON.", h.logger)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request createNoteRequest
	if err := decoder.Decode(&request); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The request body exceeds the allowed size.", h.logger)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body must be valid note JSON.", h.logger)
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body must contain one JSON value.", h.logger)
		return
	}
	if request.Location == nil {
		writeCreateValidationError(w, &noteSearchError{Field: "location", Message: "is required"}, h.logger)
		return
	}
	command := notes.CreateCommand{
		Location: notes.CreateLocation{
			Type: request.Location.Type, LocationID: request.Location.LocationID,
			Label: request.Location.Label, Latitude: request.Location.Latitude,
			Longitude: request.Location.Longitude, UserConfirmed: request.Location.UserConfirmed,
		},
		Title: request.Title, Body: request.Body, Category: request.Category,
	}
	command, validationErr := notes.NormalizeCreateCommand(command)
	if validationErr != nil {
		var fieldErr *notes.CreateValidationError
		if errors.As(validationErr, &fieldErr) {
			writeCreateValidationError(w, &noteSearchError{Field: fieldErr.Field, Message: fieldErr.Message}, h.logger)
			return
		}
		writeError(w, http.StatusBadRequest, "validation_error", "One or more note fields are invalid.", h.logger)
		return
	}

	result, err := h.noteCreator.CreateNote(r.Context(), principal.UserID, idempotencyKey, command)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAccountUnavailable), errors.Is(err, notes.ErrAccountUnavailable):
			writeError(w, http.StatusForbidden, "account_unavailable", "This StreetMap account is not available.", h.logger)
		case errors.Is(err, notes.ErrLocationNotFound):
			writeError(w, http.StatusNotFound, "location_not_found", "The verified StreetMap location was not found.", h.logger)
		case errors.Is(err, notes.ErrIdempotencyConflict):
			writeError(w, http.StatusConflict, "idempotency_conflict", "This idempotency key was already used for a different note request.", h.logger)
		default:
			var rateErr *notes.RateLimitError
			if errors.As(err, &rateErr) {
				retrySeconds := int(math.Ceil(rateErr.RetryAfter.Seconds()))
				if retrySeconds < 1 {
					retrySeconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				writeError(w, http.StatusTooManyRequests, "note_creation_rate_limited", "Too many notes were created recently. Please try again later.", h.logger)
				return
			}
			h.logger.Error("create note request failed")
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.", h.logger)
			return
		}
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeSuccess(w, status, createdNoteData{Note: mapPublicNotes([]notes.Note{result.Note})[0]}, h.logger)
}

func isJSONContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func writeCreateValidationError(w http.ResponseWriter, validationErr *noteSearchError, logger *slog.Logger) {
	if validationErr == nil {
		writeError(w, http.StatusBadRequest, "validation_error", "One or more note fields are invalid.", logger)
		return
	}
	body := envelope{Error: &apiError{
		Code: "validation_error", Message: "One or more note fields are invalid.",
		Fields: map[string]string{validationErr.Field: validationErr.Message},
	}}
	writeJSON(w, http.StatusBadRequest, body, logger)
}
