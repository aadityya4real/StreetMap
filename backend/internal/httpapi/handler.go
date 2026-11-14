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
	noteCreator      notes.Creator
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

func NewHandler(database Pinger, noteReader notes.Reader, logger *slog.Logger, readinessTimeout time.Duration, authenticator Authenticator, noteCreator notes.Creator) http.Handler {
	return NewHandlerForEnvironment(database, noteReader, logger, readinessTimeout, authenticator, noteCreator, "production")
}

// NewHandlerForEnvironment enables the development extension CORS policy only
// for explicitly local environments. Production and other deployment modes
// keep the handler's normal same-origin behavior.
func NewHandlerForEnvironment(database Pinger, noteReader notes.Reader, logger *slog.Logger, readinessTimeout time.Duration, authenticator Authenticator, noteCreator notes.Creator, environment string) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	handler := Handler{database: database, noteReader: noteReader, noteCreator: noteCreator, logger: logger, readinessTimeout: readinessTimeout, authenticator: authenticator}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", handler.health)
	mux.HandleFunc("/api/v1/ready", handler.ready)
	mux.HandleFunc("/api/v1/notes", handler.notesCollection)
	mux.HandleFunc("/", handler.notFound)
	var routes http.Handler = mux
	if enablesDevelopmentExtensionCORS(environment) {
		routes = developmentExtensionCORS(routes)
	}
	return requestLogger(logger, routes)
}

// Authenticated wraps a protected handler with the configured bearer-token
// authentication service. Public GET routes deliberately do not use it.
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

func (h Handler) notesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listNotes(w, r)
	case http.MethodPost:
		h.Authenticated(http.HandlerFunc(h.createNote)).ServeHTTP(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported.", h.logger)
	}
}

func (h Handler) notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "The requested endpoint was not found.", h.logger)
}
