package mcp_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A waiting_on parks a ticket whatever its status: inbox reports it blocked on
// the value, ready and frontier drop it, and clearing the field alone restores
// it. A question still wins the inbox detail.
func TestWaitingOnLifecycle(t *testing.T) {
	session := testServer(t)
	id := callObject(t, session, "ticket_create", map[string]any{"title": "Waits upstream", "type": "feature"})["id"].(string)
	for _, step := range []struct {
		name     string
		status   string
		set      map[string]any
		action   string
		detail   string
		ready    bool
		frontier bool
	}{
		{"backlog absent", "backlog", nil, "", "", true, false},
		{"backlog waiting", "backlog", map[string]any{ticket.WaitingOnField: "  observer/q-1  "}, "blocked", "observer/q-1", false, false},
		{"backlog blank", "backlog", map[string]any{ticket.WaitingOnField: "   "}, "", "", true, false},
		{"ready waiting", "ready", map[string]any{ticket.WaitingOnField: "observer/q-1"}, "blocked", "observer/q-1", false, false},
		{"ready cleared", "ready", map[string]any{ticket.WaitingOnField: ""}, "work", "ready for work", true, true},
		{"open waiting", "open", map[string]any{ticket.WaitingOnField: "observer/q-2"}, "blocked", "observer/q-2", false, false},
		{"open question wins", "open", map[string]any{ticket.QuestionField: "Which store wins?"}, "blocked", "Which store wins?", false, false},
		{"open question cleared", "open", map[string]any{ticket.QuestionField: ""}, "blocked", "observer/q-2", false, false},
		{"open cleared", "open", map[string]any{ticket.WaitingOnField: ""}, "work", "in progress", true, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			args := map[string]any{"id": id, "status": step.status}
			if step.set != nil {
				args["set"] = step.set
			}
			callObject(t, session, "ticket_edit", args)

			result := callTool(t, session, "ticket_inbox", map[string]any{})
			if result.IsError {
				t.Fatalf("inbox error: %v", result.Content)
			}
			var items []struct {
				Action string `json:"action"`
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &items); err != nil {
				t.Fatal(err)
			}
			if step.action == "" {
				if len(items) != 0 {
					t.Errorf("inbox = %+v, want empty", items)
				}
			} else if len(items) != 1 || items[0].Action != step.action || items[0].Detail != step.detail {
				t.Errorf("inbox = %+v, want %s %q", items, step.action, step.detail)
			}

			if got := slices.Contains(readyIDs(t, session, map[string]any{}), id); got != step.ready {
				t.Errorf("in ready = %v, want %v", got, step.ready)
			}
			if got := slices.Contains(frontierIDs(t, session, map[string]any{}), id); got != step.frontier {
				t.Errorf("in frontier = %v, want %v", got, step.frontier)
			}
		})
	}
}
