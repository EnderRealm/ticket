package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bodyFields is the fixed set of body keys tk query and ticket_show carry.
var bodyFields = []string{"description", "design", "acceptance_criteria", "test_results", "notes"}

const fullBody = "Why it matters.\n\n## Design\n\nThe approach.\n\n## Acceptance Criteria\n\n- It works\n  verify: go test ./...\n\n## Test Results\n\nAll green.\n"

func TestQueryCarriesUpdatedAndClosed(t *testing.T) {
	store := centralStore(t, "q-times")
	before := time.Now().UTC().Truncate(time.Second)

	for _, s := range []ticket.Status{ticket.StatusOpen, ticket.StatusDone, ticket.StatusClosed} {
		tk := &ticket.Ticket{
			ID:      "q-" + string(s),
			Status:  s,
			Type:    ticket.TypeFeature,
			Created: time.Now(),
			Title:   "Item " + string(s),
			Body:    "\n",
		}
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", s, err)
		}
	}
	epic := &ticket.Ticket{ID: "q-epic", Status: ticket.StatusBacklog, Type: ticket.TypeEpic, Created: time.Now(), Title: "Epic", Body: "\n"}
	if err := store.Create(epic); err != nil {
		t.Fatalf("Create epic: %v", err)
	}
	child := &ticket.Ticket{ID: "q-child", Status: ticket.StatusDone, Type: ticket.TypeFeature, Parent: "q-epic", Created: time.Now(), Title: "Child", Body: "\n"}
	if err := store.Create(child); err != nil {
		t.Fatalf("Create child: %v", err)
	}
	// Abandoning a childless epic closes it with no child to date the close.
	abandoned := &ticket.Ticket{ID: "q-abandon", Status: ticket.StatusBacklog, Type: ticket.TypeEpic, Created: time.Now(), Title: "Abandoned", Body: "\n"}
	if err := store.Create(abandoned); err != nil {
		t.Fatalf("Create abandoned epic: %v", err)
	}
	abandoned.Status = ticket.StatusClosed
	if _, err := ticket.SaveEdit(store, abandoned, true); err != nil {
		t.Fatalf("abandon epic: %v", err)
	}

	// Files planted by hand rather than written by tk: a legacy done ticket
	// carrying neither date, an epic abandoned before the abandon was dated, and
	// live tickets carrying a hand-edited completed date or an unknown `closed`
	// key.
	files := map[string]string{
		"q-legacyepic.md": "---\nid: q-legacyepic\nstatus: closed\nabandoned: true\ndeps: []\nlinks: []\ncreated: 2024-01-01T00:00:00Z\nupdated: 2024-01-02T00:00:00Z\ntype: epic\npriority: 2\n---\n# Legacy abandon\n",
		"q-legacy.md":     "---\nid: q-legacy\nstatus: done\ndeps: []\nlinks: []\ncreated: 2024-01-01T00:00:00Z\ntype: feature\npriority: 2\n---\n# Legacy\n",
		"q-handdone.md":   "---\nid: q-handdone\nstatus: open\ndeps: []\nlinks: []\ncreated: 2024-01-01T00:00:00Z\nupdated: 2024-01-02T00:00:00Z\ncompleted: 2024-01-03T00:00:00Z\ntype: feature\npriority: 2\n---\n# Hand edited\n",
		"q-extra.md":      "---\nid: q-extra\nstatus: open\ndeps: []\nlinks: []\ncreated: 2024-01-01T00:00:00Z\nclosed: 2024-01-03T00:00:00Z\ntype: feature\npriority: 2\n---\n# Extra key\n",
		"q-legacyedit.md": "---\nid: q-legacyedit\nstatus: done\ndeps: []\nlinks: []\ncreated: 2024-01-01T00:00:00Z\ntype: feature\npriority: 2\n---\n# Legacy retitled\n",
		"q-dated.md":      "---\nid: q-dated\nstatus: done\ndeps: []\nlinks: []\ncreated: 2024-01-01T00:00:00Z\ncompleted: 2024-01-03T00:00:00Z\ntype: feature\npriority: 2\n---\n# Dated retitled\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(store.Dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A write that leaves a finished ticket finished is not its finish: the
	// retitle keeps whatever completed date the file held, none included.
	for _, id := range []string{"q-legacyedit", "q-dated"} {
		tk, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		tk.Title = "Retitled"
		if err := store.Update(tk); err != nil {
			t.Fatalf("retitle %s: %v", id, err)
		}
	}

	rows := captureQuery(t)

	for _, id := range []string{"q-open", "q-done", "q-closed", "q-epic", "q-child", "q-abandon"} {
		updated := parseStamp(t, rows[id], "updated")
		if updated.Before(before) {
			t.Errorf("%s updated = %v, want a time at or after %v", id, updated, before)
		}
	}
	for _, id := range []string{"q-done", "q-closed", "q-child", "q-abandon"} {
		if closed := parseStamp(t, rows[id], "closed"); closed.Before(before) {
			t.Errorf("%s closed = %v, want a time at or after %v", id, closed, before)
		}
	}
	for id, want := range map[string]string{"q-epic": "done", "q-abandon": "closed", "q-legacyepic": "closed"} {
		if rows[id]["status"] != want {
			t.Errorf("%s status = %v, want %s", id, rows[id]["status"], want)
		}
	}
	if got, want := rows["q-epic"]["closed"], rows["q-child"]["closed"]; got != want {
		t.Errorf("q-epic closed = %v, want its child's %v", got, want)
	}

	for id, absent := range map[string][]string{
		"q-open":     {"closed"},
		"q-legacy":   {"updated", "closed"},
		"q-handdone": {"closed"},
		"q-extra":    {"updated", "closed"},
		// An abandon with no recorded date is unknown, not dated by a child or
		// the file's last write.
		"q-legacyepic": {"closed"},
		"q-legacyedit": {"closed"},
	} {
		for _, key := range absent {
			if v, ok := rows[id][key]; ok {
				t.Errorf("%s carries %s = %v, want the key absent", id, key, v)
			}
		}
	}
	if updated := parseStamp(t, rows["q-legacyedit"], "updated"); updated.Before(before) {
		t.Errorf("q-legacyedit updated = %v, want the retitle at or after %v", updated, before)
	}
	if got := rows["q-dated"]["closed"]; got != "2024-01-03T00:00:00Z" {
		t.Errorf("q-dated closed = %v after a retitle, want the stored 2024-01-03T00:00:00Z", got)
	}
	if got := rows["q-handdone"]["updated"]; got != "2024-01-02T00:00:00Z" {
		t.Errorf("q-handdone updated = %v, want the stored 2024-01-02T00:00:00Z", got)
	}
}

func TestQueryCarriesBodyFields(t *testing.T) {
	store := centralStore(t, "q-body")

	noted := time.Date(2026, 11, 1, 8, 20, 32, 0, time.UTC)
	const posing = "Real text.\n\n---\nid: q-posing\nstatus: open\ntype: bug\n---\n# Posing ticket\n\nTail."
	for id, tk := range map[string]*ticket.Ticket{
		"q-full": {Body: fullBody, Notes: []ticket.Note{
			{Timestamp: noted, Text: "First note."},
			{Timestamp: noted.Add(time.Hour), Text: "Second note."},
		}},
		"q-frontmatter": {Body: posing + "\n"},
		"q-heading":     {Body: "Intro.\n\n## Background\n\nContext.\n\n## Design\n\nThe approach.\n\n## Rollout\n\nIn stages.\n"},
		"q-emptydesign": {Body: "Only this.\n\n## Design\n\n## Acceptance Criteria\n\n- It works\n"},
		"q-nobody":      {Body: "\n"},
	} {
		tk.ID, tk.Status, tk.Type, tk.Created, tk.Title = id, ticket.StatusOpen, ticket.TypeFeature, time.Now(), "Item "+id
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	rows := captureQuery(t)

	if len(rows) != 5 {
		t.Errorf("query returned %d rows, want 5: %v", len(rows), rows)
	}

	wantNotes := []any{
		map[string]any{"timestamp": "2026-11-01T08:20:32Z", "text": "First note."},
		map[string]any{"timestamp": "2026-11-01T09:20:32Z", "text": "Second note."},
	}
	for id, want := range map[string]map[string]any{
		"q-full": {
			"description":         "Why it matters.",
			"design":              "The approach.",
			"acceptance_criteria": "- It works\n  verify: go test ./...",
			"test_results":        "All green.",
			"notes":               wantNotes,
		},
		// Text that looks like the next ticket's frontmatter is this ticket's
		// description, not a second row.
		"q-frontmatter": {"description": posing},
		// A heading tk does not name is not a field; it stays in the section
		// it follows.
		"q-heading": {
			"description": "Intro.\n\n## Background\n\nContext.",
			"design":      "The approach.\n\n## Rollout\n\nIn stages.",
		},
		"q-emptydesign": {"description": "Only this.", "acceptance_criteria": "- It works"},
		"q-nobody":      {},
	} {
		for _, key := range bodyFields {
			got, ok := rows[id][key]
			wantValue, wantOK := want[key]
			if ok != wantOK || !reflect.DeepEqual(got, wantValue) {
				t.Errorf("%s %s = %#v (present %v), want %#v (present %v)", id, key, got, ok, wantValue, wantOK)
			}
		}
	}
	for _, key := range []string{"background", "rollout"} {
		if v, ok := rows["q-heading"][key]; ok {
			t.Errorf("q-heading carries %s = %v, want no field for the heading", key, v)
		}
	}
}

// TestQueryAndShowAgreeOnBodyFields holds a `tk query --all-projects` row to the
// ticket_show response for the same ticket: same keys present, same values. More
// notes than ticket_show's default window, which notes_limit=0 opens, and a
// heading after the acceptance section, whose text neither surface carries.
func TestQueryAndShowAgreeOnBodyFields(t *testing.T) {
	store := centralStore(t, "q-agree")

	var notes []ticket.Note
	for i := range 25 {
		notes = append(notes, ticket.Note{
			Timestamp: time.Date(2026, 11, 1, 8, i, 0, 0, time.UTC),
			Text:      fmt.Sprintf("Note %d.", i),
		})
	}
	const afterAcceptance = "Past the acceptance section."
	for id, tk := range map[string]*ticket.Ticket{
		"q-full":    {Body: fullBody, Notes: notes},
		"q-heading": {Body: "Intro.\n\n## Background\n\nContext.\n\n## Acceptance Criteria\n\n- It works\n\n## Rollout\n\n" + afterAcceptance + "\n"},
		"q-nobody":  {Body: "\n"},
	} {
		tk.ID, tk.Status, tk.Type, tk.Created, tk.Title = id, ticket.StatusOpen, ticket.TypeFeature, time.Now(), "Item "+id
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	if err := queryCmd.Flags().Set("all-projects", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queryCmd.Flags().Set("all-projects", "false") })
	rows := captureQuery(t)

	served, root, err := serveStore()
	if err != nil {
		t.Fatalf("serveStore: %v", err)
	}
	session := serveSession(t, served, "q-agree", root)

	for _, id := range []string{"q-agree/q-full", "q-agree/q-heading", "q-agree/q-nobody"} {
		row, ok := rows[id]
		if !ok {
			t.Fatalf("query has no row %s: %v", id, rows)
		}
		shown := callTool(t, session, "ticket_show", map[string]any{"id": id, "notes_limit": 0})
		for _, key := range bodyFields {
			got, ok := row[key]
			want, wantOK := shown[key]
			if ok != wantOK || !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: query %#v (present %v), ticket_show %#v (present %v)", id, key, got, ok, want, wantOK)
			}
		}
	}
	if got := len(rows["q-agree/q-full"]["notes"].([]any)); got != 25 {
		t.Errorf("q-full carries %d notes, want all 25", got)
	}
	if line, _ := json.Marshal(rows["q-agree/q-heading"]); strings.Contains(string(line), afterAcceptance) {
		t.Errorf("q-heading carries the text past the acceptance section: %s", line)
	}

	listed := callTool(t, session, "ticket_list", map[string]any{"project": "q-agree"})
	listRows := listed["tickets"].([]any)
	if len(listRows) != 3 {
		t.Fatalf("ticket_list returned %d rows, want 3: %v", len(listRows), listed)
	}
	for _, r := range listRows {
		for _, key := range bodyFields {
			if v, ok := r.(map[string]any)[key]; ok {
				t.Errorf("ticket_list row %v carries %s = %v, want metadata only", r.(map[string]any)["id"], key, v)
			}
		}
	}
}

// The body fields belong to query: frontier --json shares ticketJSON but its
// rows stay metadata.
func TestFrontierJSONCarriesNoBody(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	tk := &ticket.Ticket{
		ID:      "fr-body",
		Status:  ticket.StatusReady,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Item",
		Body:    fullBody,
		Notes:   []ticket.Note{{Timestamp: time.Now(), Text: "A note."}},
	}
	if err := stores["alpha"].Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var rows []map[string]any
	out := captureFrontier(t)
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("frontier --json returned %d rows, want 1: %s", len(rows), out)
	}
	for _, key := range bodyFields {
		if v, ok := rows[0][key]; ok {
			t.Errorf("frontier row carries %s = %v, want metadata only", key, v)
		}
	}
}

func callTool(t *testing.T, session *gomcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*gomcp.TextContent).Text
	if result.IsError {
		t.Fatalf("%s returned error: %s", name, text)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("%s: invalid JSON response: %v", name, err)
	}
	return decoded
}

func captureQuery(t *testing.T) map[string]map[string]any {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runQuery(queryCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runQuery: %v", err)
	}

	out, _ := io.ReadAll(r)
	rows := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("query line %q: %v", line, err)
		}
		rows[row["id"].(string)] = row
	}
	return rows
}

func parseStamp(t *testing.T, row map[string]any, key string) time.Time {
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
