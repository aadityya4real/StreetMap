package httpapi

import (
	"errors"
	"net/http"

	"streetmap.in/backend/internal/auth"
	"streetmap.in/backend/internal/notes"
)

type voteResponse struct {
	Voted bool  `json:"voted"`
	Count int64 `json:"helpful_vote_count"`
}

func (h Handler) noteVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported.", h.logger)
		return
	}
	h.Authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ := AuthenticatedPrincipal(r.Context())
		if h.voter == nil {
			writeError(w, http.StatusServiceUnavailable, "vote_unavailable", "Voting is temporarily unavailable.", h.logger)
			return
		}
		var result notes.VoteResult
		var err error
		if r.Method == http.MethodGet {
			result, err = h.voter.HasVoted(r.Context(), principal.UserID, r.PathValue("noteID"))
		} else {
			result, err = h.voter.ToggleVote(r.Context(), principal.UserID, r.PathValue("noteID"))
		}
		if errors.Is(err, notes.ErrVoteTargetUnavailable) {
			writeError(w, http.StatusNotFound, "note_not_found", "This note is not available for voting.", h.logger)
			return
		}
		if errors.Is(err, auth.ErrAccountUnavailable) {
			writeError(w, http.StatusForbidden, "account_unavailable", "This StreetMap account is not available.", h.logger)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "The vote could not be completed.", h.logger)
			return
		}
		writeSuccess(w, http.StatusOK, voteResponse{Voted: result.Voted, Count: result.Count}, h.logger)
	})).ServeHTTP(w, r)
}
