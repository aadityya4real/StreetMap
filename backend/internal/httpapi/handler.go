package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/notes"
)

type Pinger interface {
	PingContext(context.Context) error
}

type Authenticator interface {
	Authenticate(context.Context, string) (auth.Principal, error)
}

type Handler struct {
	database         Pinger
	noteReader       notes.Reader
	logger           *slog.Logger
	readinessTimeout time.Duration
	authenticator    Authenticator
}

type healthData struct {
	Status string `json:"status"`
}

type readinessData struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies"`
}

func NewHandler(database Pinger, noteReader notes.Reader, logger *slog.Logger, readinessTimeout time.Duration, authenticators ...Authenticator) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	var authenticator Authenticator
	if len(authenticators) > 0 {
		authenticator = authenticators[0]
	}
	handler := Handler{database: database, noteReader: noteReader, logger: logger, readinessTimeout: readinessTimeout, authenticator: authenticator}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", handler.health)
	mux.HandleFunc("/api/v1/ready", handler.ready)
	mux.HandleFunc("/api/v1/notes", handler.listNotes)
	mux.HandleFunc("/", handler.notFound)
	return requestLogger(logger, mux)
}

// Authenticated wraps a future protected handler with the configured bearer-token
// authentication service. Current public routes deliberately do not use it.
func (h Handler) Authenticated(next http.Handler) http.Handler {
	return RequireAuthentication(h.authenticator, h.logger, next)
}

func (h Handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w)
		return
	}
	writeSuccess(w, http.StatusOK, healthData{Status: "ok"}, h.logger)
}

func (h Handler) ready(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.readinessTimeout)
	defer cancel()
	if err := h.database.PingContext(ctx); err != nil {
		h.logger.Warn("readiness dependency unavailable", "dependency", "database")
		writeError(
			w,
			http.StatusServiceUnavailable,
			"dependency_unavailable",
			"A required service is temporarily unavailable.",
			h.logger,
		)
		return
	}
	writeSuccess(w, http.StatusOK, readinessData{
		Status:       "ready",
		Dependencies: map[string]string{"database": "available"},
	}, h.logger)
}

func (h Handler) methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is supported.", h.logger)
}

func (h Handler) notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "The requested endpoint was not found.", h.logger)
}
