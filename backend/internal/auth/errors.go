package auth

import (
	"errors"
)

// ReasonCode is a fixed, non-sensitive diagnostic category. It must never
// contain values derived from a request, token, claim, or database error.
type ReasonCode string

const (
	ReasonMissingBearerToken           ReasonCode = "auth_missing_bearer_token"
	ReasonMalformedBearerHeader        ReasonCode = "auth_malformed_bearer_header"
	ReasonInvalidJWTStructure          ReasonCode = "auth_invalid_jwt_structure"
	ReasonUnsupportedAlgorithm         ReasonCode = "auth_unsupported_algorithm"
	ReasonMissingKeyID                 ReasonCode = "auth_missing_key_id"
	ReasonInvalidSignature             ReasonCode = "auth_invalid_signature"
	ReasonInvalidIssuer                ReasonCode = "auth_invalid_issuer"
	ReasonInvalidAudience              ReasonCode = "auth_invalid_audience"
	ReasonExpiredToken                 ReasonCode = "auth_expired_token"
	ReasonInvalidIssuedAt              ReasonCode = "auth_invalid_issued_at"
	ReasonInvalidNotBefore             ReasonCode = "auth_invalid_not_before"
	ReasonInvalidSubject               ReasonCode = "auth_invalid_subject"
	ReasonInvalidRole                  ReasonCode = "auth_invalid_role"
	ReasonInvalidAnonymousStatus       ReasonCode = "auth_invalid_anonymous_status"
	ReasonUnknownSigningKey            ReasonCode = "auth_unknown_signing_key"
	ReasonJWKSUnavailable              ReasonCode = "auth_jwks_unavailable"
	ReasonIdentityResolverUnavailable  ReasonCode = "auth_identity_resolver_unavailable"
	ReasonAuthenticatorUnavailable     ReasonCode = "auth_authenticator_unavailable"
	ReasonAccountUnavailable           ReasonCode = "auth_account_unavailable"
	ReasonUnknownAuthenticationFailure ReasonCode = "auth_unknown_failure"
)

var (
	ErrInvalidToken       = errors.New("invalid access token")
	ErrUnavailable        = errors.New("authentication unavailable")
	ErrAccountUnavailable = errors.New("account unavailable")
)

// diagnosticFailure carries a fixed reason while preserving errors.Is checks
// against the existing public authentication error categories. Its Error text
// is deliberately generic and never includes its cause.
type diagnosticFailure struct {
	reason ReasonCode
	cause  error
}

func (failure *diagnosticFailure) Error() string { return "authentication failed" }

func (failure *diagnosticFailure) Unwrap() error { return failure.cause }

func withReason(reason ReasonCode, cause error) error {
	return &diagnosticFailure{reason: reason, cause: cause}
}

// ReasonCodeOf returns a fixed diagnostic reason, or the fixed generic reason
// when the error did not originate from a classified authentication stage.
func ReasonCodeOf(err error) ReasonCode {
	if reason, ok := diagnosticReason(err); ok {
		return reason
	}
	return ReasonUnknownAuthenticationFailure
}

func diagnosticReason(err error) (ReasonCode, bool) {
	var failure *diagnosticFailure
	if errors.As(err, &failure) {
		return failure.reason, true
	}
	return "", false
}
