package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/notes"
)

type stubAuthenticator struct {
	principal auth.Principal
	err       error
	token     string
}

func (s *stubAuthenticator) Authenticate(_ context.Context, token string) (auth.Principal, error) {
	s.token = token
	return s.principal, s.err
}

func TestRequireAuthenticationRejectsMissingAndMalformedAuthorization(t *testing.T) {
	authenticator := &stubAuthenticator{principal: auth.Principal{UserID: "internal-user"}}
	handler := RequireAuthentication(authenticator, slog.New(slog.NewTextHandler(io.Discard, nil)), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, header := range []string{"", "Basic abc", "Bearer", "Bearer ", "Bearer one two", "Bearer one"} {
		t.Run(strings.ReplaceAll(header, " ", "_"), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if header != "" {
				request.Header.Add("Authorization", header)
			}
			if header == "Bearer one" {
				request.Header.Add("Authorization", "Bearer two")
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestRequireAuthenticationProvidesPrincipalAndOnlyPassesBearerToken(t *testing.T) {
	authenticator := &stubAuthenticator{principal: auth.Principal{UserID: "00000000-0000-4000-8000-000000000001"}}
	handler := RequireAuthentication(authenticator, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := AuthenticatedPrincipal(r.Context())
		if !ok || principal.UserID != authenticator.principal.UserID {
			t.Error("authenticated internal principal missing from request context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "bEaReR opaque-access-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || authenticator.token != "opaque-access-token" {
		t.Fatalf("status=%d token=%q", recorder.Code, authenticator.token)
	}
}

func TestRequireAuthenticationHidesInternalErrorsAndFailsClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	protected := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	tests := []struct {
		name          string
		authenticator Authenticator
		err           error
		wantStatus    int
		wantCode      string
	}{
		{"authentication unavailable", nil, nil, http.StatusServiceUnavailable, "authentication_unavailable"},
		{"invalid token", &stubAuthenticator{err: auth.ErrInvalidToken}, auth.ErrInvalidToken, http.StatusUnauthorized, "unauthorized"},
		{"suspended account", &stubAuthenticator{err: auth.ErrAccountUnavailable}, auth.ErrAccountUnavailable, http.StatusForbidden, "account_unavailable"},
		{"database failure", &stubAuthenticator{err: errors.New("sensitive database detail")}, errors.New("sensitive database detail"), http.StatusServiceUnavailable, "authentication_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if stub, ok := test.authenticator.(*stubAuthenticator); ok {
				stub.err = test.err
			}
			handler := RequireAuthentication(test.authenticator, logger, protected)
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			request.Header.Set("Authorization", "Bearer token-value")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), test.wantCode) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "sensitive database detail") || strings.Contains(recorder.Body.String(), "token-value") {
				t.Fatal("response exposed internal details or bearer token")
			}
		})
	}
}

func TestPublicRoutesRemainAnonymousAndAuthenticatedWrapperFailsClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reader := notesReaderFunc(func(context.Context, notes.Query) (notes.Page, error) { return emptyNotesPage(), nil })
	server := NewHandler(pingerFunc(func(context.Context) error { return nil }), reader, logger, time.Second)
	for _, path := range []string{"/api/v1/health", "/api/v1/ready", "/api/v1/notes?context_type=map_center&latitude=0&longitude=0"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("anonymous public route %s status=%d body=%s", path, recorder.Code, recorder.Body.String())
			}
		})
	}
	wrapped := Handler{logger: logger}.Authenticated(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/future-write", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured protected handler status=%d, want 503", recorder.Code)
	}
}
