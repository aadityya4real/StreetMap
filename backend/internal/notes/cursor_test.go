package notes

import (
	"strings"
	"testing"
	"time"
)

func TestCursorIsBoundToContextAndRejectsInvalidValues(t *testing.T) {
	context := Context{Type: ContextBroadArea, Label: "Greater Noida", RadiusM: 0}
	contextKey, err := ContextKey(context)
	if err != nil {
		t.Fatal(err)
	}
	cursor := Cursor{
		ContextKey: contextKey, Phase: PhaseContext,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		NoteID:    "00000000-0000-0000-0000-000000000001",
	}
	encoded, err := EncodeCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCursor(encoded, contextKey)
	if err != nil || decoded.NoteID != cursor.NoteID || decoded.Phase != cursor.Phase {
		t.Fatalf("decode cursor = %+v, err=%v", decoded, err)
	}
	otherKey, _ := ContextKey(Context{Type: ContextBroadArea, Label: "Another area"})
	if _, err := DecodeCursor(encoded, otherKey); err == nil {
		t.Fatal("cursor for one context was accepted for another")
	}
	if _, err := DecodeCursor(encoded+"=", contextKey); err == nil {
		t.Fatal("padded cursor was accepted")
	}
	if _, err := DecodeCursor(strings.Repeat("A", maxCursorLength+1), contextKey); err == nil {
		t.Fatal("oversized cursor was accepted")
	}
}
