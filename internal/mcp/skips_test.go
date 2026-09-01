package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP transport discards the server's stderr at both ends, so the warning
// the store prints about a file it could not read reaches nobody. These tests
// hold the tools to reporting it on the response instead: without that, a store
// with a corrupt ticket is indistinguishable from a healthy one.

// plantUnreadable writes a file no parse can structure into the store the
// session reads, which the tools themselves cannot produce.
func plantUnreadable(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("---\nid: x\n  status: open\n---\n# Broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// callJSON calls a tool and decodes the object it answered with.
func callJSON(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("%s error: %v", name, result.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatalf("%s: json parse: %v", name, err)
	}
	return out
}

// assertSkipReported checks the response's skipped_files names the planted file
// with a reason, and says the epic derivations in its project are degraded.
func assertSkipReported(t *testing.T, tool string, payload map[string]any, file string) {
	t.Helper()
	skips, ok := payload["skipped_files"].([]any)
	if !ok || len(skips) != 1 {
		t.Fatalf("%s skipped_files = %v, want the one planted file", tool, payload["skipped_files"])
	}
	skip, _ := skips[0].(map[string]any)
	if skip["file"] != file {
		t.Errorf("%s skipped_files[0].file = %v, want %q", tool, skip["file"], file)
	}
	if reason, _ := skip["error"].(string); reason == "" {
		t.Errorf("%s skipped_files[0] = %v, want the reason it was skipped", tool, skip)
	}
	if skip["kind"] != "unreadable" {
		t.Errorf("%s skipped_files[0].kind = %v, want unreadable", tool, skip["kind"])
	}
	if degraded, _ := skip["epic_status_degraded"].(bool); !degraded {
		t.Errorf("%s skipped_files[0] = %v, want epic_status_degraded — the file could be any epic's child", tool, skip)
	}
}

func TestListReportsSkippedFiles(t *testing.T) {
	session, dir := testServerDir(t)
	createTicketID(t, session, map[string]any{"title": "Readable", "type": "feature"})
	plantUnreadable(t, dir, "sk-broken-0001.md")

	payload := callJSON(t, session, "ticket_list", map[string]any{})
	if tickets, _ := payload["tickets"].([]any); len(tickets) != 1 {
		t.Errorf("ticket_list returned %d tickets, want the one that parses", len(tickets))
	}
	assertSkipReported(t, "ticket_list", payload, "sk-broken-0001.md")
}

func TestSearchReportsSkippedFiles(t *testing.T) {
	session, dir := testServerDir(t)
	createTicketID(t, session, map[string]any{"title": "Findable", "type": "feature"})
	plantUnreadable(t, dir, "sk-broken-0002.md")

	payload := callJSON(t, session, "ticket_search", map[string]any{"query": "Findable"})
	if matches, _ := payload["matches"].([]any); len(matches) != 1 {
		t.Errorf("ticket_search returned %d matches, want the one that parses", len(matches))
	}
	assertSkipReported(t, "ticket_search", payload, "sk-broken-0002.md")
}

func TestFrontierReportsSkippedFiles(t *testing.T) {
	session, dir := testServerDir(t)
	ctx := context.Background()
	id := createTicketID(t, session, map[string]any{"title": "Schedulable", "type": "feature"})
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": id, "status": "ready"},
	}); err != nil {
		t.Fatal(err)
	}
	plantUnreadable(t, dir, "sk-broken-0003.md")

	payload := callJSON(t, session, "ticket_frontier", map[string]any{})
	tickets, ok := payload["tickets"].([]any)
	if !ok || len(tickets) != 1 {
		t.Fatalf("ticket_frontier tickets = %v, want the one ready ticket", payload["tickets"])
	}
	assertSkipReported(t, "ticket_frontier", payload, "sk-broken-0003.md")
}

func TestShowReportsAnUnreadableFileDistinctlyFromAMissingTicket(t *testing.T) {
	session, dir := testServerDir(t)
	ctx := context.Background()
	plantUnreadable(t, dir, "sk-broken-0004.md")

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": "sk-broken-0004"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_show on a corrupt file succeeded: %v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "unreadable") || !strings.Contains(text, "sk-broken-0004.md") {
		t.Errorf("ticket_show = %q, want it to report the file as unreadable and name it", text)
	}
	// The two call for opposite repairs, so they must not read the same.
	if strings.Contains(text, "ticket not found:") {
		t.Errorf("ticket_show = %q, want an unreadable file told apart from an absent ticket", text)
	}

	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": "sk-absent-0005"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_show on an absent ticket succeeded: %v", result.Content)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "ticket not found:") {
		t.Errorf("ticket_show on an absent ticket = %q, want it still reported as not found", text)
	}
}

func TestSkippedFileDegradesTheEpicsACallerCanSee(t *testing.T) {
	// The consequence of an unreadable file reaches tickets the caller does see:
	// the file could be any epic's child, so no epic in the project reads done.
	session, dir := testServerDir(t)
	ctx := context.Background()
	epic := createTicketID(t, session, map[string]any{"title": "Epic", "type": "epic"})
	child := createTicketID(t, session, map[string]any{"title": "Child", "type": "feature", "parent": epic})
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": child, "status": "done"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := callJSON(t, session, "ticket_show", map[string]any{"id": epic})["status"]; got != "done" {
		t.Fatalf("epic status = %v with its only child done, want done", got)
	}

	plantUnreadable(t, dir, "sk-broken-0006.md")

	if got := callJSON(t, session, "ticket_show", map[string]any{"id": epic})["status"]; got == "done" {
		t.Errorf("epic still reads done beside an unreadable file that could be its child")
	}
	payload := callJSON(t, session, "ticket_list", map[string]any{"type": "epic"})
	tickets, _ := payload["tickets"].([]any)
	if len(tickets) != 1 {
		t.Fatalf("ticket_list returned %d epics, want 1", len(tickets))
	}
	if status := tickets[0].(map[string]any)["status"]; status == "done" {
		t.Errorf("listed epic reads done beside an unreadable file")
	}
	// And the response says why, rather than leaving the caller with an epic
	// that silently stopped reading done.
	assertSkipReported(t, "ticket_list", payload, "sk-broken-0006.md")
}
