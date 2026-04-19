package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func createWithNotes(t *testing.T, session *mcp.ClientSession, title string, noteCount int) string {
	t.Helper()
	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": title},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tk map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tk)
	id, _ := tk["id"].(string)
	if id == "" {
		t.Fatal("no id returned")
	}
	for i := 0; i < noteCount; i++ {
		if _, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "ticket_add_note",
			Arguments: map[string]any{
				"id":   id,
				"text": fmt.Sprintf("note %d", i),
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func showResult(t *testing.T, session *mcp.ClientSession, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("show failed: %v", result.Content)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out)
	return out
}

func TestShow_DefaultCapsNotesToTwenty(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Notes cap", 30)

	out := showResult(t, session, map[string]any{"id": id})

	notes, _ := out["notes"].([]any)
	if len(notes) != 20 {
		t.Errorf("default notes returned = %d, want 20", len(notes))
	}
	if total, _ := out["notes_total"].(float64); int(total) != 30 {
		t.Errorf("notes_total = %v, want 30", out["notes_total"])
	}
	if shown, _ := out["notes_shown"].(float64); int(shown) != 20 {
		t.Errorf("notes_shown = %v, want 20", out["notes_shown"])
	}
	// Default window is newest-first; the last visible note should be the newest.
	last, _ := notes[len(notes)-1].(map[string]any)
	if last["text"] != "note 29" {
		t.Errorf("newest note in default window = %v, want %q", last["text"], "note 29")
	}
	first, _ := notes[0].(map[string]any)
	if first["text"] != "note 10" {
		t.Errorf("oldest note in default window = %v, want %q", first["text"], "note 10")
	}
}

func TestShow_NotesLimitZeroReturnsAll(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Notes all", 25)

	out := showResult(t, session, map[string]any{
		"id":          id,
		"notes_limit": 0,
	})

	notes, _ := out["notes"].([]any)
	if len(notes) != 25 {
		t.Errorf("notes returned = %d, want 25", len(notes))
	}
}

func TestShow_MetadataOnlySkipsNotes(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Metadata only", 5)

	out := showResult(t, session, map[string]any{
		"id":            id,
		"metadata_only": true,
	})

	if _, ok := out["notes"]; ok {
		t.Errorf("notes should be omitted with metadata_only, got %v", out["notes"])
	}
	if total, _ := out["notes_total"].(float64); int(total) != 5 {
		t.Errorf("notes_total = %v, want 5", out["notes_total"])
	}
	if shown, _ := out["notes_shown"].(float64); int(shown) != 0 {
		t.Errorf("notes_shown = %v, want 0", out["notes_shown"])
	}
}

func TestShow_NotesOffsetPagesBack(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Notes paging", 30)

	// Skip the newest 20, ask for next 5.
	out := showResult(t, session, map[string]any{
		"id":           id,
		"notes_limit":  5,
		"notes_offset": 20,
	})

	notes, _ := out["notes"].([]any)
	if len(notes) != 5 {
		t.Fatalf("notes returned = %d, want 5", len(notes))
	}
	first, _ := notes[0].(map[string]any)
	last, _ := notes[len(notes)-1].(map[string]any)
	// After skipping 20 from newest (indices 10..29), next 5 are indices 5..9.
	if first["text"] != "note 5" {
		t.Errorf("first paged note = %v, want %q", first["text"], "note 5")
	}
	if last["text"] != "note 9" {
		t.Errorf("last paged note = %v, want %q", last["text"], "note 9")
	}
}

func TestShow_MetadataOnlyAsString(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "metadata only string", 3)

	out := showResult(t, session, map[string]any{
		"id":            id,
		"metadata_only": "true",
	})
	if _, ok := out["notes"]; ok {
		t.Errorf("notes should be omitted with metadata_only=\"true\"")
	}
}
