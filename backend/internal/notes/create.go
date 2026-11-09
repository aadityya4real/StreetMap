package notes

import (
	"context"
	"errors"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	CreateLocationVerified    = "verified"
	CreateLocationUnverified  = "unverified"
	CreateLocationBroad       = "broad"
	CreateLocationCoordinates = "coordinates"
	maxCreateLabelLength      = 200
)

var (
	ErrAccountUnavailable  = errors.New("account is unavailable")
	ErrLocationNotFound    = errors.New("verified location was not found")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different payload")
)

type CreateLocation struct {
	Type          string
	LocationID    string
	Label         string
	Latitude      *float64
	Longitude     *float64
	UserConfirmed *bool
}

type CreateCommand struct {
	Location CreateLocation
	Title    string
	Body     string
	Category string
}

type CreateValidationError struct {
	Field   string
	Message string
}

func (e *CreateValidationError) Error() string {
	return e.Field + " " + e.Message
}

type CreateResult struct {
	Note     Note
	Replayed bool
}

type Creator interface {
	CreateNote(context.Context, string, string, CreateCommand) (CreateResult, error)
}

func NormalizeCreateCommand(command CreateCommand) (CreateCommand, error) {
	command.Title = strings.TrimSpace(command.Title)
	if err := validateText(command.Title, 3, 120, false); err != nil {
		return CreateCommand{}, &CreateValidationError{Field: "title", Message: "must contain 3 to 120 characters without control characters"}
	}
	command.Body = strings.TrimSpace(command.Body)
	if err := validateText(command.Body, 1, 5000, true); err != nil {
		return CreateCommand{}, &CreateValidationError{Field: "body", Message: "must contain 1 to 5000 characters"}
	}
	command.Category = strings.TrimSpace(command.Category)
	if !supportedCategory(command.Category) {
		return CreateCommand{}, &CreateValidationError{Field: "category", Message: "must be a supported note category"}
	}

	location := &command.Location
	switch location.Type {
	case CreateLocationVerified:
		if !validUUID(location.LocationID) || location.Label != "" || location.Latitude != nil || location.Longitude != nil || location.UserConfirmed != nil {
			return CreateCommand{}, &CreateValidationError{Field: "location", Message: "verified locations require only a valid location_id"}
		}
		location.LocationID = strings.ToLower(location.LocationID)
	case CreateLocationUnverified, CreateLocationBroad:
		if location.LocationID != "" || location.Latitude != nil || location.Longitude != nil || location.UserConfirmed != nil {
			return CreateCommand{}, &CreateValidationError{Field: "location", Message: "label contexts cannot include IDs, coordinates, or confirmation"}
		}
		location.Label = strings.TrimSpace(location.Label)
		if err := validateText(location.Label, 1, maxCreateLabelLength, false); err != nil {
			return CreateCommand{}, &CreateValidationError{Field: "location.label", Message: "must contain 1 to 200 characters without control characters"}
		}
	case CreateLocationCoordinates:
		if location.LocationID != "" || location.Latitude == nil || location.Longitude == nil || location.UserConfirmed == nil || !*location.UserConfirmed {
			return CreateCommand{}, &CreateValidationError{Field: "location", Message: "coordinates require latitude, longitude, and user_confirmed=true"}
		}
		if math.IsNaN(*location.Latitude) || math.IsInf(*location.Latitude, 0) || *location.Latitude < -90 || *location.Latitude > 90 {
			return CreateCommand{}, &CreateValidationError{Field: "location.latitude", Message: "must be a finite value from -90 to 90"}
		}
		if math.IsNaN(*location.Longitude) || math.IsInf(*location.Longitude, 0) || *location.Longitude < -180 || *location.Longitude > 180 {
			return CreateCommand{}, &CreateValidationError{Field: "location.longitude", Message: "must be a finite value from -180 to 180"}
		}
		// Canonicalize negative zero so semantically equal coordinates share a
		// stable idempotency payload hash.
		if *location.Latitude == 0 {
			*location.Latitude = 0
		}
		if *location.Longitude == 0 {
			*location.Longitude = 0
		}
		location.Label = strings.TrimSpace(location.Label)
		if location.Label == "" {
			location.Label = "User-confirmed coordinates"
		}
		if err := validateText(location.Label, 1, maxCreateLabelLength, false); err != nil {
			return CreateCommand{}, &CreateValidationError{Field: "location.label", Message: "must contain 1 to 200 characters without control characters"}
		}
	default:
		return CreateCommand{}, &CreateValidationError{Field: "location.type", Message: "must be verified, unverified, broad, or coordinates"}
	}
	return command, nil
}

func validateText(value string, minimum, maximum int, allowBodyWhitespace bool) error {
	if !utf8.ValidString(value) {
		return errors.New("invalid UTF-8")
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return errors.New("text length outside bounds")
	}
	for _, character := range value {
		if !unicode.IsControl(character) {
			continue
		}
		if allowBodyWhitespace && (character == '\n' || character == '\r' || character == '\t') {
			continue
		}
		return errors.New("control characters are not allowed")
	}
	return nil
}

func supportedCategory(category string) bool {
	switch category {
	case "hidden_gem", "shortcut", "local_tip", "parking", "warning", "road_condition", "safety", "other":
		return true
	default:
		return false
	}
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
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
