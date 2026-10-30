package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"streetmap.in/backend/internal/auth"
)

const fallbackDisplayName = "StreetMap member"

type PostgresResolver struct {
	database *sql.DB
}

func NewPostgresResolver(database *sql.DB) *PostgresResolver {
	return &PostgresResolver{database: database}
}

func (r *PostgresResolver) Resolve(ctx context.Context, external auth.Identity) (auth.Principal, error) {
	if r == nil || r.database == nil || external.Issuer == "" || external.Subject == "" {
		return auth.Principal{}, auth.ErrUnavailable
	}
	external.DisplayName = safeDisplayName(external.DisplayName)
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return auth.Principal{}, fmt.Errorf("begin identity resolution: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	principal, status, err := lookupIdentity(ctx, tx, external.Issuer, external.Subject)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return auth.Principal{}, fmt.Errorf("lookup external identity: %w", err)
	}
	if err == nil {
		if err := tx.Commit(); err != nil {
			return auth.Principal{}, fmt.Errorf("commit identity lookup: %w", err)
		}
		return activePrincipal(principal, status)
	}

	var createdUserID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name)
		VALUES (NULL, $1)
		RETURNING id::text`, external.DisplayName).Scan(&createdUserID); err != nil {
		return auth.Principal{}, fmt.Errorf("create local user: %w", err)
	}

	var mappedUserID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO auth_identities (user_id, issuer, subject)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (issuer, subject) DO NOTHING
		RETURNING user_id::text`, createdUserID, external.Issuer, external.Subject).Scan(&mappedUserID)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return auth.Principal{}, fmt.Errorf("commit new identity: %w", err)
		}
		return auth.Principal{UserID: mappedUserID}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return auth.Principal{}, fmt.Errorf("create identity mapping: %w", err)
	}

	// A concurrent first login inserted this (issuer, subject) first. Remove
	// this transaction's temporary user and resolve the committed winner.
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1::uuid`, createdUserID); err != nil {
		return auth.Principal{}, fmt.Errorf("remove losing first-login user: %w", err)
	}
	principal, status, err = lookupIdentity(ctx, tx, external.Issuer, external.Subject)
	if err != nil {
		return auth.Principal{}, fmt.Errorf("resolve concurrent identity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return auth.Principal{}, fmt.Errorf("commit concurrent identity resolution: %w", err)
	}
	return activePrincipal(principal, status)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func lookupIdentity(ctx context.Context, query queryRower, issuer, subject string) (auth.Principal, string, error) {
	var principal auth.Principal
	var status string
	err := query.QueryRowContext(ctx, `
		SELECT u.id::text, u.account_status
		FROM auth_identities AS i
		JOIN users AS u ON u.id = i.user_id
		WHERE i.issuer = $1 AND i.subject = $2`, issuer, subject).Scan(&principal.UserID, &status)
	return principal, status, err
}

func activePrincipal(principal auth.Principal, status string) (auth.Principal, error) {
	if status != "active" {
		return auth.Principal{}, auth.ErrAccountUnavailable
	}
	return principal, nil
}

func safeDisplayName(displayName string) string {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) > 80 {
		return fallbackDisplayName
	}
	for _, char := range displayName {
		if unicode.IsControl(char) {
			return fallbackDisplayName
		}
	}
	return displayName
}
