package auth

import "context"

type Identity struct {
	Issuer      string
	Subject     string
	DisplayName string
}

type Principal struct {
	UserID string
}

type TokenVerifier interface {
	Verify(context.Context, string) (Identity, error)
}

type IdentityResolver interface {
	Resolve(context.Context, Identity) (Principal, error)
}

type Service struct {
	verifier TokenVerifier
	resolver IdentityResolver
}

func NewService(verifier TokenVerifier, resolver IdentityResolver) *Service {
	return &Service{verifier: verifier, resolver: resolver}
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (Principal, error) {
	if s == nil || s.verifier == nil || s.resolver == nil {
		return Principal{}, ErrUnavailable
	}
	identity, err := s.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Principal{}, err
	}
	principal, err := s.resolver.Resolve(ctx, identity)
	if err != nil {
		return Principal{}, err
	}
	return principal, nil
}
