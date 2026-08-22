package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ticketmcp "github.com/EnderRealm/ticket/v8/internal/mcp"
	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain points the user cache directory at a temp tree, so the per-ticket
// lock files a write takes (see ticket.FileStore.lockFile) land there and go
// away with it rather than accumulating in the developer's real cache
// directory. Both variables: os.UserCacheDir reads HOME on darwin and prefers
// XDG_CACHE_HOME elsewhere.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tk-test-cache")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

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

func TestCreateWithRepoResolvesCentralProject(t *testing.T) {
	// A repo with no .tickets/ of its own resolves through the central store
	// config, so the ticket lands in that project's central directory.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	repoDir := t.TempDir()
	projectDir := filepath.Join(root, "tickets", "beta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"beta": {Path: repoDir, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}

	session := testServer(t)
	id := createTicketID(t, session, map[string]any{
		"title": "Central repo override",
		"type":  "feature",
		"repo":  repoDir,
	})

	if _, err := os.Stat(filepath.Join(projectDir, id+".md")); err != nil {
		t.Errorf("ticket %s did not land in the central project dir %s: %v", id, projectDir, err)
	}
}

func TestCreateWithRepoAcceptsConfiguredProjectName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	working := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(working); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)

	repoDir := t.TempDir()
	projectDir := filepath.Join(root, "tickets", "beta")
	pathRepo := filepath.Join(working, "beta")
	pathProjectDir := filepath.Join(root, "tickets", "path-project")
	for _, dir := range []string{projectDir, pathRepo, pathProjectDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := project.Save(project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"beta":         {Path: repoDir, Store: "central"},
			"path-project": {Path: pathRepo, Store: "central"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	session := testServer(t)
	id := createTicketID(t, session, map[string]any{
		"title": "Central project name override",
		"type":  "feature",
		"repo":  "beta",
	})

	if _, err := os.Stat(filepath.Join(projectDir, id+".md")); err != nil {
		t.Errorf("ticket %s did not land in the named central project dir %s: %v", id, projectDir, err)
	}
	if _, err := os.Stat(filepath.Join(pathProjectDir, id+".md")); !os.IsNotExist(err) {
		t.Errorf("ticket %s followed the colliding filesystem path into %s: %v", id, pathProjectDir, err)
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
	if field, _ := resp.Matches[0]["match_field"].(string); field == "" {
		t.Errorf("match_field = %q, want non-empty", resp.Matches[0]["match_field"])
	}
	if snippet, _ := resp.Matches[0]["snippet"].(string); snippet == "" {
		t.Errorf("snippet = %q, want non-empty", resp.Matches[0]["snippet"])
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

func TestReadyReturnsSummaryShape(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	create, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":       "Ready with body",
			"type":        "feature",
			"description": "a lengthy description that should not appear",
			"acceptance":  "criteria that should not appear",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	json.Unmarshal([]byte(create.Content[0].(*mcp.TextContent).Text), &created)
	id, _ := created["id"].(string)

	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": id, "status": "ready"},
	})

	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_add_note",
		Arguments: map[string]any{"id": id, "text": "a note that should not appear"},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_ready",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tickets []map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tickets)
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ready ticket, got %d", len(tickets))
	}
	for _, field := range []string{"description", "acceptance_criteria", "design", "test_results", "notes"} {
		if _, ok := tickets[0][field]; ok {
			t.Errorf("ready ticket should omit %q, got %v", field, tickets[0][field])
		}
	}
	if tickets[0]["title"] != "Ready with body" {
		t.Errorf("title = %q, want summary fields present", tickets[0]["title"])
	}
}

// frontierIDs calls ticket_frontier and returns the IDs it reports.
func frontierIDs(t *testing.T, session *mcp.ClientSession, args map[string]any) []string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_frontier",
		Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_frontier error: %v", result.Content)
	}
	var tickets []map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tickets)
	ids := make([]string, len(tickets))
	for i, tk := range tickets {
		ids[i], _ = tk["id"].(string)
	}
	return ids
}

func TestFrontierExcludesBlockedAndNonReady(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	unblocked := createTicketID(t, session, map[string]any{"title": "Unblocked", "type": "feature"})
	blocker := createTicketID(t, session, map[string]any{"title": "Blocker", "type": "feature"})
	blocked := createTicketID(t, session, map[string]any{"title": "Blocked", "type": "feature"})
	inProgress := createTicketID(t, session, map[string]any{"title": "In progress", "type": "feature"})
	// A backlog ticket is left as created — it must not reach the frontier.
	createTicketID(t, session, map[string]any{"title": "Backlog", "type": "feature"})

	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_dep",
		Arguments: map[string]any{"id": blocked, "dep_id": blocker, "action": "add"},
	})
	for _, id := range []string{unblocked, blocker, blocked} {
		session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "ticket_edit",
			Arguments: map[string]any{"id": id, "status": "ready"},
		})
	}
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": inProgress, "status": "open"},
	})

	ids := frontierIDs(t, session, map[string]any{})
	want := map[string]bool{unblocked: true, blocker: true}
	if len(ids) != len(want) {
		t.Fatalf("frontier = %v, want %v", ids, want)
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("frontier contains unexpected ticket %s: %v", id, ids)
		}
	}
}

func TestFrontierPicksUpFlippedDep(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	blocker := createTicketID(t, session, map[string]any{"title": "Blocker", "type": "feature"})
	blocked := createTicketID(t, session, map[string]any{"title": "Blocked", "type": "feature"})
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_dep",
		Arguments: map[string]any{"id": blocked, "dep_id": blocker, "action": "add"},
	})
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": blocked, "status": "ready"},
	})
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": blocker, "status": "open"},
	})

	if ids := frontierIDs(t, session, map[string]any{}); len(ids) != 0 {
		t.Fatalf("frontier = %v, want empty while the dep is open", ids)
	}

	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": blocker, "status": "done"},
	})

	ids := frontierIDs(t, session, map[string]any{})
	if len(ids) != 1 || ids[0] != blocked {
		t.Errorf("frontier = %v, want [%s] after the dep flipped to done", ids, blocked)
	}
}

func TestFrontierScopedToExplicitProject(t *testing.T) {
	session := testCentralServerWithDefault(t, "alpha", "alpha", "beta")
	ctx := context.Background()

	for _, proj := range []string{"alpha", "beta"} {
		id := createTicketID(t, session, map[string]any{"title": "Ready " + proj, "type": "feature", "project": proj})
		session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "ticket_edit",
			Arguments: map[string]any{"id": id, "status": "ready"},
		})
	}

	ids := frontierIDs(t, session, map[string]any{"project": "beta"})
	if len(ids) != 1 {
		t.Fatalf("frontier = %v, want 1 ticket for project beta", ids)
	}
	if !strings.HasPrefix(ids[0], "beta/") {
		t.Errorf("frontier id = %q, want a beta/ ticket", ids[0])
	}
}

func TestListDefaultStatusSet(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create one ticket per status (ticket_create always starts in backlog).
	statuses := []string{"backlog", "ready", "open", "done", "closed"}
	idByStatus := map[string]string{}
	for _, status := range statuses {
		id := createTicketID(t, session, map[string]any{
			"title": "Item " + status,
			"type":  "feature",
		})
		if status != "backlog" {
			editStatus(t, session, id, status)
		}
		idByStatus[status] = id
	}

	// Default list should include the four non-closed statuses and exclude closed.
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
	ids := listIDs(resp)
	for _, status := range []string{"backlog", "ready", "open", "done"} {
		if !ids[idByStatus[status]] {
			t.Errorf("default ticket_list should include %s ticket %q, got %v", status, idByStatus[status], ids)
		}
	}
	if ids[idByStatus["closed"]] {
		t.Errorf("default ticket_list should exclude closed ticket %q, got %v", idByStatus["closed"], ids)
	}
	if total, _ := resp["total"].(float64); int(total) != 4 {
		t.Errorf("total = %v, want 4", resp["total"])
	}
}

func TestListParentExcludesClosedByDefault(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	parentID := createTicketID(t, session, map[string]any{
		"title": "Parent epic",
		"type":  "epic",
	})

	// Create one child per status spanning the full set.
	statuses := []string{"closed", "done", "backlog", "ready", "open"}
	childByStatus := map[string]string{}
	for _, status := range statuses {
		id := createTicketID(t, session, map[string]any{
			"title":  "Child " + status,
			"type":   "feature",
			"parent": parentID,
		})
		editStatus(t, session, id, status)
		childByStatus[status] = id
	}

	// Default list by parent should return the 4 non-closed children.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"parent": parentID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	if total, _ := resp["total"].(float64); int(total) != 4 {
		t.Errorf("total = %v, want 4", resp["total"])
	}
	for _, tk := range resp["tickets"].([]any) {
		if tk.(map[string]any)["status"] == "closed" {
			t.Errorf("default parent list should not include closed children, got %v", tk)
		}
	}
	ids := listIDs(resp)
	for _, status := range []string{"done", "backlog", "ready", "open"} {
		if !ids[childByStatus[status]] {
			t.Errorf("default parent list should include %s child %q, got %v", status, childByStatus[status], ids)
		}
	}
	if ids[childByStatus["closed"]] {
		t.Errorf("default parent list should exclude closed child %q, got %v", childByStatus["closed"], ids)
	}

	// Sanity: explicit status=closed still returns exactly the closed child.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"parent": parentID, "status": "closed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var closedResp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &closedResp)
	closedTickets := closedResp["tickets"].([]any)
	if len(closedTickets) != 1 {
		t.Fatalf("status=closed should return 1 child, got %d", len(closedTickets))
	}
	if closedTickets[0].(map[string]any)["id"] != childByStatus["closed"] {
		t.Errorf("status=closed returned %v, want %q", closedTickets[0], childByStatus["closed"])
	}
}

// editStatus sets a ticket's status via ticket_edit.
func editStatus(t *testing.T, session *mcp.ClientSession, id, status string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": id, "status": status},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_edit to status %q error: %v", status, result.Content)
	}
}

// listIDs collects the ids of tickets in a ticket_list response.
func listIDs(resp map[string]any) map[string]bool {
	ids := map[string]bool{}
	for _, tk := range resp["tickets"].([]any) {
		ids[tk.(map[string]any)["id"].(string)] = true
	}
	return ids
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

func TestDeleteTicket(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Create a ticket to delete.
	id := createTicketID(t, session, map[string]any{
		"title": "Delete me",
		"type":  "feature",
	})

	// Delete it and verify the response confirms the resolved ID.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_delete",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_delete error: %v", result.Content)
	}
	var resp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	if resp["deleted"] != id {
		t.Errorf("deleted = %q, want %q", resp["deleted"], id)
	}

	// Showing the deleted ticket should now return an error result.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("ticket_show on deleted ticket should return an error")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_delete",
		Arguments: map[string]any{"id": "does-not-exist-0000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("ticket_delete on nonexistent ID should return an error")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "ticket not found") {
		t.Errorf("error message = %q, want substring %q", text, "ticket not found")
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
	// Set up an alternate repo owning its own central project.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	altDir := t.TempDir()
	altTicketsDir := filepath.Join(root, "tickets", "alt")
	if err := os.MkdirAll(altTicketsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"alt": {Path: altDir, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}

	session := testServer(t)
	ctx := context.Background()

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
	// A repo owning no central project, sandboxed HOME included so the
	// resolution cannot match one of the machine's own.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	working := t.TempDir()
	if err := project.Save(project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"known": {Path: working, Store: "central"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(working); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	noStoreDir := t.TempDir()

	session := testServer(t)
	for _, repo := range []string{noStoreDir, "missing-project"} {
		t.Run(repo, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "ticket_create",
				Arguments: map[string]any{
					"title": "Should fail",
					"type":  "feature",
					"repo":  repo,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Errorf("expected an error for unknown repo value %q", repo)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			wants := []string{repo}
			if repo == noStoreDir {
				wants = append(wants, "no ticket store found", "tk init")
			} else {
				wants = append(wants, "neither a registered project name nor a directory")
			}
			for _, want := range wants {
				if !strings.Contains(text, want) {
					t.Errorf("error message = %q, want substring %q", text, want)
				}
			}
		})
	}
}

func TestCreateTicketRemoteRepoNamesALegacyTicketsDir(t *testing.T) {
	// A repo still holding a .tickets/ is told where those tickets are rather
	// than having the directory written to, and nothing in it is touched.
	t.Setenv("HOME", t.TempDir())
	if err := project.Save(project.Config{CentralRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	repoDir := t.TempDir()
	legacy := filepath.Join(repoDir, ".tickets")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	session := testServer(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Should fail",
			"type":  "feature",
			"repo":  repoDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected an error for a repo holding only a .tickets/")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{legacy, "tk init"} {
		if !strings.Contains(text, want) {
			t.Errorf("error message = %q, want substring %q", text, want)
		}
	}
	entries, err := os.ReadDir(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf(".tickets/ holds %v, want nothing written into it", entries)
	}
}

func TestCreateTicketRemoteRepoWarnsUnregisteredProject(t *testing.T) {
	// The CLI lists and writes an unregistered central project, warning as it
	// goes; this surface reported the same repo as having no store at all, so
	// one repo gave two answers. The warning rides on the response because
	// stderr here is the server's log, not something the caller reads.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	repoDir := t.TempDir()
	if err := project.Save(project.Config{CentralRoot: root}); err != nil {
		t.Fatal(err)
	}
	strayDir := filepath.Join(root, "tickets", filepath.Base(repoDir))
	if err := os.MkdirAll(strayDir, 0o755); err != nil {
		t.Fatal(err)
	}

	session := testServer(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Into an unregistered project",
			"type":  "feature",
			"repo":  repoDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	warning, _ := resp["unregistered_warning"].(string)
	for _, want := range []string{filepath.Base(repoDir), "tk init"} {
		if !strings.Contains(warning, want) {
			t.Errorf("unregistered_warning = %q, want substring %q", warning, want)
		}
	}
	id, _ := resp["id"].(string)
	if _, err := os.Stat(filepath.Join(strayDir, id+".md")); err != nil {
		t.Errorf("ticket %s did not land in the unregistered project dir %s: %v", id, strayDir, err)
	}
}

func TestCreateTicketRemoteRepoRefusesATraversingProjectName(t *testing.T) {
	// A project name resolved from a config path mapping is the config map key
	// verbatim, and the shared config arrives over git from other machines.
	// filepath.Join cleans the traversal rather than failing, so an unbounded
	// name steers this write out of the central root.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	repoDir := t.TempDir()
	cfg := project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"../escaped": {Path: repoDir, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}

	session := testServer(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Should fail",
			"type":  "feature",
			"repo":  repoDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected a refusal for a project name that leaves the store, got %v", result.Content)
	}
	escaped := filepath.Join(root, "escaped")
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Errorf("a ticket directory was created outside the central tickets root at %s: %v", escaped, err)
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

func TestEditOutputs(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Outputs edit test", "type": "feature"},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":      id,
			"outputs": map[string]any{"branch": "feature-x", "commit": "abc1234"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}

	// ticket_show returns outputs as a nested object, not flattened like extras.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	text = result.Content[0].(*mcp.TextContent).Text
	var shown map[string]any
	json.Unmarshal([]byte(text), &shown)

	outputs, ok := shown["outputs"].(map[string]any)
	if !ok {
		t.Fatalf("outputs missing from show response: %s", text)
	}
	if outputs["branch"] != "feature-x" || outputs["commit"] != "abc1234" {
		t.Errorf("outputs = %v, want branch=feature-x commit=abc1234", outputs)
	}

	// Empty value removes the key.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":      id,
			"outputs": map[string]any{"commit": ""},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	var edited map[string]any
	json.Unmarshal([]byte(text), &edited)

	outputs = edited["outputs"].(map[string]any)
	if _, exists := outputs["commit"]; exists {
		t.Errorf("commit should be removed, got %v", outputs)
	}
	if outputs["branch"] != "feature-x" {
		t.Errorf("branch = %v, want feature-x", outputs["branch"])
	}
}

func TestDepCargo(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	id := createTicketID(t, session, map[string]any{"title": "Cargo dependent", "type": "feature"})
	depID := createTicketID(t, session, map[string]any{"title": "Cargo blocker", "type": "feature"})
	bareID := createTicketID(t, session, map[string]any{"title": "Bare blocker", "type": "feature"})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_dep",
		Arguments: map[string]any{
			"id": id, "dep_id": depID, "action": "add",
			"cargo": "event schema: the ingest table",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_dep error: %v", result.Content)
	}
	var tk map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tk)
	cargo, ok := tk["dep_cargo"].(map[string]any)
	if !ok {
		t.Fatalf("dep_cargo missing from response: %v", tk)
	}
	if cargo[depID] != "event schema: the ingest table" {
		t.Errorf("dep_cargo[%s] = %v, want the cargo", depID, cargo[depID])
	}

	// A dep added without cargo leaves the map untouched.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_dep",
		Arguments: map[string]any{"id": id, "dep_id": bareID, "action": "add"},
	})
	var bare map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &bare)
	cargo, _ = bare["dep_cargo"].(map[string]any)
	if len(cargo) != 1 || cargo[depID] == nil {
		t.Errorf("dep_cargo = %v, want only the annotated edge", cargo)
	}

	// Removing the dep drops its cargo.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_dep",
		Arguments: map[string]any{"id": id, "dep_id": depID, "action": "remove"},
	})
	var removed map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &removed)
	if _, exists := removed["dep_cargo"]; exists {
		t.Errorf("dep_cargo = %v, want omitted after the dep was removed", removed["dep_cargo"])
	}
}

func TestDepCargoRejectsInvalid(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	id := createTicketID(t, session, map[string]any{"title": "Cargo dependent", "type": "feature"})
	depID := createTicketID(t, session, map[string]any{"title": "Cargo blocker", "type": "feature"})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_dep",
		Arguments: map[string]any{
			"id": id, "dep_id": depID, "action": "add",
			"cargo": "two\nlines",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Errorf("expected an error result for cargo containing a newline: %v", result.Content)
	}
}

func TestEditToDonePopulatesBranchOutput(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	result, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Done outputs test", "type": "feature", "branch": "feature-x"},
	})
	text := result.Content[0].(*mcp.TextContent).Text
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	id := created["id"].(string)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": id, "status": "done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("edit error: %v", result.Content)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	var edited map[string]any
	json.Unmarshal([]byte(text), &edited)

	outputs, ok := edited["outputs"].(map[string]any)
	if !ok {
		t.Fatalf("outputs missing after done transition: %s", text)
	}
	if outputs["branch"] != "feature-x" {
		t.Errorf("outputs[branch] = %v, want feature-x", outputs["branch"])
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

func TestListFieldFilter(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Prod deploy",
			"type":  "feature",
			"set":   map[string]any{"env": "production"},
		},
	})
	session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Staging deploy",
			"type":  "feature",
			"set":   map[string]any{"env": "staging"},
		},
	})

	// Substring match: env=prod matches the production ticket only.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog", "field": "env=prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_list error: %v", result.Content)
	}
	var resp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	tickets := resp["tickets"].([]any)
	if len(tickets) != 1 {
		t.Fatalf("field=env=prod should return 1, got %d", len(tickets))
	}
	if tickets[0].(map[string]any)["title"] != "Prod deploy" {
		t.Errorf("matched ticket = %q, want %q", tickets[0].(map[string]any)["title"], "Prod deploy")
	}

	// No substring match → empty result.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"status": "backlog", "field": "env=nope"},
	})
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	if len(resp["tickets"].([]any)) != 0 {
		t.Errorf("field=env=nope should return 0, got %d", len(resp["tickets"].([]any)))
	}

	// Malformed filter → error result.
	result, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"field": "bogusformat"},
	})
	if !result.IsError {
		t.Error("malformed field filter should return error")
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

func testCentralServerWithDefault(t *testing.T, defaultProject string, projects ...string) *mcp.ClientSession {
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
	server := ticketmcp.NewServer(store, defaultProject, root)

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

// createTicketID calls ticket_create and returns the new ticket's ID.
func createTicketID(t *testing.T, session *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_create error: %v", result.Content)
	}
	var tk map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tk)
	id, _ := tk["id"].(string)
	if id == "" {
		t.Fatal("ticket_create returned empty id")
	}
	return id
}

// makeBlocked creates a blocker and a dependent open ticket in the given project,
// returning the dependent ticket's ID (which is blocked by the unresolved blocker).
func makeBlocked(t *testing.T, session *mcp.ClientSession, project string) string {
	t.Helper()
	ctx := context.Background()
	blocker := createTicketID(t, session, map[string]any{"title": "blocker", "type": "feature", "project": project})
	dependent := createTicketID(t, session, map[string]any{"title": "dependent", "type": "feature", "project": project})
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_dep",
		Arguments: map[string]any{"id": dependent, "dep_id": blocker, "action": "add"},
	})
	session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": dependent, "status": "open"},
	})
	return dependent
}

func TestBlockedScopedToDefaultProject(t *testing.T) {
	session := testCentralServerWithDefault(t, "alpha", "alpha", "beta")
	ctx := context.Background()

	alphaBlocked := makeBlocked(t, session, "alpha")
	makeBlocked(t, session, "beta")

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_blocked",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_blocked error: %v", result.Content)
	}

	var tickets []map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tickets)
	if len(tickets) != 1 {
		t.Fatalf("expected 1 blocked ticket scoped to default project, got %d", len(tickets))
	}
	if tickets[0]["id"] != alphaBlocked {
		t.Errorf("blocked id = %q, want %q", tickets[0]["id"], alphaBlocked)
	}
}

func TestBlockedScopedToExplicitProject(t *testing.T) {
	session := testCentralServerWithDefault(t, "alpha", "alpha", "beta")
	ctx := context.Background()

	makeBlocked(t, session, "alpha")
	betaBlocked := makeBlocked(t, session, "beta")

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_blocked",
		Arguments: map[string]any{"project": "beta"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_blocked error: %v", result.Content)
	}

	var tickets []map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tickets)
	if len(tickets) != 1 {
		t.Fatalf("expected 1 blocked ticket for explicit project, got %d", len(tickets))
	}
	if tickets[0]["id"] != betaBlocked {
		t.Errorf("blocked id = %q, want %q", tickets[0]["id"], betaBlocked)
	}
}

func TestStoreInfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

// store_info is where an agent looks up what projects exist, so it names the
// ones no repo is registered to. "nostore" covers the half of that set a
// presence check misses: `store: central` lives in the shared config alone, so a
// project whose shared config is missing keeps only its local path entry.
func TestStoreInfoMarksUnregisteredProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	session, root := testCentralServer(t, "alpha", "nostore", "stray")
	ctx := context.Background()

	cfg := project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"alpha":   {Path: filepath.Join(home, "alpha"), Store: "central"},
			"nostore": {Path: filepath.Join(home, "nostore")},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

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

	var info map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &info); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	unregistered, ok := info["unregistered"].([]any)
	if !ok {
		t.Fatalf("unregistered is not an array: %T", info["unregistered"])
	}
	if len(unregistered) != 2 || unregistered[0] != "nostore" || unregistered[1] != "stray" {
		t.Errorf("unregistered = %v, want [nostore stray]", unregistered)
	}
	projects, _ := info["projects"].(map[string]any)
	if len(projects) != 3 {
		t.Errorf("expected all projects listed, got %v", projects)
	}
}

// An agent reading a project's tickets should learn the project is unregistered
// from the same response, not by knowing to cross-reference ticket_store_info.
// Unregistered is a property of a project, not of a ticket, so the field rides
// on the response rather than repeating on every row.
func TestListMarksUnregisteredProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	session, root := testCentralServer(t, "alpha", "stray")
	ctx := context.Background()

	cfg := project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"alpha": {Path: filepath.Join(home, "alpha"), Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	createTicketID(t, session, map[string]any{"title": "Registered", "project": "alpha"})
	createTicketID(t, session, map[string]any{"title": "Stray", "project": "stray"})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	if tickets, _ := resp["tickets"].([]any); len(tickets) != 2 {
		t.Fatalf("expected both tickets listed, got %v", resp["tickets"])
	}
	unregistered, ok := resp["unregistered_projects"].([]any)
	if !ok {
		t.Fatalf("unregistered_projects is not an array: %T", resp["unregistered_projects"])
	}
	if len(unregistered) != 1 || unregistered[0] != "stray" {
		t.Errorf("unregistered_projects = %v, want [stray]", unregistered)
	}

	// Scoped to a registered project, no project in the response is
	// unregistered, so the field is omitted.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"project": "alpha"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var scoped map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &scoped)
	if _, present := scoped["unregistered_projects"]; present {
		t.Errorf("unregistered_projects = %v, want omitted", scoped["unregistered_projects"])
	}
}

// The field describes the result set, like total, not the page. Derived after
// pagination, an unregistered project whose tickets sort past the default limit
// of 50 would go unnamed on a busy store, making the signal depend on which page
// was asked for.
func TestListMarksUnregisteredProjectsPastFirstPage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	session, root := testCentralServer(t, "alpha", "stray")
	ctx := context.Background()

	cfg := project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"alpha": {Path: filepath.Join(home, "alpha"), Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	// Priority orders the two tickets, so the stray one lands on page two.
	createTicketID(t, session, map[string]any{"title": "Registered", "project": "alpha", "priority": 0})
	createTicketID(t, session, map[string]any{"title": "Stray", "project": "stray", "priority": 4})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"limit": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	tickets, _ := resp["tickets"].([]any)
	if len(tickets) != 1 {
		t.Fatalf("expected one ticket on the page, got %v", resp["tickets"])
	}
	if id, _ := tickets[0].(map[string]any)["id"].(string); !strings.HasPrefix(id, "alpha/") {
		t.Fatalf("page should hold the registered project's ticket, got %q", id)
	}
	unregistered, ok := resp["unregistered_projects"].([]any)
	if !ok {
		t.Fatalf("unregistered_projects is not an array: %T", resp["unregistered_projects"])
	}
	if len(unregistered) != 1 || unregistered[0] != "stray" {
		t.Errorf("unregistered_projects = %v, want [stray]", unregistered)
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
	t.Setenv("HOME", t.TempDir())
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

// verifyServer starts a central-store server with project "alpha" registered to
// a temp repo directory, so ticket_verify can resolve an execution directory.
// Returns the session and that repo directory.
func verifyServer(t *testing.T) (*mcp.ClientSession, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	root := t.TempDir()
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tickets", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := project.Config{
		CentralRoot: root,
		VerifyAllow: []string{"/bin/cat", "/bin/echo", "/bin/sh"},
		Projects: map[string]project.ProjectConfig{
			"alpha": {Path: repoDir, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}

	store := ticket.NewMultiStore(filepath.Join(root, "tickets"))
	server := ticketmcp.NewServer(store, "alpha", root)

	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go server.Run(ctx, st)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session, repoDir
}

func TestVerifyRunsCriterionCommands(t *testing.T) {
	session, repoDir := verifyServer(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(repoDir, "marker.txt"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := createTicketID(t, session, map[string]any{
		"title": "Verifiable ticket",
		"type":  "feature",
		"acceptance": "- Passing check.\n  verify: /bin/cat marker.txt\n" +
			"- Failing check.\n  verify: /bin/sh -c 'echo boom; exit 3'\n" +
			"- Docs updated.\n",
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_verify",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_verify error: %v", result.Content)
	}

	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if report.ID != id {
		t.Errorf("id = %q, want %q", report.ID, id)
	}
	if report.Dir != repoDir {
		t.Errorf("dir = %q, want %q", report.Dir, repoDir)
	}
	if report.OK {
		t.Error("ok = true, want false with a failing criterion")
	}
	if report.Summary.Pass != 1 || report.Summary.Fail != 1 || report.Summary.Unverified != 1 {
		t.Errorf("summary = %+v, want 1 pass, 1 fail, 1 unverified", report.Summary)
	}
	if len(report.Results) != 3 {
		t.Fatalf("got %d results, want 3", len(report.Results))
	}
	if report.Results[0].Status != string(ticket.VerifyPass) || report.Results[0].Output != "here" {
		t.Errorf("first result = %+v, want a pass reading marker.txt in the repo dir", report.Results[0])
	}
	if report.Results[1].Status != string(ticket.VerifyFail) || report.Results[1].ExitCode != 3 {
		t.Errorf("second result = %+v, want fail with exit 3", report.Results[1])
	}
	if report.Results[2].Status != string(ticket.VerifyUnverified) || report.Results[2].Command != "" {
		t.Errorf("third result = %+v, want unverified with no command", report.Results[2])
	}

	// Results are recorded on the ticket.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	var shown map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &shown)
	testResults, _ := shown["test_results"].(string)
	for _, want := range []string{
		"1 pass, 1 fail, 0 refused, 1 unverified",
		"- PASS (exit 0): Passing check.",
		"- FAIL (exit 3): Failing check.",
		"- UNVERIFIED: Docs updated.",
	} {
		if !strings.Contains(testResults, want) {
			t.Errorf("test_results missing %q:\n%s", want, testResults)
		}
	}
	if !strings.HasPrefix(testResults, "verify 20") {
		t.Errorf("test_results should start with a timestamped verify header, got:\n%s", testResults)
	}
}

func TestVerifyUnresolvableProject(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	id := createTicketID(t, session, map[string]any{
		"title":      "Unregistered project",
		"type":       "feature",
		"acceptance": "- Passing check.\n  verify: /bin/echo ok\n",
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_verify",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_verify should error when no project directory resolves: %v", result.Content)
	}
}

// verifyRefusalCase creates a sentinel file and a ticket whose only criterion
// tries to delete it, then runs ticket_verify. Returns the tool result and the
// sentinel path so a caller can prove the command never ran.
func verifyRefusalCase(t *testing.T, session *mcp.ClientSession) (*mcp.CallToolResult, string, string) {
	t.Helper()
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := createTicketID(t, session, map[string]any{
		"title":      "Hostile ticket",
		"type":       "feature",
		"acceptance": "- Hostile check.\n  verify: rm -rf " + sentinel + "\n",
	})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_verify",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result, sentinel, id
}

func TestVerifyRefusesCommandOutsideAllowList(t *testing.T) {
	session, _ := verifyServer(t)

	result, sentinel, id := verifyRefusalCase(t, session)
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("ticket content deleted the sentinel: %v", err)
	}
	if result.IsError {
		t.Fatalf("ticket_verify error: %v", result.Content)
	}

	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if report.OK {
		t.Error("ok = true, want false when a criterion was refused")
	}
	if report.Summary.Refused != 1 || report.Summary.Fail != 0 {
		t.Errorf("summary = %+v, want the refusal counted as refused, not failed", report.Summary)
	}
	if report.Results[0].Status != string(ticket.VerifyRefused) {
		t.Errorf("status = %q, want refused", report.Results[0].Status)
	}
	for _, want := range []string{"rm", "verify_allow", "~/.ticket/config.yaml"} {
		if !strings.Contains(report.Results[0].Output, want) {
			t.Errorf("refusal missing %q, so a user can't act on it:\n%s", want, report.Results[0].Output)
		}
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	var shown map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &shown)
	testResults, _ := shown["test_results"].(string)
	if !strings.Contains(testResults, "0 pass, 0 fail, 1 refused, 0 unverified") {
		t.Errorf("test_results should count the refusal separately:\n%s", testResults)
	}
	if strings.Contains(testResults, "FAIL") {
		t.Errorf("a refusal must not be recorded as a failure:\n%s", testResults)
	}
}

func TestVerifySharedConfigCannotWidenAllowList(t *testing.T) {
	session, _ := verifyServer(t)

	// The shared config lives in the synced tickets repo, so it is reachable by
	// whatever plants the command. It must not grant permission.
	sharedPath, err := project.SharedConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sharedPath, []byte("verify_allow:\n  - rm\n"+string(raw)), 0o644); err != nil {
		t.Fatal(err)
	}

	result, sentinel, _ := verifyRefusalCase(t, session)
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("an allow-list planted in the shared config widened what runs: %v", err)
	}

	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if report.Results[0].Status != string(ticket.VerifyRefused) {
		t.Errorf("status = %q, want refused", report.Results[0].Status)
	}
}

func TestVerifyTicketContentCannotWidenAllowList(t *testing.T) {
	session, _ := verifyServer(t)
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := createTicketID(t, session, map[string]any{
		"title":       "Self-permitting ticket",
		"type":        "feature",
		"description": "verify_allow: [rm]",
		"acceptance":  "- Hostile check.\n  verify: rm -rf " + sentinel + "\n",
	})

	// The ticket is the one place the attacker certainly writes, so plant the
	// key in its frontmatter too. Frontmatter is a closed struct and VerifyAllow
	// never reads ticket content, which is what this pins: a future frontmatter
	// field must not become a permission grant.
	dir, err := project.CentralProjectDir("alpha")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, strings.TrimPrefix(id, "alpha/")+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "---\n", "---\nverify_allow:\n  - rm\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_verify",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("an allow-list planted in the ticket widened what runs: %v", err)
	}

	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if report.Results[0].Status != string(ticket.VerifyRefused) {
		t.Errorf("status = %q, want refused", report.Results[0].Status)
	}
}

func TestVerifyToolArgumentCannotWidenAllowList(t *testing.T) {
	session, _ := verifyServer(t)
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := createTicketID(t, session, map[string]any{
		"title":      "Hostile ticket",
		"type":       "feature",
		"acceptance": "- Hostile check.\n  verify: rm -rf " + sentinel + "\n",
	})

	// ticket_verify takes an id and nothing else: an allow-list argument is
	// rejected by the tool's schema, so a caller has no way to widen what runs.
	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_verify",
		Arguments: map[string]any{"id": id, "verify_allow": []string{"rm"}, "allow": "rm"},
	})
	if err == nil {
		t.Error("ticket_verify accepted an allow-list argument")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("a tool argument widened what runs: %v", err)
	}
}

func TestVerifyGivesNoShellSemantics(t *testing.T) {
	session, repoDir := verifyServer(t)
	sentinel := filepath.Join(repoDir, "sentinel.txt")

	id := createTicketID(t, session, map[string]any{
		"title": "Metacharacters",
		"type":  "feature",
		"acceptance": "- Literal metacharacters.\n" +
			"  verify: /bin/echo a ; touch " + sentinel + " && echo $(whoami) `id` ~\n",
	})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_verify",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_verify error: %v", result.Content)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("the second half of a chained command ran: sentinel stat err = %v", err)
	}

	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	want := "a ; touch " + sentinel + " && echo $(whoami) `id` ~"
	if report.Results[0].Status != string(ticket.VerifyPass) || report.Results[0].Output != want {
		t.Errorf("result = %+v, want a pass echoing the metacharacters literally: %q", report.Results[0], want)
	}
}

func TestVerifyNoAcceptanceCriteria(t *testing.T) {
	session, _ := verifyServer(t)
	ctx := context.Background()

	id := createTicketID(t, session, map[string]any{
		"title":       "No criteria",
		"type":        "feature",
		"description": "Description only.",
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_verify",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_verify should error on a ticket with no acceptance criteria: %v", result.Content)
	}
}

func TestCreateRejectsNonEpicParent(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	featureID := createTicketID(t, session, map[string]any{"title": "A feature", "type": "feature"})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": "Child of a feature", "type": "feature", "parent": featureID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_create should reject a non-epic parent: %v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, featureID) || !strings.Contains(text, "not an epic") {
		t.Errorf("error should name the parent and its type, got: %s", text)
	}
}

func TestEditRejectsParentOnEpic(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	epicID := createTicketID(t, session, map[string]any{"title": "An epic", "type": "epic"})
	childID := createTicketID(t, session, map[string]any{"title": "A child", "type": "feature", "parent": epicID})

	// Promoting a child to an epic would give an epic a parent.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": childID, "type": "epic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_edit should reject an epic with a parent: %v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "epics are top level") {
		t.Errorf("error should explain that epics are top level, got: %s", text)
	}
}

func TestEditClearsParent(t *testing.T) {
	// The remedy the validation error names is "repoint or clear it", so an
	// explicit empty parent has to clear the field rather than mean "no change".
	session := testServer(t)
	ctx := context.Background()

	epicID := createTicketID(t, session, map[string]any{"title": "An epic", "type": "epic"})
	childID := createTicketID(t, session, map[string]any{"title": "A child", "type": "feature", "parent": epicID})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": childID, "parent": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_edit with an empty parent should clear it: %v", result.Content)
	}

	var tk map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tk); err != nil {
		t.Fatal(err)
	}
	if parent, ok := tk["parent"]; ok && parent != "" {
		t.Errorf("parent = %v, want cleared", parent)
	}

	// An omitted parent still means "no change".
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": childID, "parent": epicID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_edit should accept an epic parent: %v", result.Content)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": childID, "title": "A renamed child"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_edit: %v", result.Content)
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tk); err != nil {
		t.Fatal(err)
	}
	if tk["parent"] != epicID {
		t.Errorf("parent = %v after an edit that omitted it, want %s", tk["parent"], epicID)
	}
}

func TestEpicStatusDerivedInResponses(t *testing.T) {
	// An epic's status is computed from its children, so every response that
	// carries one has to show the same value.
	session := testServer(t)
	ctx := context.Background()

	epicID := createTicketID(t, session, map[string]any{"title": "An epic", "type": "epic"})
	childID := createTicketID(t, session, map[string]any{"title": "A child", "type": "feature", "parent": epicID})

	epic := showResult(t, session, map[string]any{"id": epicID})
	if epic["status"] != "backlog" {
		t.Errorf("ticket_show: epic status = %v, want backlog", epic["status"])
	}

	editStatus(t, session, childID, "open")
	epic = showResult(t, session, map[string]any{"id": epicID})
	if epic["status"] != "open" {
		t.Errorf("ticket_show: epic status = %v, want open", epic["status"])
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"type": "epic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	listed := resp["tickets"].([]any)[0].(map[string]any)
	if listed["status"] != "open" {
		t.Errorf("ticket_list: epic status = %v, want open", listed["status"])
	}

	editStatus(t, session, childID, "done")
	epic = showResult(t, session, map[string]any{"id": epicID})
	if epic["status"] != "done" {
		t.Errorf("ticket_show: epic status = %v, want done once every child is terminal", epic["status"])
	}
}

func TestEditRejectsManualEpicStatus(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	epicID := createTicketID(t, session, map[string]any{"title": "An epic", "type": "epic"})
	createTicketID(t, session, map[string]any{"title": "A child", "type": "feature", "parent": epicID})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": epicID, "status": "done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_edit should reject a status set by hand on an epic: %v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, epicID) || !strings.Contains(text, "derived from its children") {
		t.Errorf("error should name the epic and say the status is derived, got: %s", text)
	}
}

func TestEditClosingEpicCascadesToChildren(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	epicID := createTicketID(t, session, map[string]any{"title": "An epic", "type": "epic"})
	childID := createTicketID(t, session, map[string]any{"title": "A child", "type": "feature", "parent": epicID})
	editStatus(t, session, childID, "open")

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": epicID, "status": "closed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_edit should accept closing an epic: %v", result.Content)
	}

	// The edit mutated a ticket besides the one it was asked to write, so it
	// says which rather than leaving the caller to list the epic's children.
	var edited map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &edited)
	closed, _ := edited["closed_children"].([]any)
	if len(closed) != 1 || closed[0] != childID {
		t.Errorf("closed_children = %v, want [%s]", edited["closed_children"], childID)
	}

	child := showResult(t, session, map[string]any{"id": childID})
	if child["status"] != "closed" {
		t.Errorf("child status = %v, want closed — closing an epic closes its children", child["status"])
	}
	epic := showResult(t, session, map[string]any{"id": epicID})
	if epic["status"] != "closed" {
		t.Errorf("epic status = %v, want closed", epic["status"])
	}
	if _, ok := epic["closed_children"]; ok {
		t.Errorf("ticket_show carried closed_children: %v", epic["closed_children"])
	}
}

func TestEditPromotesToEpic(t *testing.T) {
	// Changing a ticket's type to epic carries the status it was read with,
	// which was never a status chosen for an epic — the promotion is one normal
	// edit, not one that has to reset the status in the same call.
	session := testServer(t)
	ctx := context.Background()

	id := createTicketID(t, session, map[string]any{"title": "Grew into an epic", "type": "feature"})
	editStatus(t, session, id, "open")

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": id, "type": "epic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_edit should accept promoting a ticket to an epic: %v", result.Content)
	}

	tk := showResult(t, session, map[string]any{"id": id})
	if tk["type"] != "epic" {
		t.Errorf("type = %v, want epic", tk["type"])
	}
	if tk["status"] != "backlog" {
		t.Errorf("status = %v, want backlog — a childless epic derives backlog", tk["status"])
	}
}

func TestEditPromotesToEpicWithAStatus(t *testing.T) {
	// type and status arrive in one call, so the status was passed rather than
	// carried: closed is the abandon it looks like, and any other status is
	// refused the way it is on an epic that already existed.
	session := testServer(t)
	ctx := context.Background()

	closedID := createTicketID(t, session, map[string]any{"title": "Promoted and abandoned", "type": "feature"})
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": closedID, "type": "epic", "status": "closed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_edit should accept promoting a ticket and abandoning it: %v", result.Content)
	}
	tk := showResult(t, session, map[string]any{"id": closedID})
	if tk["status"] != "closed" || tk["abandoned"] != true {
		t.Errorf("promoted epic = %v [abandoned %v], want closed with the intent recorded", tk["status"], tk["abandoned"])
	}

	readyID := createTicketID(t, session, map[string]any{"title": "Promoted and set ready", "type": "feature"})
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_edit",
		Arguments: map[string]any{"id": readyID, "type": "epic", "status": "ready"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("ticket_edit should refuse a status set alongside the promotion: %v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "derived from its children") {
		t.Errorf("error should say an epic's status is derived, got: %s", text)
	}
	if tk := showResult(t, session, map[string]any{"id": readyID}); tk["type"] == "epic" {
		t.Error("the refused edit promoted the ticket anyway")
	}
}

// closeTag and openTag build tool-call envelope markup from its pieces, the
// same way pkg/ticket does — a terminator spelled out verbatim in this file
// would corrupt the tool call of any agent that quotes it, which is the failure
// under test.
func closeTag(name string) string { return "</" + name + ">" }

func openTag(name, attrs string) string { return "<" + name + " " + attrs + ">" }

// corruptedDescription is what a create call leaves behind when the caller
// closes the description parameter with the wrong tag: the acceptance argument
// and the call terminator end up inside the description's value.
func corruptedDescription() string {
	return "The real description text.\n" + closeTag("antml:description") + "\n" +
		openTag("antml:parameter", `name="acceptance"`) + "\n- The criteria that never arrived\n" +
		closeTag("antml:parameter") + "\n" + closeTag("antml:invoke")
}

func TestCreateRejectsEnvelopeFragment(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	for _, field := range []string{"description", "design", "acceptance"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "ticket_create",
			Arguments: map[string]any{"title": "Envelope " + field, field: corruptedDescription()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatalf("ticket_create stored a %s carrying an envelope fragment: %v", field, result.Content)
		}
		text := result.Content[0].(*mcp.TextContent).Text
		if !strings.Contains(text, field) {
			t.Errorf("error does not name the field %q: %s", field, text)
		}
		if !strings.Contains(text, closeTag("antml:invoke")) {
			t.Errorf("error does not show the offending tail: %s", text)
		}
	}

	// Nothing was written: a refused create leaves no half-formed ticket.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	if len(listIDs(resp)) != 0 {
		t.Errorf("refused creates left tickets in the store: %v", resp)
	}
}

func TestEditRejectsEnvelopeFragment(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()
	id := createTicketID(t, session, map[string]any{
		"title":       "Edit envelope test",
		"description": "The original description",
		"acceptance":  "The original criteria",
	})

	for _, field := range []string{"description", "design", "acceptance", "test_results"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "ticket_edit",
			Arguments: map[string]any{"id": id, field: corruptedDescription()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatalf("ticket_edit stored a %s carrying an envelope fragment: %v", field, result.Content)
		}
		text := result.Content[0].(*mcp.TextContent).Text
		if !strings.Contains(text, field) || !strings.Contains(text, closeTag("antml:invoke")) {
			t.Errorf("error should name %q and show the tail: %s", field, text)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	var shown map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &shown)
	if shown["description"] != "The original description" {
		t.Errorf("a refused edit changed the description: %q", shown["description"])
	}
}

// A ticket that discusses envelope markup is ordinary content and must still be
// creatable — the check is anchored at the tail for exactly this reason. The
// description here is the one from the ticket that asked for the check, tags
// and all.
func TestCreateAcceptsADescriptionThatDiscussesEnvelopes(t *testing.T) {
	session := testServer(t)
	description := "ROOT CAUSE IS NOT IN TK. Stating that first so nobody hunts a parsing defect that does not exist.\n\n" +
		"Observed three times in one session, across two different agents: a `ticket_create` call carrying both " +
		"`description` and `acceptance` produced a ticket whose `acceptance_criteria` was empty and whose description " +
		"ended with literal text of this shape: the real description text, then " + closeTag("antml:description") +
		", then " + openTag("antml:parameter", `name="acceptance"`) + " with the intended criteria, then " +
		closeTag("antml:parameter") + ", then " + closeTag("antml:invoke") + ".\n\n" +
		"The cause is client-side. The calling agent closed the `description` parameter with a wrong closing tag, so " +
		"the tool-call parser kept consuming and the entire remainder of the envelope was absorbed into the " +
		"`description` string value. tk stored precisely what it was handed.\n\n" +
		"Two independent signals were available and neither was used: a description whose tail is an unmistakable " +
		"truncated tool-call envelope, and a create call that set a description but left acceptance empty."

	id := createTicketID(t, session, map[string]any{
		"title":       "ticket_create silently accepts a malformed tool-call envelope",
		"description": description,
		"acceptance":  "The check rejects a fragment and this ticket still creates",
	})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: map[string]any{"id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	var shown map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &shown)
	if shown["description"] != description {
		t.Errorf("description round-tripped as %q", shown["description"])
	}
}

func TestCreateWarnsOnEmptyAcceptance(t *testing.T) {
	session := testServer(t)

	tests := []struct {
		name string
		args map[string]any
		warn bool
	}{
		{"description without acceptance", map[string]any{"title": "No criteria", "description": "Why it matters"}, true},
		{"description with acceptance", map[string]any{"title": "Both", "description": "Why it matters", "acceptance": "What done means"}, false},
		{"neither", map[string]any{"title": "Stub"}, false},
		{"epic with a description", map[string]any{"title": "Container", "type": "epic", "description": "Why it matters"}, false},
		{"whitespace-only acceptance", map[string]any{"title": "Blank criteria", "description": "Why it matters", "acceptance": "   "}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "ticket_create",
				Arguments: tt.args,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("ticket_create error: %v", result.Content)
			}
			var created map[string]any
			json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &created)
			warning, _ := created["empty_acceptance_warning"].(string)
			if tt.warn && warning == "" {
				t.Errorf("create with a description and no acceptance carried no warning: %v", created)
			}
			if !tt.warn && warning != "" {
				t.Errorf("unexpected warning %q", warning)
			}
			if tt.warn && !strings.Contains(warning, created["id"].(string)) {
				t.Errorf("warning does not name the ticket: %q", warning)
			}
		})
	}
}
