package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"streetmap.in/backend/internal/notes"
)

type notesReaderFunc func(context.Context, notes.Query) (notes.Page, error)

func (reader notesReaderFunc) ListPublicNotes(ctx context.Context, query notes.Query) (notes.Page, error) {
	return reader(ctx, query)
}

func TestListNotesParsesSupportedContexts(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantType   string
		wantLabel  string
		wantRadius int
	}{
		{"verified location", "location_id=7c48a51e-3d31-4a34-b503-88747a555a74", notes.ContextVerifiedPlace, "", 0},
		{"broad area", "context_type=broad_area&label=Greater+Noida%2C+Uttar+Pradesh", notes.ContextBroadArea, "Greater Noida, Uttar Pradesh", 0},
		{"unverified place label", "context_type=place_page_unverified&label=GP+Hotel+%26+Banquet", notes.ContextUnverifiedPlace, "GP Hotel & Banquet", 0},
		{"map center", "context_type=map_center&label=GP+Hotel&latitude=26.8178068&longitude=80.9071453", notes.ContextMapCenter, "GP Hotel", 5000},
		{"user selected coordinate", "context_type=coordinate&latitude=28.6&longitude=77.2", notes.ContextCoordinate, "", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got notes.Query
			handler := notesHandler(notesReaderFunc(func(_ context.Context, query notes.Query) (notes.Page, error) {
				got = query
				return emptyNotesPage(), nil
			}))
			response := request(handler, http.MethodGet, "/api/v1/notes?"+test.query)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			if got.Context.Type != test.wantType || got.Context.Label != test.wantLabel || got.Context.RadiusM != test.wantRadius {
				t.Fatalf("parsed context = %+v", got.Context)
			}
		})
	}
}

func TestListNotesRejectsInvalidContextsAndBounds(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
	}{
		{"missing context", "", "context_type"},
		{"duplicate selector", "location_id=7c48a51e-3d31-4a34-b503-88747a555a74&context_type=broad_area&label=Town", "context_type"},
		{"invalid uuid", "location_id=not-a-uuid", "location_id"},
		{"missing broad label", "context_type=broad_area", "label"},
		{"label too long", "context_type=broad_area&label=" + strings.Repeat("x", 201), "label"},
		{"unknown context", "context_type=place", "context_type"},
		{"map center missing coordinates", "context_type=map_center&latitude=1", "longitude"},
		{"invalid latitude", "context_type=map_center&latitude=91&longitude=1", "latitude"},
		{"nonfinite longitude", "context_type=map_center&latitude=1&longitude=NaN", "longitude"},
		{"radius over limit", "context_type=map_center&latitude=1&longitude=1&radius_m=5001", "radius_m"},
		{"zero limit", "context_type=broad_area&label=Town&limit=0", "limit"},
		{"too many results", "context_type=broad_area&label=Town&limit=51", "limit"},
		{"repeated value", "context_type=broad_area&label=Town&label=Other", "label"},
		{"unknown parameter", "context_type=broad_area&label=Town&author_user_id=1", "author_user_id"},
		{"malformed escaping", "context_type=broad_area&label=%ZZ", "query"},
		{"incompatible nearby radius", "context_type=broad_area&label=Town&radius_m=10", "context_type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler := notesHandler(notesReaderFunc(func(context.Context, notes.Query) (notes.Page, error) {
				called = true
				return emptyNotesPage(), nil
			}))
			response := request(handler, http.MethodGet, "/api/v1/notes?"+test.query)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"`+test.field+`"`) || called {
				t.Fatalf("missing field error or reader was called: body=%s called=%v", response.Body.String(), called)
			}
		})
	}
}

func TestListNotesReturnsOnlyPublicFieldsAndLabelsMatches(t *testing.T) {
	distance := 643.7
	handler := notesHandler(notesReaderFunc(func(context.Context, notes.Query) (notes.Page, error) {
		return notes.Page{
			Exact:  []notes.Note{{ID: "00000000-0000-0000-0000-000000000001", Title: "Exact note", Body: "A useful detail.", Category: "local_tip", AuthorDisplayName: "Mira", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), HelpfulVoteCount: 2, LocationLabel: "Verified cafe", MatchType: notes.MatchExactLocation}},
			Nearby: []notes.Note{{ID: "00000000-0000-0000-0000-000000000002", Title: "Nearby note", Body: "A nearby detail.", Category: "hidden_gem", AuthorDisplayName: "Dev", CreatedAt: time.Date(2026, 1, 1, 3, 4, 5, 0, time.UTC), LocationLabel: "Nearby market", DistanceM: &distance, MatchType: notes.MatchNearby}},
		}, nil
	}))
	response := request(handler, http.MethodGet, "/api/v1/notes?location_id=7c48a51e-3d31-4a34-b503-88747a555a74")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "email") || strings.Contains(body, "account_status") || strings.Index(body, "exact_location") > strings.Index(body, "nearby") {
		t.Fatalf("response exposed private fields or did not preserve match groups: %s", body)
	}
	var decoded struct {
		Data notesDataResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data.Exact) != 1 || len(decoded.Data.Nearby) != 1 || decoded.Data.Nearby[0].Match.DistanceM == nil || *decoded.Data.Nearby[0].Match.DistanceM != 640 {
		t.Fatalf("unexpected response groups or rounded nearby distance: %+v", decoded.Data)
	}
}

func TestListNotesHandlesMissingLocationContextAndUnsupportedMethod(t *testing.T) {
	handler := notesHandler(notesReaderFunc(func(context.Context, notes.Query) (notes.Page, error) {
		return notes.Page{}, notes.ErrLocationContextNotFound
	}))
	missing := request(handler, http.MethodGet, "/api/v1/notes?location_id=7c48a51e-3d31-4a34-b503-88747a555a74")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"code":"not_found"`) {
		t.Fatalf("unexpected missing-context response: %d %s", missing.Code, missing.Body.String())
	}
	method := request(handler, http.MethodPost, "/api/v1/notes")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("unexpected method response: %d headers=%v", method.Code, method.Header())
	}
}

func TestListNotesValidatesAndBindsCursorsToContext(t *testing.T) {
	ctxInfo := notes.Context{Type: notes.ContextBroadArea, Label: "Town"}
	key, err := notes.ContextKey(ctxInfo)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := notes.EncodeCursor(notes.Cursor{
		ContextKey: key, Phase: notes.PhaseContext,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		NoteID:    "00000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("matching query", func(t *testing.T) {
		called := false
		handler := notesHandler(notesReaderFunc(func(_ context.Context, query notes.Query) (notes.Page, error) {
			called = query.Cursor != nil && query.Cursor.NoteID == "00000000-0000-0000-0000-000000000001"
			return emptyNotesPage(), nil
		}))
		requestURL := "/api/v1/notes?context_type=broad_area&label=Town&cursor=" + url.QueryEscape(cursor)
		response := request(handler, http.MethodGet, requestURL)
		if response.Code != http.StatusOK || !called {
			t.Fatalf("cursor was not accepted: status=%d body=%s called=%v", response.Code, response.Body.String(), called)
		}
	})
	t.Run("different context", func(t *testing.T) {
		handler := notesHandler(notesReaderFunc(func(context.Context, notes.Query) (notes.Page, error) {
			t.Fatal("reader must not receive a cursor from another context")
			return notes.Page{}, nil
		}))
		requestURL := "/api/v1/notes?context_type=broad_area&label=Other&cursor=" + url.QueryEscape(cursor)
		response := request(handler, http.MethodGet, requestURL)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"cursor"`) {
			t.Fatalf("unexpected cross-context cursor response: %d %s", response.Code, response.Body.String())
		}
	})
}

func TestListNotesDoesNotExposeStorageErrors(t *testing.T) {
	handler := notesHandler(notesReaderFunc(func(context.Context, notes.Query) (notes.Page, error) {
		return notes.Page{}, context.Canceled
	}))
	response := request(handler, http.MethodGet, "/api/v1/notes?context_type=broad_area&label=Town")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "context canceled") {
		t.Fatalf("unexpected database error response: %d %s", response.Code, response.Body.String())
	}
}

func notesHandler(reader notes.Reader) http.Handler {
	return NewHandler(pingerFunc(func(context.Context) error { return nil }), reader, slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Second)
}

func emptyNotesPage() notes.Page {
	return notes.Page{Exact: []notes.Note{}, ContextMatches: []notes.Note{}, Nearby: []notes.Note{}}
}
