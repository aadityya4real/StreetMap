package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestDiagnosticFailurePreservesStatusCategoryWithoutExposingCause(t *testing.T) {
	const sensitiveCause = "synthetic-private-error-marker"
	err := withReason(ReasonJWKSUnavailable, errors.Join(ErrUnavailable, errors.New(sensitiveCause)))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("diagnostic wrapper did not preserve the existing unavailable error category")
	}
	if got := ReasonCodeOf(err); got != ReasonJWKSUnavailable {
		t.Fatalf("reason code = %q, want %q", got, ReasonJWKSUnavailable)
	}
	if strings.Contains(err.Error(), sensitiveCause) || err.Error() != "authentication failed" {
		t.Fatalf("diagnostic error exposed its underlying cause: %q", err.Error())
	}
}
