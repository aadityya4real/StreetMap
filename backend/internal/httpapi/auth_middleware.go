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
			logAuthenticationFailure(logger, auth.ReasonAuthenticatorUnavailable)
			writeError(w, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is temporarily unavailable.", logger)
			return
		}
		token, reason := bearerToken(r)
		if reason != "" {
			logAuthenticationFailure(logger, reason)
			w.Header().Set("WWW-Authenticate", `Bearer realm="streetmap"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "A valid bearer access token is required.", logger)
			return
		}
		principal, err := authenticator.Authenticate(r.Context(), token)
		if err != nil {
			reason := auth.ReasonCodeOf(err)
			if reason == auth.ReasonUnknownAuthenticationFailure {
				switch {
				case errors.Is(err, auth.ErrAccountUnavailable):
					reason = auth.ReasonAccountUnavailable
				case errors.Is(err, auth.ErrUnavailable):
					reason = auth.ReasonAuthenticatorUnavailable
				}
			}
			logAuthenticationFailure(logger, reason)
			switch {
			case errors.Is(err, auth.ErrInvalidToken):
				w.Header().Set("WWW-Authenticate", `Bearer realm="streetmap", error="invalid_token"`)
				writeError(w, http.StatusUnauthorized, "unauthorized", "The bearer access token is invalid or expired.", logger)
			case errors.Is(err, auth.ErrAccountUnavailable):
				writeError(w, http.StatusForbidden, "account_unavailable", "This StreetMap account is not available.", logger)
			default:
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

func bearerToken(r *http.Request) (string, auth.ReasonCode) {
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		return "", auth.ReasonMissingBearerToken
	}
	if len(values) != 1 {
		return "", auth.ReasonMalformedBearerHeader
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", auth.ReasonMalformedBearerHeader
	}
	return parts[1], ""
}

func logAuthenticationFailure(logger *slog.Logger, reason auth.ReasonCode) {
	if reason == "" {
		reason = auth.ReasonUnknownAuthenticationFailure
	}
	logger.Error("authentication rejected", "reason_code", string(reason))
}
