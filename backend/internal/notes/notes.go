package notes

import (
	"context"
	"time"
)

const (
	ContextVerifiedPlace   = "verified_place"
	ContextBroadArea       = "broad_area"
	ContextUnverifiedPlace = "place_page_unverified"
	ContextMapCenter       = "map_center"
	ContextCoordinate      = "coordinate"

	MatchExactLocation       = "exact_location"
	MatchExactCoordinate     = "exact_coordinate"
	MatchBroadArea           = "broad_area"
	MatchUnverifiedReference = "unverified_reference_label"
	MatchNearby              = "nearby"
)

type Context struct {
	Type       string
	LocationID string
	Label      string
	Latitude   float64
	Longitude  float64
	RadiusM    int
}

type CursorPhase string

const (
	PhaseExact   CursorPhase = "exact"
	PhaseContext CursorPhase = "context"
	PhaseNearby  CursorPhase = "nearby"
)

// Cursor stores the keyset position for one deterministically ordered result group.
// ContextKey binds it to the request context; it is validation, not an authorization token.
type Cursor struct {
	ContextKey  string      `json:"context_key"`
	Phase       CursorPhase `json:"phase"`
	CreatedAt   time.Time   `json:"created_at,omitempty"`
	DistanceM   float64     `json:"distance_m,omitempty"`
	NoteID      string      `json:"note_id,omitempty"`
	NearbyCount int         `json:"nearby_count,omitempty"`
}

type Query struct {
	Context Context
	Limit   int
	Cursor  *Cursor
}

type Note struct {
	ID                string
	Title             string
	Body              string
	Category          string
	AuthorDisplayName string
	CreatedAt         time.Time
	HelpfulVoteCount  int64
	LocationLabel     string
	DistanceM         *float64
	MatchType         string
}

type Page struct {
	ContextLabel   string
	Exact          []Note
	ContextMatches []Note
	Nearby         []Note
	NextCursor     *Cursor
	HasMore        bool
}

type Reader interface {
	ListPublicNotes(context.Context, Query) (Page, error)
}
