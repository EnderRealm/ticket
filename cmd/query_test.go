package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

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
