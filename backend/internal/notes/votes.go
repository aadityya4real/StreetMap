package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrVoteTargetUnavailable = errors.New("vote target unavailable")

type VoteResult struct {
	Voted bool
	Count int64
}

type Voter interface {
	ToggleVote(context.Context, string, string) (VoteResult, error)
	HasVoted(context.Context, string, string) (VoteResult, error)
}

type PostgresVoter struct{ db *sql.DB }

func NewPostgresVoter(db *sql.DB) *PostgresVoter { return &PostgresVoter{db: db} }

func (v *PostgresVoter) HasVoted(ctx context.Context, userID, noteID string) (VoteResult, error) {
	var result VoteResult
	err := v.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM votes WHERE note_id=$1::uuid AND user_id=$2::uuid), (SELECT count(*) FROM votes WHERE note_id=$1::uuid AND vote_type='helpful')`, noteID, userID).Scan(&result.Voted, &result.Count)
	if err != nil {
		return VoteResult{}, fmt.Errorf("read vote state: %w", err)
	}
	return result, nil
}

func (v *PostgresVoter) ToggleVote(ctx context.Context, userID, noteID string) (VoteResult, error) {
	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return VoteResult{}, fmt.Errorf("begin vote transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1::uuid AND status='published')`, noteID).Scan(&exists); err != nil {
		return VoteResult{}, fmt.Errorf("verify vote note: %w", err)
	}
	if !exists {
		return VoteResult{}, ErrVoteTargetUnavailable
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT account_status='active' FROM users WHERE id=$1::uuid FOR UPDATE`, userID).Scan(&active); err != nil || !active {
		return VoteResult{}, ErrAccountUnavailable
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM votes WHERE note_id=$1::uuid AND user_id=$2::uuid)`, noteID, userID).Scan(&exists); err != nil {
		return VoteResult{}, fmt.Errorf("read vote: %w", err)
	}
	if exists {
		_, err = tx.ExecContext(ctx, `DELETE FROM votes WHERE note_id=$1::uuid AND user_id=$2::uuid`, noteID, userID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO votes (note_id,user_id) VALUES ($1::uuid,$2::uuid) ON CONFLICT (note_id,user_id) DO NOTHING`, noteID, userID)
	}
	if err != nil {
		return VoteResult{}, fmt.Errorf("toggle note vote: %w", err)
	}
	var result VoteResult
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM votes WHERE note_id=$1::uuid AND user_id=$2::uuid), (SELECT count(*) FROM votes WHERE note_id=$1::uuid AND vote_type='helpful')`, noteID, userID).Scan(&result.Voted, &result.Count); err != nil {
		return VoteResult{}, fmt.Errorf("count note votes: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return VoteResult{}, fmt.Errorf("commit vote transaction: %w", err)
	}
	return result, nil
}
