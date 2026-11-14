package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/notes"
)

func TestDevelopmentCORSAllowsOnlyExpectedExtensionPreflight(t *testing.T) {
	handler := corsTestHandler("test", nil, nil)
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/notes", nil)
	request.Header.Set("Origin", developmentExtensionOrigin)
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "authorization, content-type, idempotency-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != developmentExtensionOrigin {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
		t.Fatalf("allow-methods = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type, Idempotency-Key" {
		t.Fatalf("allow-headers = %q", got)
	}
	if !varyContains(response.Header(), "Origin") || !varyContains(response.Header(), "Access-Control-Request-Method") || !varyContains(response.Header(), "Access-Control-Request-Headers") {
		t.Fatalf("preflight Vary header is incomplete: %v", response.Header().Values("Vary"))
	}
	if response.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentialed CORS must remain disabled")
	}
}

func TestDevelopmentCORSRejectsUnrelatedOriginAndUnsupportedPreflights(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		method      string
		headers     string
		wantStatus  int
		wantAllowed bool
	}{
		{"unrelated origin", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "POST", "authorization,content-type,idempotency-key", http.StatusMethodNotAllowed, false},
		{"multiple origins", developmentExtensionOrigin, "POST", "authorization", http.StatusMethodNotAllowed, false},
		{"unsupported method", developmentExtensionOrigin, "DELETE", "authorization", http.StatusForbidden, false},
		{"unsupported header", developmentExtensionOrigin, "POST", "authorization,x-unapproved", http.StatusForbidden, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := corsTestHandler("development", nil, nil)
			request := httptest.NewRequest(http.MethodOptions, "/api/v1/notes", nil)
			request.Header.Set("Origin", test.origin)
			if test.name == "multiple origins" {
				request.Header.Add("Origin", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			}
			request.Header.Set("Access-Control-Request-Method", test.method)
			request.Header.Set("Access-Control-Request-Headers", test.headers)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); (got != "") != test.wantAllowed {
				t.Fatalf("allow-origin = %q, wantAllowed=%v", got, test.wantAllowed)
			}
		})
	}
}

func TestDevelopmentCORSDoesNotBypassAuthenticationOrChangePublicGET(t *testing.T) {
	reader := notesReaderFunc(func(context.Context, notes.Query) (notes.Page, error) {
		return emptyNotesPage(), nil
	})
	creator := noteCreatorFunc(func(context.Context, string, string, notes.CreateCommand) (notes.CreateResult, error) {
		t.Fatal("note creator called by an authentication regression test")
		return notes.CreateResult{}, errors.New("unreachable")
	})
	invalidJWT := &stubAuthenticator{err: auth.ErrInvalidToken}
	handler := corsTestHandler("test", reader, creator, invalidJWT)

	t.Run("missing bearer token remains unauthorized", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/notes", strings.NewReader(`{}`))
		request.Header.Set("Origin", developmentExtensionOrigin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
		if response.Header().Get("Access-Control-Allow-Origin") != developmentExtensionOrigin {
			t.Fatal("allowed origin cannot read the expected unauthorized response")
		}
	})

	t.Run("invalid JWT remains unauthorized", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/notes", strings.NewReader(`{}`))
		request.Header.Set("Origin", developmentExtensionOrigin)
		request.Header.Set("Authorization", "Bearer synthetic-invalid-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"unauthorized"`) {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
		if response.Header().Get("Access-Control-Allow-Origin") != developmentExtensionOrigin {
			t.Fatal("CORS response exposed by the development origin must retain normal JWT rejection")
		}
	})

	t.Run("public GET remains anonymous", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/notes?context_type=map_center&latitude=0&longitude=0", nil)
		request.Header.Set("Origin", developmentExtensionOrigin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
		}
		if response.Header().Get("Access-Control-Allow-Origin") != developmentExtensionOrigin {
			t.Fatal("public notes response is missing the development extension origin")
		}
	})
}

func TestDevelopmentCORSIsNotEnabledForProduction(t *testing.T) {
	handler := corsTestHandler(" production ", nil, nil)
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/notes", nil)
	request.Header.Set("Origin", developmentExtensionOrigin)
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type,idempotency-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("production OPTIONS status = %d, want 405", response.Code)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("production unexpectedly allowed the development extension origin")
	}
}

func corsTestHandler(environment string, reader notes.Reader, creator notes.Creator, authenticators ...Authenticator) http.Handler {
	var authenticator Authenticator
	if len(authenticators) > 0 {
		authenticator = authenticators[0]
	}
	return NewHandlerForEnvironment(pingerFunc(func(context.Context) error { return nil }), reader, testLogger(), time.Second, authenticator, creator, environment)
}

func varyContains(header http.Header, expected string) bool {
	for _, value := range header.Values("Vary") {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), expected) {
				return true
			}
		}
	}
	return false
}
