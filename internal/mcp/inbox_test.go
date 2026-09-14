package mcp_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInboxBlocked(t *testing.T) {
	session, root := testCentralServerWithDefault(t, "alpha", "alpha", "beta")
	alpha := ticket.NewProjectFileStore(filepath.Join(root, "tickets", "alpha"), "alpha")
	beta := ticket.NewProjectFileStore(filepath.Join(root, "tickets", "beta"), "beta")
	for _, fixture := range []struct {
		store  *ticket.FileStore
		id     string
		status ticket.Status
		deps   []string
		q      string
	}{
		{beta, "dep-open", ticket.StatusOpen, nil, ""},
		{alpha, "dep-open", ticket.StatusDone, nil, ""},
		{alpha, "dep-closed", ticket.StatusClosed, nil, ""},
		{alpha, "item-ready", ticket.StatusReady, []string{"beta/dep-open", "dep-open", "dep-closed"}, ""},
		{alpha, "item-open", ticket.StatusOpen, []string{"beta/dep-open", "beta/missing"}, ""},
		{alpha, "item-missing", ticket.StatusReady, []string{"beta/missing"}, ""},
		{alpha, "item-work", ticket.StatusReady, []string{"dep-open", "dep-closed"}, ""},
		{alpha, "item-parked", ticket.StatusOpen, []string{"beta/dep-open"}, "Which store wins?"},
	} {
		if err := fixture.store.Create(&ticket.Ticket{
			ID: fixture.id, Status: fixture.status, Type: ticket.TypeFeature,
			Title: fixture.id, Created: time.Now(), Deps: fixture.deps,
			Extra: map[string]string{ticket.QuestionField: fixture.q},
		}); err != nil {
			t.Fatal(err)
		}
	}
	result := callTool(t, session, "ticket_inbox", map[string]any{"project": "alpha"})
	if result.IsError {
		t.Fatalf("inbox error: %v", result.Content)
	}
	var items []struct {
		Ticket struct {
			ID     string        `json:"id"`
			Status ticket.Status `json:"status"`
		} `json:"ticket"`
		Action string `json:"action"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &items); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		action string
		detail string
		status ticket.Status
	}{
		"alpha/item-ready":   {"blocked", "blocked on beta/dep-open", ticket.StatusReady},
		"alpha/item-open":    {"blocked", "blocked on beta/dep-open, beta/missing", ticket.StatusOpen},
		"alpha/item-missing": {"blocked", "blocked on beta/missing", ticket.StatusReady},
		"alpha/item-work":    {"work", "ready for work", ticket.StatusReady},
		"alpha/item-parked":  {"blocked", "Which store wins?", ticket.StatusOpen},
	}
	if len(items) != len(want) {
		t.Fatalf("inbox returned %d items, want %d: %+v", len(items), len(want), items)
	}
	actions := map[string]string{}
	for _, item := range items {
		w, ok := want[item.Ticket.ID]
		if !ok {
			t.Errorf("unexpected inbox ticket %s", item.Ticket.ID)
			continue
		}
		if item.Action != w.action || item.Detail != w.detail || item.Ticket.Status != w.status {
			t.Errorf("%s: got %+v, want %+v", item.Ticket.ID, item, w)
		}
		actions[item.Ticket.ID] = item.Action
		shown := callObject(t, session, "ticket_show", map[string]any{"id": item.Ticket.ID})
		if shown["status"] != string(w.status) {
			t.Errorf("inbox changed %s status to %v", item.Ticket.ID, shown["status"])
		}
	}
	blocked := callTool(t, session, "ticket_blocked", map[string]any{"project": "alpha"})
	if blocked.IsError {
		t.Fatalf("blocked error: %v", blocked.Content)
	}
	var tickets []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(blocked.Content[0].(*mcp.TextContent).Text), &tickets); err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 4 {
		t.Fatalf("blocked returned %d tickets, want 4", len(tickets))
	}
	for _, tk := range tickets {
		if actions[tk.ID] != "blocked" {
			t.Errorf("ticket_blocked lists %s, but inbox action is %q", tk.ID, actions[tk.ID])
		}
	}
}
