package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ticketmcp "github.com/EnderRealm/ticket/internal/mcp"
	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testServer(t *testing.T) *mcp.ClientSession {
	t.Helper()
	dir := t.TempDir()
	store := ticket.NewFileStore(dir)
	server := ticketmcp.NewServer(store, "", "")

	st, ct := mcp.NewInMemoryTransports()

	ctx := context.Background()
	go server.Run(ctx, st)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestCreateTicket(t *testing.T) {
	session := testServer(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":       "Test ticket from MCP",
			"type":        "feature",
			"description": "Created via in-process MCP test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	// Parse the JSON response to verify fields.
	text := result.Content[0].(*mcp.TextContent).Text
	var ticket map[string]any
	if err := json.Unmarshal([]byte(text), &ticket); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if ticket["id"] == nil || ticket["id"] == "" {
		t.Error("ticket ID is empty")
	}
	if ticket["title"] != "Test ticket from MCP" {
		t.Errorf("title = %q, want %q", ticket["title"], "Test ticket from MCP")
	}
	if ticket["status"] != "backlog" {
		t.Errorf("status = %q, want %q", ticket["status"], "backlog")
	}
	created, _ := ticket["created"].(string)
	if created == "" || created == "0001-01-01T00:00:00Z" {
		t.Errorf("created = %q, want non-zero timestamp", created)
	}
}

func TestSearchRanksByRelevance(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":       "Login crash on submit",
			"type":        "bug",
			"description": "users report a crash",
		},
	})
	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":       "Unrelated dashboard work",
			"type":        "feature",
			"description": "the login screen is mentioned in passing",
		},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_search",
		Arguments: map[string]any{"query": "login crash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var resp struct {
		Matches []map[string]any `json:"matches"`
		Total   int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("total = %d, want 2", resp.Total)
	}
	if resp.Matches[0]["title"] != "Login crash on submit" {
		t.Errorf("first match = %q, want %q", resp.Matches[0]["title"], "Login crash on submit")
	}
}

func TestSearchNoMatch(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Some ticket", "type": "feature"},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_search",
		Arguments: map[string]any{"query": "nonexistentterm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var resp struct {
		Matches []map[string]any `json:"matches"`
		Total   int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("total = %d, want 0", resp.Total)
	}
	if len(resp.Matches) != 0 {
		t.Errorf("matches = %d, want 0", len(resp.Matches))
	}
}

func TestReadyExcludesBacklog(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a backlog ticket.
	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Backlog idea",
			"type":  "feature",
		},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_ready",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var tickets []any
	json.Unmarshal([]byte(text), &tickets)
	if len(tickets) != 0 {
		t.Errorf("ticket_ready should exclude backlog, got %d tickets", len(tickets))
	}
}

func TestListExcludesBacklog(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a backlog ticket.
	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Backlog item",
			"type":  "feature",
		},
	})

	// Default list should exclude backlog.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)
	tickets := resp["tickets"].([]any)
	if len(tickets) != 0 {
		t.Errorf("default ticket_list should exclude backlog, got %d", len(tickets))
	}
}

func TestListFilterByStatus(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a backlog ticket.
	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Backlog item",
			"type":  "feature",
		},
	})

	// Explicit status filter should include it.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)
	tickets := resp["tickets"].([]any)
	if len(tickets) != 1 {
		t.Errorf("ticket_list with status=backlog should return 1, got %d", len(tickets))
	}
}

func TestEditStatus(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket (defaults to backlog).
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Status edit test",
			"type":  "feature",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	// Edit status to open.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":     id,
			"status": "open",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	var edited map[string]any
	json.Unmarshal([]byte(text), &edited)
	if edited["status"] != "open" {
		t.Errorf("status after edit = %q, want open", edited["status"])
	}

	// Invalid status should fail.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":     id,
			"status": "invalid",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("editing to invalid status should return error")
	}
}

func TestAddNotePreservesNewlines(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Note test", "type": "feature"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	// Add a note with double newlines.
	noteText := "## Triage\n\n**Risk:** low\n\n**Scope:** single task"
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_add_note",
		Arguments: map[string]any{"id": id, "text": noteText},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("add_note error: %v", result.Content)
	}

	// Read the ticket back and check notes.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	notes, ok := shown["notes"].([]any)
	if !ok {
		t.Fatalf("notes is not an array: %T", shown["notes"])
	}
	if len(notes) != 1 {
		t.Errorf("expected 1 note, got %d", len(notes))
		for i, n := range notes {
			note := n.(map[string]any)
			t.Logf("  note[%d]: %q", i, note["text"])
		}
	}
	if len(notes) > 0 {
		note := notes[0].(map[string]any)
		if note["text"] != noteText {
			t.Errorf("note text = %q, want %q", note["text"], noteText)
		}
	}
}

func TestAddMultipleNotes(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket.
	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Multi note test", "type": "feature"},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	// Add first note with ## headings and blank lines.
	note1 := "## Triage\n\n**Risk:** low\n\n**Scope:** single task\n\n**Key decisions:**\n- Decision one (human)\n- Decision two (human)"
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_add_note",
		Arguments: map[string]any{"id": id, "text": note1},
	})

	// Add second note.
	note2 := "## Spec\n\n**Scope:**\n- In: feature A\n- Out: feature B"
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_add_note",
		Arguments: map[string]any{"id": id, "text": note2},
	})

	// Read back.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	notes, _ := shown["notes"].([]any)
	if len(notes) != 2 {
		t.Errorf("expected 2 notes, got %d", len(notes))
		for i, n := range notes {
			note := n.(map[string]any)
			t.Logf("  note[%d]: %q", i, note["text"])
		}
	}
	if len(notes) >= 1 {
		n := notes[0].(map[string]any)
		if n["text"] != note1 {
			t.Errorf("note[0] text mismatch\ngot:  %q\nwant: %q", n["text"], note1)
		}
	}
	if len(notes) >= 2 {
		n := notes[1].(map[string]any)
		if n["text"] != note2 {
			t.Errorf("note[1] text mismatch\ngot:  %q\nwant: %q", n["text"], note2)
		}
	}
}

func TestEditBodyFields(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Edit body test", "type": "feature"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	// Edit with description, design, and acceptance.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":          id,
			"description": "Updated description text",
			"design":      "The design plan",
			"acceptance":  "When X, the system shall Y",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}

	// Read back and verify all three fields persisted.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	if shown["description"] != "Updated description text" {
		t.Errorf("description = %q, want %q", shown["description"], "Updated description text")
	}
	if shown["design"] != "The design plan" {
		t.Errorf("design = %q, want %q", shown["design"], "The design plan")
	}
	if shown["acceptance_criteria"] != "When X, the system shall Y" {
		t.Errorf("acceptance = %q, want %q", shown["acceptance_criteria"], "When X, the system shall Y")
	}

	// Edit with test_results.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":           id,
			"test_results": "- [x] All tests pass\n- [x] No regressions",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit test_results error: %v", result.Content)
	}

	// Read back and verify test_results persisted alongside other fields.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	json.Unmarshal([]byte(text), &shown)

	if shown["test_results"] != "- [x] All tests pass\n- [x] No regressions" {
		t.Errorf("test_results = %q, want %q", shown["test_results"], "- [x] All tests pass\n- [x] No regressions")
	}
	// Verify earlier fields weren't clobbered.
	if shown["description"] != "Updated description text" {
		t.Errorf("description clobbered: %q", shown["description"])
	}
	if shown["design"] != "The design plan" {
		t.Errorf("design clobbered: %q", shown["design"])
	}
}

func TestCreateTicketWithBranch(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":  "Branch test",
			"type":   "feature",
			"branch": "feature/branch-test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var ticket map[string]any
	if err := json.Unmarshal([]byte(text), &ticket); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if ticket["branch"] != "feature/branch-test" {
		t.Errorf("branch = %q, want %q", ticket["branch"], "feature/branch-test")
	}
}

func TestEditTicketBranch(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket without branch.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Edit branch test", "type": "feature"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	// Branch should be absent.
	if created["branch"] != nil && created["branch"] != "" {
		t.Errorf("expected no branch on create, got %q", created["branch"])
	}

	// Edit to set branch.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":     id,
			"branch": "fix/edit-branch",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}

	// Read back and verify.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	if shown["branch"] != "fix/edit-branch" {
		t.Errorf("branch = %q, want %q", shown["branch"], "fix/edit-branch")
	}
}

func TestCreateTicketMissingTitle(t *testing.T) {
	session := testServer(t)

	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Error("expected error for missing title")
	}
}

func TestEditDesignWithMarkdownHeadings(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Design heading test", "type": "feature"},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	designContent := "## Overview\n\nThis is the overview.\n\n## Architecture\n\nThree-layer design.\n\n## Data Model\n\nUser table with fields."
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": id, "design": designContent},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}

	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	if shown["design"] != designContent {
		t.Errorf("design with headings truncated\ngot:  %q\nwant: %q", shown["design"], designContent)
	}
}

func TestEditDesignLongLine(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Long line test", "type": "feature"},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	longLine := strings.Repeat("x", 70000)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": id, "design": longLine},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}

	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("show error after long line: %v", result.Content)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	design, _ := shown["design"].(string)
	if len(design) != len(longLine) {
		t.Errorf("design length = %d, want %d", len(design), len(longLine))
	}
}

func TestListEmptyReturnsArray(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// List with a filter that matches nothing.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"tag": "nonexistent-tag"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_list error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	tickets, ok := resp["tickets"].([]any)
	if !ok {
		t.Fatalf("tickets field is not an array: %T", resp["tickets"])
	}
	if len(tickets) != 0 {
		t.Errorf("expected 0 tickets, got %d", len(tickets))
	}
	if resp["total"] != float64(0) {
		t.Errorf("total = %v, want 0", resp["total"])
	}
}

func TestListReturnsSummaryFields(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket with body content.
	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":       "Summary field test",
			"type":        "feature",
			"description": "A long description that should not appear in list",
			"design":      "Design content excluded from list",
			"acceptance":  "AC excluded from list",
		},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)

	tickets := resp["tickets"].([]any)
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	tk := tickets[0].(map[string]any)

	// Summary fields present.
	if tk["id"] == nil || tk["id"] == "" {
		t.Error("missing id")
	}
	if tk["title"] != "Summary field test" {
		t.Errorf("title = %q, want %q", tk["title"], "Summary field test")
	}
	if tk["type"] != "feature" {
		t.Errorf("type = %q, want %q", tk["type"], "feature")
	}
	if tk["status"] == nil {
		t.Error("missing status")
	}

	// Body fields absent.
	for _, field := range []string{"description", "design", "acceptance_criteria", "test_results", "notes"} {
		if tk[field] != nil {
			t.Errorf("list response should not include %q, got %v", field, tk[field])
		}
	}
}

func TestListPagination(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create 5 tickets.
	for i := 0; i < 5; i++ {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "ticket_create",
			Arguments: map[string]any{
				"title": fmt.Sprintf("Pagination ticket %d", i),
				"type":  "feature",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// List with limit=2.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"limit": 2, "status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)

	tickets := resp["tickets"].([]any)
	if len(tickets) != 2 {
		t.Errorf("expected 2 tickets, got %d", len(tickets))
	}
	if resp["total"] != float64(5) {
		t.Errorf("total = %v, want 5", resp["total"])
	}
	if resp["limit"] != float64(2) {
		t.Errorf("limit = %v, want 2", resp["limit"])
	}

	// List with offset=3, limit=10.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"offset": 3, "limit": 10, "status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	json.Unmarshal([]byte(text), &resp)

	tickets = resp["tickets"].([]any)
	if len(tickets) != 2 {
		t.Errorf("expected 2 tickets (5 total, offset 3), got %d", len(tickets))
	}
	if resp["offset"] != float64(3) {
		t.Errorf("offset = %v, want 3", resp["offset"])
	}

	// List with limit=0 (unlimited).
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"limit": 0, "status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	json.Unmarshal([]byte(text), &resp)

	tickets = resp["tickets"].([]any)
	if len(tickets) != 5 {
		t.Errorf("limit=0 should return all, got %d", len(tickets))
	}
}

func TestListDefaultLimitUnderCap(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create 3 tickets (under default limit).
	for i := range 3 {
		session.CallTool(ctx, &mcp.CallToolParams{
			Name: "ticket_create",
			Arguments: map[string]any{
				"title": fmt.Sprintf("Default limit ticket %d", i),
				"type":  "feature",
			},
		})
	}

	// List without explicit limit — should use default (50), return all 3.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)

	tickets := resp["tickets"].([]any)
	if len(tickets) != 3 {
		t.Errorf("expected 3 tickets, got %d", len(tickets))
	}
	if resp["limit"] != float64(50) {
		t.Errorf("default limit = %v, want 50", resp["limit"])
	}
}

func TestListDefaultLimitCapsResults(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create 55 tickets to exceed the default limit of 50.
	// Titles vary in the first three non-stopword tokens so each ticket gets a
	// distinct slug; without this the 4-hex collision tail plus rapid-fire
	// creation caused occasional collision-exhaustion failures in Create.
	for i := range 55 {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "ticket_create",
			Arguments: map[string]any{
				"title": fmt.Sprintf("alpha %d beta gamma", i),
				"type":  "feature",
			},
		})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if result.IsError {
			t.Fatalf("create %d: tool error %v", i, result.Content)
		}
	}

	// List without explicit limit — should cap at 50.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)

	tickets := resp["tickets"].([]any)
	if len(tickets) != 50 {
		t.Errorf("expected 50 tickets (capped), got %d", len(tickets))
	}
	if resp["total"] != float64(55) {
		t.Errorf("total = %v, want 55", resp["total"])
	}
}

func TestCreateTicketRemoteRepo(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Set up an alternate repo with .tickets/ directory.
	altDir := t.TempDir()
	altTicketsDir := filepath.Join(altDir, ".tickets")
	if err := os.MkdirAll(altTicketsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a ticket targeting the alternate repo.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Remote repo ticket",
			"type":  "feature",
			"repo":  altDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	// Verify ticket landed in the alternate directory.
	entries, err := os.ReadDir(altTicketsDir)
	if err != nil {
		t.Fatal(err)
	}
	mdFiles := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mdFiles++
		}
	}
	if mdFiles != 1 {
		t.Errorf("expected 1 .md file in alt repo, got %d", mdFiles)
	}

	// Verify the server's default store is unaffected.
	listResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := listResult.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)
	tickets := resp["tickets"].([]any)
	if len(tickets) != 0 {
		t.Errorf("expected 0 tickets in default store, got %d", len(tickets))
	}

	// Create a ticket without repo — should land in default store.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Default store ticket",
			"type":  "feature",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("default create error: %v", result.Content)
	}

	listResult, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text = listResult.Content[0].(*mcp.TextContent).Text
	json.Unmarshal([]byte(text), &resp)
	tickets = resp["tickets"].([]any)
	if len(tickets) != 1 {
		t.Errorf("expected 1 ticket in default store, got %d", len(tickets))
	}
}

func TestCreateTicketRemoteRepoNotFound(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Use a temp dir with no .tickets/ subdirectory.
	noTicketsDir := t.TempDir()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Should fail",
			"type":  "feature",
			"repo":  noTicketsDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error when .tickets/ not found")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "no ticket store found") {
		t.Errorf("error message = %q, want substring %q", text, "no ticket store found")
	}
}

func TestCreateExtraFields(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Extra fields create",
			"type":  "feature",
			"set":   map[string]any{"env": "staging", "team": "backend"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("create error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var tk map[string]any
	json.Unmarshal([]byte(text), &tk)

	if tk["env"] != "staging" {
		t.Errorf("env = %q, want staging", tk["env"])
	}
	if tk["team"] != "backend" {
		t.Errorf("team = %q, want backend", tk["team"])
	}
}

func TestEditExtraFields(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket.
	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Extra edit test", "type": "feature"},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	// Set extra fields.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":  id,
			"set": map[string]any{"env": "prod", "region": "us-east"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}

	// Show and verify.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	if shown["env"] != "prod" {
		t.Errorf("env = %q, want prod", shown["env"])
	}
	if shown["region"] != "us-east" {
		t.Errorf("region = %q, want us-east", shown["region"])
	}

	// Update one, remove another.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":  id,
			"set": map[string]any{"env": "staging", "region": ""},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}

	// Show and verify update/remove.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	text = result.Content[0].(*mcp.TextContent).Text
	var shown2 map[string]any
	json.Unmarshal([]byte(text), &shown2)

	if shown2["env"] != "staging" {
		t.Errorf("env = %q, want staging", shown2["env"])
	}
	if _, exists := shown2["region"]; exists {
		t.Errorf("region should be removed, got %v", shown2["region"])
	}
}

func TestShowExtraFields(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Show extra test",
			"type":  "feature",
			"set":   map[string]any{"env": "dev"},
		},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	if shown["env"] != "dev" {
		t.Errorf("env = %q, want dev", shown["env"])
	}
}

func TestListExtraFields(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "List extra test",
			"type":  "feature",
			"set":   map[string]any{"env": "prod"},
		},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var resp map[string]any
	json.Unmarshal([]byte(text), &resp)

	tickets := resp["tickets"].([]any)
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	tk := tickets[0].(map[string]any)

	if tk["env"] != "prod" {
		t.Errorf("env = %q, want prod", tk["env"])
	}
}

func TestEditExtraReservedKey(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Reserved key test", "type": "feature"},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":  id,
			"set": map[string]any{"status": "done"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for reserved key 'status'")
	}
}

func testCentralServer(t *testing.T, projects ...string) (*mcp.ClientSession, string) {
	t.Helper()
	root := t.TempDir()
	ticketsDir := filepath.Join(root, "tickets")
	if err := os.MkdirAll(ticketsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if err := os.MkdirAll(filepath.Join(ticketsDir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := ticket.NewMultiStore(ticketsDir)
	server := ticketmcp.NewServer(store, "", root)

	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go server.Run(ctx, st)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session, root
}

func TestStoreInfo(t *testing.T) {
	session, root := testCentralServer(t, "alpha", "beta")
	ctx := context.Background()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_store_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var info map[string]any
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if info["central_root"] != root {
		t.Errorf("central_root = %q, want %q", info["central_root"], root)
	}

	projects, ok := info["projects"].(map[string]any)
	if !ok {
		t.Fatalf("projects is not an object: %T", info["projects"])
	}

	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}

	for _, name := range []string{"alpha", "beta"} {
		path, ok := projects[name].(string)
		if !ok {
			t.Errorf("project %q missing", name)
			continue
		}
		expected := filepath.Join(root, "tickets", name)
		if path != expected {
			t.Errorf("project %q path = %q, want %q", name, path, expected)
		}
	}
}

func TestStoreInfoNonCentral(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_store_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for non-central server")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "central mode") {
		t.Errorf("error = %q, want mention of central mode", text)
	}
}

func TestStoreInfoEmpty(t *testing.T) {
	session, _ := testCentralServer(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_store_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var info map[string]any
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	projects, ok := info["projects"].(map[string]any)
	if !ok {
		t.Fatalf("projects is not an object: %T", info["projects"])
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}
