package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
)

const fallbackDisplayName = "StreetMap member"

type Config struct {
	ProjectURL   string
	Issuer       string
	Audience     string
	Algorithm    string
	JWKSCacheFor time.Duration
	ClockSkew    time.Duration
}

type Verifier struct {
	issuer    string
	audience  string
	algorithm string
	clockSkew time.Duration
	keys      *jwksCache
}

type supabaseClaims struct {
	jwt.RegisteredClaims
	Role         string         `json:"role"`
	IsAnonymous  *bool          `json:"is_anonymous"`
	UserMetadata map[string]any `json:"user_metadata"`
	AAL          string         `json:"aal"`
	SessionID    string         `json:"session_id"`
}

func NewVerifier(config Config) (*Verifier, error) {
	return newVerifierWithClient(config, nil)
}

func newVerifierWithClient(config Config, client *http.Client) (*Verifier, error) {
	if config.Issuer == "" || config.Audience == "" {
		return nil, fmt.Errorf("Supabase issuer and audience are required")
	}
	if err := validateProjectIssuer(config.ProjectURL, config.Issuer); err != nil {
		return nil, err
	}
	if config.JWKSCacheFor <= 0 || config.ClockSkew < 0 {
		return nil, fmt.Errorf("invalid JWT cache duration or clock skew")
	}
	switch config.Algorithm {
	case "RS256", "ES256", "EdDSA":
	default:
		return nil, fmt.Errorf("unsupported configured JWT algorithm")
	}
	keyCache, err := newJWKSCache(config.ProjectURL, config.Algorithm, config.JWKSCacheFor, client)
	if err != nil {
		return nil, err
	}
	return &Verifier{
		issuer:    config.Issuer,
		audience:  config.Audience,
		algorithm: config.Algorithm,
		clockSkew: config.ClockSkew,
		keys:      keyCache,
	}, nil
}

func validateProjectIssuer(projectURL, issuer string) error {
	if err := ValidateSupabaseProjectURL(projectURL); err != nil {
		return err
	}
	if issuer != strings.TrimRight(projectURL, "/")+"/auth/v1" {
		return fmt.Errorf("Supabase issuer must match the configured project")
	}
	return nil
}

func (v *Verifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	if v == nil || v.keys == nil || strings.TrimSpace(rawToken) == "" || len(rawToken) > 16*1024 {
		return Identity{}, withReason(ReasonInvalidJWTStructure, ErrInvalidToken)
	}
	claims := &supabaseClaims{}
	parsed, err := jwt.ParseWithClaims(
		rawToken,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != v.algorithm {
				return nil, withReason(ReasonUnsupportedAlgorithm, ErrInvalidToken)
			}
			keyID, ok := token.Header["kid"].(string)
			if !ok || keyID == "" || len(keyID) > 256 {
				return nil, withReason(ReasonMissingKeyID, ErrInvalidToken)
			}
			return v.keys.key(ctx, keyID)
		},
		jwt.WithValidMethods([]string{v.algorithm}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(v.clockSkew),
	)
	if err != nil {
		if algorithm, ok := parsedHeaderAlgorithm(parsed); ok && algorithm != v.algorithm {
			return Identity{}, withReason(ReasonUnsupportedAlgorithm, ErrInvalidToken)
		}
		if _, ok := diagnosticReason(err); ok {
			return Identity{}, err
		}
		reason := jwtFailureReason(err)
		if errors.Is(err, ErrUnavailable) {
			return Identity{}, withReason(reason, ErrUnavailable)
		}
		return Identity{}, withReason(reason, ErrInvalidToken)
	}
	if parsed == nil || !parsed.Valid {
		return Identity{}, withReason(ReasonInvalidJWTStructure, ErrInvalidToken)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != v.audience {
		return Identity{}, withReason(ReasonInvalidAudience, ErrInvalidToken)
	}
	if claims.Subject == "" || len(claims.Subject) > 255 || strings.TrimSpace(claims.Subject) != claims.Subject || !validClaimText(claims.Subject) {
		return Identity{}, withReason(ReasonInvalidSubject, ErrInvalidToken)
	}
	// Supabase's access-token role and anonymous flag distinguish an authenticated
	// access token from an ID token or anonymous token. This is token-shape
	// validation only; StreetMap authorization never trusts provider roles.
	if claims.Role != "authenticated" {
		return Identity{}, withReason(ReasonInvalidRole, ErrInvalidToken)
	}
	if claims.IsAnonymous == nil || *claims.IsAnonymous {
		return Identity{}, withReason(ReasonInvalidAnonymousStatus, ErrInvalidToken)
	}
	return Identity{
		Issuer:      claims.Issuer,
		Subject:     claims.Subject,
		DisplayName: validatedDisplayName(claims.UserMetadata),
	}, nil
}

func parsedHeaderAlgorithm(token *jwt.Token) (string, bool) {
	if token == nil || token.Header == nil {
		return "", false
	}
	algorithm, ok := token.Header["alg"].(string)
	return algorithm, ok && algorithm != ""
}

func jwtFailureReason(err error) ReasonCode {
	switch {
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return ReasonInvalidIssuer
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return ReasonInvalidAudience
	case errors.Is(err, jwt.ErrTokenExpired):
		return ReasonExpiredToken
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
		return ReasonInvalidIssuedAt
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return ReasonInvalidNotBefore
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return ReasonInvalidSignature
	case errors.Is(err, jwt.ErrTokenInvalidSubject):
		return ReasonInvalidSubject
	case errors.Is(err, jwt.ErrTokenMalformed), errors.Is(err, jwt.ErrTokenRequiredClaimMissing), errors.Is(err, jwt.ErrTokenInvalidClaims), errors.Is(err, jwt.ErrTokenUnverifiable):
		return ReasonInvalidJWTStructure
	default:
		return ReasonInvalidJWTStructure
	}
}

func validatedDisplayName(metadata map[string]any) string {
	for _, field := range []string{"full_name", "name"} {
		name, ok := metadata[field].(string)
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 {
			continue
		}
		valid := true
		for _, char := range name {
			if unicode.IsControl(char) {
				valid = false
				break
			}
		}
		if valid {
			return name
		}
	}
	return fallbackDisplayName
}

func validClaimText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}
