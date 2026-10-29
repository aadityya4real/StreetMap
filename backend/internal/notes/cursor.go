package notes

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strings"
)

const maxCursorLength = 1024

var canonicalUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ContextKey(context Context) (string, error) {
	encoded, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func EncodeCursor(cursor Cursor) (string, error) {
	if err := validateCursor(cursor); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func DecodeCursor(raw, contextKey string) (*Cursor, error) {
	if raw == "" || len(raw) > maxCursorLength || strings.ContainsAny(raw, "= \r\n") {
		return nil, errors.New("invalid cursor")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid cursor")
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var cursor Cursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid cursor")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid cursor")
	}
	if err := validateCursor(cursor); err != nil || cursor.ContextKey != contextKey {
		return nil, errors.New("invalid cursor")
	}
	return &cursor, nil
}

func validateCursor(cursor Cursor) error {
	if len(cursor.ContextKey) != sha256.Size*2 {
		return errors.New("invalid cursor context")
	}
	switch cursor.Phase {
	case PhaseExact, PhaseContext:
		if cursor.CreatedAt.IsZero() || !canonicalUUID.MatchString(cursor.NoteID) || cursor.DistanceM != 0 {
			return errors.New("invalid keyset cursor")
		}
	case PhaseNearby:
		if cursor.NearbyCount < 0 || cursor.NearbyCount > maxNearbySupplement {
			return errors.New("invalid nearby cursor")
		}
		if cursor.NoteID == "" && cursor.CreatedAt.IsZero() && cursor.DistanceM == 0 {
			return nil
		}
		if cursor.CreatedAt.IsZero() || !canonicalUUID.MatchString(cursor.NoteID) || math.IsNaN(cursor.DistanceM) || math.IsInf(cursor.DistanceM, 0) || cursor.DistanceM < 0 || cursor.DistanceM > 5000 {
			return errors.New("invalid nearby keyset cursor")
		}
	default:
		return errors.New("invalid cursor phase")
	}
	return nil
}
