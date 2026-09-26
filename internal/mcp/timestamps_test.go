package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListAndShowCarryUpdatedAndClosed(t *testing.T) {
	session, dir := testServerDir(t)
	before := time.Now().UTC().Truncate(time.Second)

	idByStatus := map[string]string{}
	for _, status := range []string{"open", "done", "closed"} {
		id := createTicketID(t, session, map[string]any{"title": "Item " + status, "type": "feature"})
		editStatus(t, session, id, status)
		idByStatus[status] = id
	}
	// An abandoned childless epic is closed by the edit alone, with no child to
	// date it.
	epicID := createTicketID(t, session, map[string]any{"title": "Abandoned epic", "type": "epic"})
	editStatus(t, session, epicID, "closed")
	// A ticket last written before the fields existed carries neither date.
	legacy := "---\nid: lg-0001\nstatus: done\ndeps: []\nlinks: []\ncreated: 2024-01-01T00:00:00Z\ntype: feature\npriority: 2\n---\n# Legacy\n"
	if err := os.WriteFile(filepath.Join(dir, "lg-0001.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	// A note on a legacy done ticket is a write, but not its finish.
	noted := strings.Replace(legacy, "lg-0001", "lg-0002", 1)
	if err := os.WriteFile(filepath.Join(dir, "lg-0002.md"), []byte(noted), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_add_note",
		Arguments: map[string]any{"id": "lg-0002", "text": "late note"},
	}); err != nil || r.IsError {
		t.Fatalf("ticket_add_note: %v %v", err, r)
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_list",
		Arguments: map[string]any{"include_closed": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_list error: %v", result.Content)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp); err != nil {
		t.Fatal(err)
	}
	rows := map[string]map[string]any{}
	for _, tk := range resp["tickets"].([]any) {
		row := tk.(map[string]any)
		rows[row["id"].(string)] = row
	}

	shown := map[string]map[string]any{}
	for _, id := range []string{idByStatus["open"], idByStatus["done"], idByStatus["closed"], epicID, "lg-0001", "lg-0002"} {
		shown[id] = showResult(t, session, map[string]any{"id": id})
	}

	for name, out := range map[string]map[string]map[string]any{"ticket_list": rows, "ticket_show": shown} {
		for status, id := range idByStatus {
			if updated := stampAt(t, out[id], "updated"); updated.Before(before) {
				t.Errorf("%s %s updated = %v, want at or after %v", name, status, updated, before)
			}
		}
		for _, status := range []string{"done", "closed"} {
			if closed := stampAt(t, out[idByStatus[status]], "closed"); closed.Before(before) {
				t.Errorf("%s %s closed = %v, want at or after %v", name, status, closed, before)
			}
		}
		if out[epicID]["status"] != "closed" {
			t.Errorf("%s abandoned epic status = %v, want closed", name, out[epicID]["status"])
		}
		if closed := stampAt(t, out[epicID], "closed"); closed.Before(before) {
			t.Errorf("%s abandoned epic closed = %v, want at or after %v", name, closed, before)
		}
		if v, ok := out[idByStatus["open"]]["closed"]; ok {
			t.Errorf("%s open ticket carries closed = %v, want absent", name, v)
		}
		for _, key := range []string{"updated", "closed"} {
			if v, ok := out["lg-0001"][key]; ok {
				t.Errorf("%s legacy ticket carries %s = %v, want absent", name, key, v)
			}
		}
		if updated := stampAt(t, out["lg-0002"], "updated"); updated.Before(before) {
			t.Errorf("%s noted legacy ticket updated = %v, want the note at or after %v", name, updated, before)
		}
		if v, ok := out["lg-0002"]["closed"]; ok {
			t.Errorf("%s noted legacy ticket carries closed = %v, want absent", name, v)
		}
	}
}

func stampAt(t *testing.T, row map[string]any, key string) time.Time {
	t.Helper()
	s, ok := row[key].(string)
	if !ok {
		t.Fatalf("%v: %s missing or not a string: %v", row["id"], key, row[key])
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("%v: %s = %q is not RFC 3339: %v", row["id"], key, s, err)
	}
	if !strings.HasSuffix(s, "Z") {
		t.Errorf("%v: %s = %q is not UTC", row["id"], key, s)
	}
	return ts
}
