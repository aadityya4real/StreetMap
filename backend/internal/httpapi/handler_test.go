package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingerFunc func(context.Context) error

func (f pingerFunc) PingContext(ctx context.Context) error {
	return f(ctx)
}

func TestHealthReturnsJSONWithoutDatabaseDependency(t *testing.T) {
	handler := NewHandler(pingerFunc(func(context.Context) error {
		t.Fatal("health endpoint should not ping the database")
		return nil
	}), nil, testLogger(), time.Second)
	response := request(handler, http.MethodGet, "/api/v1/health")

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected health response: status=%d headers=%v", response.Code, response.Header())
	}
	var body map[string]map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["data"]["status"] != "ok" {
		t.Fatalf("unexpected health body: %s", response.Body.String())
	}
}

func TestReadyReturnsJSONWhenDatabaseIsAvailable(t *testing.T) {
	handler := NewHandler(pingerFunc(func(context.Context) error { return nil }), nil, testLogger(), time.Second)
	response := request(handler, http.MethodGet, "/api/v1/ready")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Status       string            `json:"status"`
			Dependencies map[string]string `json:"dependencies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "ready" || body.Data.Dependencies["database"] != "available" {
		t.Fatalf("unexpected ready body: %s", response.Body.String())
	}
}

func TestReadyReturnsSafeUnavailableResponseWhenDatabaseIsDown(t *testing.T) {
	handler := NewHandler(pingerFunc(func(context.Context) error {
		return errors.New("password=must-not-appear")
	}), nil, testLogger(), time.Second)
	response := request(handler, http.MethodGet, "/api/v1/ready")

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if response.Body.String() == "" || strings.Contains(response.Body.String(), "must-not-appear") {
		t.Fatalf("response was empty or exposed internal error: %s", response.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != "dependency_unavailable" {
		t.Fatalf("unexpected error response: %s", response.Body.String())
	}
}

func TestUnknownRouteAndMethodUseJSONErrors(t *testing.T) {
	handler := NewHandler(pingerFunc(func(context.Context) error { return nil }), nil, testLogger(), time.Second)
	for _, tt := range []struct {
		method string
		path   string
		status int
		code   string
	}{
		{http.MethodPost, "/api/v1/health", http.StatusMethodNotAllowed, "method_not_allowed"},
		{http.MethodGet, "/api/v1/unknown", http.StatusNotFound, "not_found"},
	} {
		response := request(handler, tt.method, tt.path)
		if response.Code != tt.status || !strings.Contains(response.Body.String(), `"code":"`+tt.code+`"`) {
			t.Errorf("%s %s got status=%d body=%s", tt.method, tt.path, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%s %s returned non-JSON content type", tt.method, tt.path)
		}
	}
}

func TestReadinessUsesBoundedRequestContext(t *testing.T) {
	var deadlinePresent bool
	handler := NewHandler(pingerFunc(func(ctx context.Context) error {
		_, deadlinePresent = ctx.Deadline()
		return nil
	}), nil, testLogger(), 25*time.Millisecond)
	response := request(handler, http.MethodGet, "/api/v1/ready")
	if !deadlinePresent || response.Code != http.StatusOK {
		t.Fatalf("readiness did not ping with bounded context: status=%d", response.Code)
	}
}

func request(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
