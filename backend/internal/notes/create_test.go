package notes

import (
	"math"
	"strings"
	"testing"
)

func TestNormalizeCreateCommandSupportsLocationKindsAndCategories(t *testing.T) {
	latitude, longitude := 0.0, -0.0
	confirmed := true
	tests := []struct {
		name     string
		location CreateLocation
	}{
		{"verified", CreateLocation{Type: CreateLocationVerified, LocationID: "7C48A51E-3D31-4A34-B503-88747A555A74"}},
		{"unverified label", CreateLocation{Type: CreateLocationUnverified, Label: "  GP Hotel & Banquet  "}},
		{"broad label", CreateLocation{Type: CreateLocationBroad, Label: "  Greater Noida  "}},
		{"confirmed coordinate", CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude, UserConfirmed: &confirmed}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := CreateCommand{Location: test.location, Title: "  Local detail  ", Body: "\nA useful description.\t", Category: "other"}
			normalized, err := NormalizeCreateCommand(command)
			if err != nil {
				t.Fatalf("NormalizeCreateCommand() error = %v", err)
			}
			if normalized.Title != "Local detail" || normalized.Body != "A useful description." {
				t.Fatalf("text was not normalized: %+v", normalized)
			}
			if test.location.Type == CreateLocationVerified && normalized.Location.LocationID != strings.ToLower(test.location.LocationID) {
				t.Fatalf("verified UUID was not normalized: %q", normalized.Location.LocationID)
			}
			if test.location.Type == CreateLocationUnverified && normalized.Location.Label != "GP Hotel & Banquet" {
				t.Fatalf("unverified label was not normalized: %q", normalized.Location.Label)
			}
			if test.location.Type == CreateLocationBroad && normalized.Location.Label != "Greater Noida" {
				t.Fatalf("broad label was not normalized: %q", normalized.Location.Label)
			}
			if test.location.Type == CreateLocationCoordinates && normalized.Location.Label != "User-confirmed coordinates" {
				t.Fatalf("coordinate default label = %q", normalized.Location.Label)
			}
		})
	}
}

func TestNormalizeCreateCommandAcceptsEveryCategory(t *testing.T) {
	for _, category := range []string{"hidden_gem", "shortcut", "local_tip", "parking", "warning", "road_condition", "safety", "other"} {
		t.Run(category, func(t *testing.T) {
			command := validCreateCommand()
			command.Category = category
			if _, err := NormalizeCreateCommand(command); err != nil {
				t.Fatalf("supported category rejected: %v", err)
			}
		})
	}
	command := validCreateCommand()
	command.Category = " local_tip "
	normalized, err := NormalizeCreateCommand(command)
	if err != nil || normalized.Category != "local_tip" {
		t.Fatalf("category whitespace was not normalized: category=%q error=%v", normalized.Category, err)
	}
}

func TestNormalizeCreateCommandRejectsMalformedFields(t *testing.T) {
	confirmed := true
	falseValue := false
	latitude, longitude := 28.5, 77.2
	tests := []struct {
		name      string
		mutate    func(*CreateCommand)
		wantField string
	}{
		{"unsupported category", func(command *CreateCommand) { command.Category = "dangerous" }, "category"},
		{"title too short", func(command *CreateCommand) { command.Title = "  a " }, "title"},
		{"title too long", func(command *CreateCommand) { command.Title = strings.Repeat("x", 121) }, "title"},
		{"title control character", func(command *CreateCommand) { command.Title = "valid\u0000title" }, "title"},
		{"body empty", func(command *CreateCommand) { command.Body = " \n " }, "body"},
		{"body too long", func(command *CreateCommand) { command.Body = strings.Repeat("x", 5001) }, "body"},
		{"unverified missing label", func(command *CreateCommand) { command.Location = CreateLocation{Type: CreateLocationUnverified} }, "location.label"},
		{"unverified ID forbidden", func(command *CreateCommand) {
			command.Location = CreateLocation{Type: CreateLocationUnverified, Label: "Venue", LocationID: "00000000-0000-0000-0000-000000000001"}
		}, "location"},
		{"broad coordinates forbidden", func(command *CreateCommand) {
			command.Location = CreateLocation{Type: CreateLocationBroad, Label: "Town", Latitude: &latitude, Longitude: &longitude}
		}, "location"},
		{"verified malformed ID", func(command *CreateCommand) {
			command.Location = CreateLocation{Type: CreateLocationVerified, LocationID: "not-a-uuid"}
		}, "location"},
		{"verified label forbidden", func(command *CreateCommand) {
			command.Location = CreateLocation{Type: CreateLocationVerified, LocationID: "00000000-0000-0000-0000-000000000001", Label: "Unverified venue"}
		}, "location"},
		{"unknown location kind", func(command *CreateCommand) { command.Location.Type = "map_center" }, "location.type"},
		{"coordinate confirmation missing", func(command *CreateCommand) {
			command.Location = CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude}
		}, "location"},
		{"coordinate confirmation false", func(command *CreateCommand) {
			command.Location = CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude, UserConfirmed: &falseValue}
		}, "location"},
		{"coordinate negative latitude boundary accepted", func(command *CreateCommand) {
			latitude, longitude = -90, 77.2
			command.Location = CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude, UserConfirmed: &confirmed}
		}, ""},
		{"coordinate upper boundaries accepted", func(command *CreateCommand) {
			latitude, longitude = 90, 180
			command.Location = CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude, UserConfirmed: &confirmed}
		}, ""},
		{"coordinate latitude out of bounds", func(command *CreateCommand) {
			latitude, longitude = 90.0001, 77.2
			command.Location = CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude, UserConfirmed: &confirmed}
		}, "location.latitude"},
		{"coordinate longitude out of bounds", func(command *CreateCommand) {
			latitude, longitude = 28.5, -180.0001
			command.Location = CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude, UserConfirmed: &confirmed}
		}, "location.longitude"},
		{"coordinate nonfinite", func(command *CreateCommand) {
			latitude, longitude = math.Inf(1), 77.2
			command.Location = CreateLocation{Type: CreateLocationCoordinates, Latitude: &latitude, Longitude: &longitude, UserConfirmed: &confirmed}
		}, "location.latitude"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := validCreateCommand()
			test.mutate(&command)
			_, err := NormalizeCreateCommand(command)
			if test.wantField == "" {
				if err != nil {
					t.Fatalf("valid boundary rejected: %v", err)
				}
				return
			}
			validationErr, ok := err.(*CreateValidationError)
			if !ok || validationErr.Field != test.wantField {
				t.Fatalf("validation error = %#v, want field %q", err, test.wantField)
			}
		})
	}
}

func TestNormalizeCreateCommandCountsUnicodeCharacters(t *testing.T) {
	command := validCreateCommand()
	command.Title = strings.Repeat("界", 120)
	command.Body = strings.Repeat("語", 5000)
	if _, err := NormalizeCreateCommand(command); err != nil {
		t.Fatalf("maximum Unicode rune lengths rejected: %v", err)
	}
}

func validCreateCommand() CreateCommand {
	return CreateCommand{
		Location: CreateLocation{Type: CreateLocationBroad, Label: "Town"},
		Title:    "Local detail",
		Body:     "A useful description.",
		Category: "local_tip",
	}
}
