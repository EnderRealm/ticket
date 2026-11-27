package mcp_test

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestApplyActionMarkAndUnmark(t *testing.T) {
	session := testServer(t)
	step := createTicketID(t, session, map[string]any{"title": "Install the build", "workflow_kind": "generic"})
	code := createTicketID(t, session, map[string]any{"title": "Code"})
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	tk := action(t, session, step, "mark-1", "mark", map[string]any{"mark": "failed"})
	tk = action(t, session, step, "mark-2", "mark", map[string]any{"mark": "done"})
	if tk["status"] != "done" || tk["closed"] == nil {
		t.Errorf("mark done = %v", tk)
	}
	last, _ := showTicket(t, session, step)["last_mark"].(map[string]any)
	if last["result"] != "done" || last["note"] != "note mark-2" || last["by"] != "test" || last["host"] != host || last["prior"] != "backlog" || last["at"] == nil {
		t.Errorf("ticket_show last_mark = %v", last)
	}

	tk = action(t, session, step, "undo", "unmark", nil)
	if last, _ := tk["last_mark"].(map[string]any); tk["status"] != "backlog" || tk["closed"] != nil || last["result"] != "failed" {
		t.Errorf("unmark = %v", tk)
	}

	tk = action(t, session, step, "undo-2", "unmark", nil)
	if tk["status"] != "backlog" || tk["last_mark"] != nil {
		t.Errorf("second unmark = %v", tk)
	}

	for _, args := range []map[string]any{
		actionArgs(code, "x", "mark", map[string]any{"mark": "done"}),
		actionArgs(code, "x", "unmark", nil),
		actionArgs(step, "x", "unmark", nil),
	} {
		if text := errorText(t, applyAction(t, session, args), args["transition"].(string)); !strings.HasPrefix(text, "transition not permitted: ") {
			t.Errorf("%v refused with %q", args, text)
		}
	}
	for _, args := range []map[string]any{
		actionArgs(step, "x", "mark", nil),
		actionArgs(step, "x", "mark", map[string]any{"mark": "later"}),
		actionArgs(step, "x", "mark", map[string]any{"mark": "done", "note": ""}),
		actionArgs(step, "x", "reopen", map[string]any{"mark": "done"}),
	} {
		if text := errorText(t, applyAction(t, session, args), "a malformed request"); strings.HasPrefix(text, "transition not permitted: ") {
			t.Errorf("%v refused with %q", args, text)
		}
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "ticket_apply_action" {
			continue
		}
		props := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		transition := props["transition"].(map[string]any)["description"].(string)
		for _, name := range []string{"mark", "unmark"} {
			if !strings.Contains(transition, name+" (") || !strings.Contains(tool.Description, "`"+name+"`") {
				t.Errorf("ticket_apply_action does not describe %s", name)
			}
		}
		if d, _ := props["mark"].(map[string]any)["description"].(string); d == "" {
			t.Error("mark has no description")
		}
		return
	}
	t.Error("ticket_apply_action is not registered")
}
