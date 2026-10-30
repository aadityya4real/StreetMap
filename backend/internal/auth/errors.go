package auth

import "errors"

var (
	ErrInvalidToken       = errors.New("invalid access token")
	ErrUnavailable        = errors.New("authentication unavailable")
	ErrAccountUnavailable = errors.New("account unavailable")
)
