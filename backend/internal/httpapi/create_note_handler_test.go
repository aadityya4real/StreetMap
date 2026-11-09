package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/notes"
)

type noteCreatorFunc func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error)

func (creator noteCreatorFunc) CreateNote(ctx context.Context, userID, idempotencyKey string, command notes.CreateCommand) (notes.CreateResult, error) {
	return creator(ctx, userID, idempotencyKey, command)
}

func TestCreateNoteUsesAuthenticatedPrincipalAndReturnsExistingNoteShape(t *testing.T) {
	const userID = "00000000-0000-4000-8000-000000000001"
	const key = "00000000-0000-4000-8000-000000000002"
	var gotUserID, gotKey string
	var gotCommand notes.CreateCommand
	creator := noteCreatorFunc(func(_ context.Context, id, idempotency string, command notes.CreateCommand) (notes.CreateResult, error) {
		gotUserID, gotKey, gotCommand = id, idempotency, command
		return notes.CreateResult{Note: notes.Note{
			ID: "00000000-0000-4000-8000-000000000003", Title: command.Title, Body: command.Body,
			Category: command.Category, AuthorDisplayName: "Authenticated member", CreatedAt: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC),
			LocationLabel: "A verified place", MatchType: notes.MatchExactLocation,
		}}, nil
	})
	handler := NewHandler(pingerFunc(func(context.Context) error { return nil }), nil, testLogger(), time.Second, &stubAuthenticator{principal: auth.Principal{UserID: userID}}, creator)
	body := `{"location":{"type":"verified","location_id":"00000000-0000-4000-8000-000000000004"},"title":"A useful tip","body":"Use the side entrance.","category":"local_tip"}`
	response := createRequest(handler, key, body, "Bearer valid-token")
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if gotUserID != userID || gotKey != key || gotCommand.Location.LocationID != "00000000-0000-4000-8000-000000000004" {
		t.Fatalf("creator received untrusted or incorrect data: user=%q key=%q command=%+v", gotUserID, gotKey, gotCommand)
	}
	if !strings.Contains(response.Body.String(), `"note"`) || !strings.Contains(response.Body.String(), `"match":{"type":"exact_location"`) || strings.Contains(response.Body.String(), "user_id") {
		t.Fatalf("unexpected create response contract: %s", response.Body.String())
	}

	replay := noteCreatorFunc(func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error) {
		return notes.CreateResult{Note: notes.Note{ID: "00000000-0000-4000-8000-000000000003", Title: "A useful tip", Body: "Use the side entrance.", Category: "local_tip", AuthorDisplayName: "Authenticated member", CreatedAt: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), LocationLabel: "A verified place", MatchType: notes.MatchExactLocation}, Replayed: true}, nil
	})
	server := NewHandler(pingerFunc(func(context.Context) error { return nil }), nil, testLogger(), time.Second, &stubAuthenticator{principal: auth.Principal{UserID: userID}}, replay)
	response = createRequest(server, key, body, "Bearer valid-token")
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent replay status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateNoteSupportsAllLocationContracts(t *testing.T) {
	locations := []string{
		`"location":{"type":"verified","location_id":"00000000-0000-4000-8000-000000000001"}`,
		`"location":{"type":"unverified","label":"URL-derived venue"}`,
		`"location":{"type":"broad","label":"Neighbourhood"}`,
		`"location":{"type":"coordinates","latitude":28.5,"longitude":77.2,"user_confirmed":true}`,
	}
	for index, location := range locations {
		t.Run(fmt.Sprintf("location_%d", index), func(t *testing.T) {
			called := false
			creator := noteCreatorFunc(func(_ context.Context, _ string, _ string, command notes.CreateCommand) (notes.CreateResult, error) {
				called = true
				if command.Location.Type == "" {
					t.Fatal("location type was lost")
				}
				return notes.CreateResult{Note: notes.Note{ID: "00000000-0000-4000-8000-000000000010", Title: command.Title, Body: command.Body, Category: command.Category}}, nil
			})
			handler := newCreateTestHandler(creator, &stubAuthenticator{principal: auth.Principal{UserID: "00000000-0000-4000-8000-000000000011"}})
			body := `{` + location + `,"title":"A useful tip","body":"Description","category":"safety"}`
			response := createRequest(handler, fmt.Sprintf("00000000-0000-4000-8000-%012d", index+20), body, "Bearer valid-token")
			if response.Code != http.StatusCreated || !called {
				t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
			}
		})
	}
}

func TestCreateNoteRejectsUnauthenticatedInvalidAndSuspendedUsers(t *testing.T) {
	body := `{"location":{"type":"broad","label":"Town"},"title":"A useful tip","body":"Description","category":"local_tip"}`
	tests := []struct {
		name          string
		authenticator Authenticator
		token         string
		status        int
		code          string
	}{
		{"authentication not configured", nil, "Bearer valid-token", http.StatusServiceUnavailable, "authentication_unavailable"},
		{"missing token", &stubAuthenticator{principal: auth.Principal{UserID: "00000000-0000-4000-8000-000000000001"}}, "", http.StatusUnauthorized, "unauthorized"},
		{"invalid token", &stubAuthenticator{err: auth.ErrInvalidToken}, "Bearer invalid", http.StatusUnauthorized, "unauthorized"},
		{"suspended user", &stubAuthenticator{err: auth.ErrAccountUnavailable}, "Bearer valid-token", http.StatusForbidden, "account_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			creatorCalled := false
			creator := noteCreatorFunc(func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error) {
				creatorCalled = true
				return notes.CreateResult{}, nil
			})
			server := newCreateTestHandler(creator, test.authenticator)
			response := createRequest(server, "00000000-0000-4000-8000-000000000002", body, test.token)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || creatorCalled {
				t.Fatalf("status=%d body=%s creatorCalled=%v", response.Code, response.Body.String(), creatorCalled)
			}
		})
	}
}

func TestCreateNoteRejectsMalformedAndClientControlledFields(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"malformed JSON", `{"location":`, http.StatusBadRequest},
		{"multiple JSON values", `{"location":{"type":"broad","label":"Town"},"title":"A useful tip","body":"Description","category":"local_tip"} {}`, http.StatusBadRequest},
		{"forged author", `{"location":{"type":"broad","label":"Town"},"title":"A useful tip","body":"Description","category":"local_tip","author_user_id":"00000000-0000-4000-8000-000000000001"}`, http.StatusBadRequest},
		{"forged publication state", `{"location":{"type":"broad","label":"Town"},"title":"A useful tip","body":"Description","category":"local_tip","status":"hidden"}`, http.StatusBadRequest},
		{"unknown verification field", `{"location":{"type":"verified","location_id":"00000000-0000-4000-8000-000000000001","verified":true},"title":"A useful tip","body":"Description","category":"local_tip"}`, http.StatusBadRequest},
		{"map center is not a creation kind", `{"location":{"type":"map_center","latitude":28.5,"longitude":77.2,"user_confirmed":true},"title":"A useful tip","body":"Description","category":"local_tip"}`, http.StatusBadRequest},
		{"coordinates require confirmation", `{"location":{"type":"coordinates","latitude":28.5,"longitude":77.2},"title":"A useful tip","body":"Description","category":"local_tip"}`, http.StatusBadRequest},
		{"coordinate bounds", `{"location":{"type":"coordinates","latitude":91,"longitude":77.2,"user_confirmed":true},"title":"A useful tip","body":"Description","category":"local_tip"}`, http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			creator := noteCreatorFunc(func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error) {
				called = true
				return notes.CreateResult{}, nil
			})
			server := newCreateTestHandler(creator, &stubAuthenticator{principal: auth.Principal{UserID: "00000000-0000-4000-8000-000000000001"}})
			response := createRequest(server, "00000000-0000-4000-8000-000000000002", test.body, "Bearer valid-token")
			if response.Code != test.status || called {
				t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
			}
		})
	}
}

func TestCreateNoteMapsIdempotencyRateLimitAndInternalErrors(t *testing.T) {
	body := `{"location":{"type":"broad","label":"Town"},"title":"A useful tip","body":"Description","category":"local_tip"}`
	tests := []struct {
		name        string
		err         error
		status      int
		code        string
		retryAfter  string
		mustNotLeak string
	}{
		{"idempotency conflict", notes.ErrIdempotencyConflict, http.StatusConflict, "idempotency_conflict", "", ""},
		{"rate limited", &notes.RateLimitError{RetryAfter: 3 * time.Second}, http.StatusTooManyRequests, "note_creation_rate_limited", "3", ""},
		{"internal storage error", errors.New("password=private-detail"), http.StatusInternalServerError, "internal_error", "", "private-detail"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			creator := noteCreatorFunc(func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error) {
				return notes.CreateResult{}, test.err
			})
			server := newCreateTestHandler(creator, &stubAuthenticator{principal: auth.Principal{UserID: "00000000-0000-4000-8000-000000000001"}})
			response := createRequest(server, fmt.Sprintf("00000000-0000-4000-8000-%012d", index+30), body, "Bearer valid-token")
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.retryAfter != "" && response.Header().Get("Retry-After") != test.retryAfter {
				t.Fatalf("Retry-After = %q, want %q", response.Header().Get("Retry-After"), test.retryAfter)
			}
			if test.mustNotLeak != "" && strings.Contains(response.Body.String(), test.mustNotLeak) {
				t.Fatalf("response leaked internal error: %s", response.Body.String())
			}
		})
	}
}

func TestCreateNoteRejectsLargeBodyAndMissingIdempotencyKey(t *testing.T) {
	creator := noteCreatorFunc(func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error) {
		return notes.CreateResult{}, nil
	})
	server := newCreateTestHandler(creator, &stubAuthenticator{principal: auth.Principal{UserID: "00000000-0000-4000-8000-000000000001"}})
	large := `{"location":{"type":"broad","label":"Town"},"title":"A useful tip","body":"` + strings.Repeat("x", maxCreateNoteRequestBytes) + `","category":"local_tip"}`
	response := createRequest(server, "00000000-0000-4000-8000-000000000002", large, "Bearer valid-token")
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status=%d body=%s", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/notes", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer valid-token")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Idempotency-Key") {
		t.Fatalf("missing idempotency key status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateNoteRejectsInvalidUTF8AndUnsupportedContentType(t *testing.T) {
	creatorCalled := false
	creator := noteCreatorFunc(func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error) {
		creatorCalled = true
		return notes.CreateResult{}, nil
	})
	server := newCreateTestHandler(creator, &stubAuthenticator{principal: auth.Principal{UserID: "00000000-0000-4000-8000-000000000001"}})
	invalidUTF8 := `{"location":{"type":"broad","label":"Town"},"title":"A ` + string([]byte{0xff}) + `tip","body":"Description","category":"local_tip"}`
	response := createRequest(server, "00000000-0000-4000-8000-000000000002", invalidUTF8, "Bearer valid-token")
	if response.Code != http.StatusBadRequest || creatorCalled {
		t.Fatalf("invalid UTF-8 status=%d called=%v body=%s", response.Code, creatorCalled, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/notes", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Idempotency-Key", "00000000-0000-4000-8000-000000000003")
	request.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType || creatorCalled {
		t.Fatalf("unsupported content type status=%d called=%v body=%s", recorder.Code, creatorCalled, recorder.Body.String())
	}
}

func newCreateTestHandler(creator notes.Creator, authenticator Authenticator) http.Handler {
	return NewHandler(pingerFunc(func(context.Context) error { return nil }), nil, testLogger(), time.Second, authenticator, creator)
}

func createRequest(handler http.Handler, key, body, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notes", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Idempotency-Key", key)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
