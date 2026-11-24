package mcp_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// testingFixture is an epic holding a done child and a child in testing, and a
// ready ticket whose only dependency is the one in testing.
func testingFixture(t *testing.T) (call func(string, map[string]any) map[string]any, ids map[string]string, ready, frontier []string) {
	t.Helper()
	session := testServer(t)
	call = func(tool string, args map[string]any) map[string]any {
		t.Helper()
		return callObject(t, session, tool, args)
	}
	ids = map[string]string{"epic": createTicketID(t, session, map[string]any{"title": "Epic", "type": "epic"})}
	for _, name := range []string{"finished", "landed"} {
		ids[name] = createTicketID(t, session, map[string]any{"title": "Child " + name, "type": "feature", "parent": ids["epic"]})
	}
	ids["waiter"] = createTicketID(t, session, map[string]any{"title": "Waiter", "type": "feature"})
	callTool(t, session, "ticket_dep", map[string]any{"id": ids["waiter"], "action": "add", "dep_id": ids["landed"]})
	editStatus(t, session, ids["finished"], "done")
	editStatus(t, session, ids["landed"], "testing")
	editStatus(t, session, ids["waiter"], "ready")
	if deps, _ := call("ticket_show", map[string]any{"id": ids["waiter"]})["deps"].([]any); len(deps) != 1 {
		t.Fatalf("waiter deps = %v, want the testing ticket", deps)
	}
	return call, ids, readyIDs(t, session, map[string]any{}), frontierIDs(t, session, map[string]any{})
}

func TestTestingStatusIsValid(t *testing.T) {
	session, dir := testServerDir(t)
	open := createTicketID(t, session, map[string]any{"title": "Open item", "type": "feature"})
	id := createTicketID(t, session, map[string]any{"title": "Landed item", "type": "feature"})
	editStatus(t, session, open, "open")
	edited := callObject(t, session, "ticket_edit", map[string]any{"id": id, "status": "testing"})
	if edited["status"] != "testing" {
		t.Errorf("status after edit = %v, want testing", edited["status"])
	}
	if _, present := edited["closed"]; present {
		t.Errorf("a testing ticket carries closed: %v", edited["closed"])
	}
	if ids := listIDs(callObject(t, session, "ticket_list", map[string]any{"status": "testing"})); len(ids) != 1 || !ids[id] {
		t.Errorf("status=testing rows = %v, want %s", ids, id)
	}
	if ids := listIDs(callObject(t, session, "ticket_list", map[string]any{})); !ids[id] {
		t.Errorf("default rows = %v, want the testing ticket listed", ids)
	}
	if r := callTool(t, session, "ticket_edit", map[string]any{"id": id, "status": "needs_testing"}); !r.IsError {
		t.Error("ticket_edit accepted needs_testing")
	}

	legacy := "---\nid: legacy-0001\nstatus: needs_testing\ndeps: []\nlinks: []\ntype: feature\npriority: 2\n---\n# Legacy\n"
	if err := os.WriteFile(filepath.Join(dir, "legacy-0001.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := callObject(t, session, "ticket_show", map[string]any{"id": "legacy-0001"})["status"]; got != "open" {
		t.Errorf("stored needs_testing reads %v, want open", got)
	}
}

func TestTestingDepSatisfiesDependents(t *testing.T) {
	call, ids, ready, frontier := testingFixture(t)
	if !slices.Contains(ready, ids["waiter"]) {
		t.Errorf("ready = %v, want the dependent of the testing ticket", ready)
	}
	if !slices.Contains(frontier, ids["waiter"]) {
		t.Errorf("frontier = %v, want the dependent of the testing ticket", frontier)
	}
	if rows, _ := call("ticket_blocked", map[string]any{})["tickets"].([]any); len(rows) != 0 {
		t.Errorf("blocked = %v, want none", rows)
	}
}

func TestTestingTicketIsNotReady(t *testing.T) {
	_, ids, ready, frontier := testingFixture(t)
	if slices.Contains(ready, ids["landed"]) {
		t.Errorf("ready = %v lists a testing ticket", ready)
	}
	if slices.Contains(frontier, ids["landed"]) {
		t.Errorf("frontier = %v lists a testing ticket", frontier)
	}
}

func TestEpicWithTestingChildDerivesOpen(t *testing.T) {
	call, ids, _, _ := testingFixture(t)
	shown := call("ticket_show", map[string]any{"id": ids["epic"]})
	counts, _ := shown["counts"].(map[string]any)
	if shown["status"] != "open" || counts["testing"] != 1.0 || counts["done"] != 1.0 || counts["open"] != 0.0 {
		t.Errorf("show status = %v, counts = %v, want open with 1 testing and 1 done", shown["status"], counts)
	}
	parent, counts := parentOfResp(t, call("ticket_list", map[string]any{"parent": ids["epic"]}))
	if parent["status"] != "open" || counts["testing"] != 1.0 {
		t.Errorf("list parent = %v, counts = %v, want open with 1 testing", parent, counts)
	}
}

func TestInboxListsTestingAsTest(t *testing.T) {
	call, ids, _, _ := testingFixture(t)
	for _, row := range call("ticket_inbox", map[string]any{})["items"].([]any) {
		item := row.(map[string]any)
		if item["ticket"].(map[string]any)["id"] == ids["landed"] {
			if item["action"] != "test" || item["detail"] == "" {
				t.Errorf("inbox row = %v, want the action test with a detail", item)
			}
			return
		}
	}
	t.Error("inbox does not list the testing ticket")
}
