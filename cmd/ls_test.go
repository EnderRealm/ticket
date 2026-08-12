package cmd

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

func TestLsDefaultStatusSet(t *testing.T) {
	store := centralStore(t, "ls-status")

	// One ticket per status, using the status as a distinctive ID.
	statuses := []ticket.Status{
		ticket.StatusBacklog,
		ticket.StatusReady,
		ticket.StatusOpen,
		ticket.StatusDone,
		ticket.StatusClosed,
	}
	for _, s := range statuses {
		tk := &ticket.Ticket{
			ID:      "ls-" + string(s),
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

	out := captureLs(t)

	for _, s := range []ticket.Status{ticket.StatusBacklog, ticket.StatusReady, ticket.StatusOpen, ticket.StatusDone} {
		if !contains(out, "ls-"+string(s)) {
			t.Errorf("default ls output missing %s ticket:\n%s", s, out)
		}
	}
	if contains(out, "ls-"+string(ticket.StatusClosed)) {
		t.Errorf("default ls output should exclude closed ticket:\n%s", out)
	}
}

func TestLsBacklogOnlyNotEmpty(t *testing.T) {
	store := centralStore(t, "ls-backlog")

	tk := &ticket.Ticket{
		ID:      "ls-only-backlog",
		Status:  ticket.StatusBacklog,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Only backlog",
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	out := captureLs(t)

	if !contains(out, "ls-only-backlog") {
		t.Errorf("default ls with only a backlog ticket should list it, got:\n%s", out)
	}
}

func TestLsParentMatchesBareAndNamespacedParent(t *testing.T) {
	store := centralStore(t, "proj")

	// The central store records a child's parent namespaced; tickets written
	// before the namespacing rollout record it bare. Both are children.
	parents := map[string]string{
		"ls-child-bare": "ls-epic-0001",
		"ls-child-ns":   "proj/ls-epic-0001",
	}
	for _, id := range []string{"ls-epic-0001", "ls-child-bare", "ls-child-ns"} {
		typ := ticket.TypeFeature
		status := ticket.StatusOpen
		if id == "ls-epic-0001" {
			typ = ticket.TypeEpic
			status = ticket.StatusBacklog
		}
		tk := &ticket.Ticket{
			ID:      id,
			Status:  status,
			Type:    typ,
			Parent:  parents[id],
			Created: time.Now(),
			Title:   "Item " + id,
			Body:    "\n",
		}
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	f := lsCmd.Flags()
	if err := f.Set("parent", "ls-epic-0001"); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	defer func() { _ = f.Set("parent", "") }()

	out := captureLs(t)

	for _, id := range []string{"ls-child-bare", "ls-child-ns"} {
		if !contains(out, id) {
			t.Errorf("--parent output missing %s:\n%s", id, out)
		}
	}
}

func captureLs(t *testing.T) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runLs(lsCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runLs: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}
