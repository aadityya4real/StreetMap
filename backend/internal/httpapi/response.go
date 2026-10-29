package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type envelope struct {
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeSuccess(w http.ResponseWriter, status int, data any, logger *slog.Logger) {
	writeJSON(w, status, envelope{Data: data}, logger)
}

func writeError(w http.ResponseWriter, status int, code, message string, logger *slog.Logger) {
	writeJSON(w, status, envelope{Error: &apiError{Code: code, Message: message}}, logger)
}

func writeJSON(w http.ResponseWriter, status int, body envelope, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Error("write HTTP response failed", "error", err)
	}
}
