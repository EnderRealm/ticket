package cmd

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

func TestLsDefaultStatusSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TICKETS_DIR", dir)
	store := ticket.NewFileStore(dir)

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
	dir := t.TempDir()
	t.Setenv("TICKETS_DIR", dir)
	store := ticket.NewFileStore(dir)

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
