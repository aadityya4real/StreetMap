package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"streetmap.in/backend/internal/auth"
)

type principalContextKey struct{}

func RequireAuthentication(authenticator Authenticator, logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authenticator == nil {
			writeError(w, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is temporarily unavailable.", logger)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="streetmap"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "A valid bearer access token is required.", logger)
			return
		}
		principal, err := authenticator.Authenticate(r.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidToken):
				w.Header().Set("WWW-Authenticate", `Bearer realm="streetmap", error="invalid_token"`)
				writeError(w, http.StatusUnauthorized, "unauthorized", "The bearer access token is invalid or expired.", logger)
			case errors.Is(err, auth.ErrAccountUnavailable):
				writeError(w, http.StatusForbidden, "account_unavailable", "This StreetMap account is not available.", logger)
			default:
				logger.Error("authentication dependency unavailable")
				writeError(w, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is temporarily unavailable.", logger)
			}
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AuthenticatedPrincipal(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(auth.Principal)
	return principal, ok
}

func bearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
