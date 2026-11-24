package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func TestTestingStatusIsValid(t *testing.T) {
	store := editStore(t, "ed-testing")
	if err := runEditWith(t, "ed-testing", "status", "testing"); err != nil {
		t.Fatalf("edit --status testing: %v", err)
	}
	tk, err := store.Get("ed-testing")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != ticket.StatusTesting || !tk.ClosedAt().IsZero() {
		t.Errorf("status = %s, closed = %v, want testing with no closed date", tk.Status, tk.ClosedAt())
	}
	if err := runEditWith(t, "ed-testing", "status", "needs_testing"); err == nil || !strings.Contains(err.Error(), "testing") {
		t.Errorf("edit --status needs_testing = %v, want it refused naming the statuses", err)
	}

	legacy := "---\nid: ed-legacy\nstatus: needs_testing\ndeps: []\nlinks: []\ntype: feature\npriority: 2\n---\n# Legacy\n"
	if err := os.WriteFile(filepath.Join(store.Dir, "ed-legacy.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := captureShow(t, store, "ed-legacy", false); !contains(out, "status: open") {
		t.Errorf("stored needs_testing does not read open:\n%s", out)
	}

	mkCentral(t, store, "ed-done", ticket.TypeFeature, ticket.StatusDone, "")
	if err := lsCmd.Flags().Set("all", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lsCmd.Flags().Set("all", "false") }()
	out := captureLs(t)
	openAt, testingAt, doneAt := strings.Index(out, "=== open ==="), strings.Index(out, "=== testing ==="), strings.Index(out, "=== done ===")
	if openAt < 0 || testingAt < openAt || doneAt < testingAt {
		t.Errorf("ls groups open, testing, done at %d, %d, %d, want that order:\n%s", openAt, testingAt, doneAt, out)
	}
}

// A testing ticket is live work, and a dep it was held by before it landed no
// longer moves it to the Blocked group.
func TestLsListsTestingOutsideBlocked(t *testing.T) {
	store := centralStore(t, "ls-testing")
	mkCentral(t, store, "lt-dep", ticket.TypeFeature, ticket.StatusOpen, "")
	if err := store.Create(&ticket.Ticket{
		ID: "lt-landed", Status: ticket.StatusTesting, Type: ticket.TypeFeature,
		Deps: []string{"lt-dep"}, Created: time.Now(), Title: "Landed", Body: "\n",
	}); err != nil {
		t.Fatal(err)
	}
	out := captureLs(t)
	if !contains(out, "=== testing ===") || !contains(out, "lt-landed") || contains(out, "=== Blocked ===") {
		t.Errorf("ls does not list the testing ticket in its own group:\n%s", out)
	}
}

func TestTestingDepSatisfiesDependents(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	frontierTicket(t, stores["alpha"], "fr-landed", ticket.StatusTesting)
	frontierTicket(t, stores["alpha"], "fr-waiter", ticket.StatusReady, "fr-landed")

	if out := captureFrontier(t); !contains(out, "fr-waiter") || contains(out, "fr-landed") {
		t.Errorf("frontier should list the dependent and not the testing ticket:\n%s", out)
	}
	if out := captureShow(t, stores["alpha"], "fr-waiter", false); contains(out, "## Blockers") {
		t.Errorf("show lists a testing dep as a blocker:\n%s", out)
	}
	if out := captureShow(t, stores["alpha"], "fr-landed", false); contains(out, "## Blocking") {
		t.Errorf("show lists a testing ticket as blocking:\n%s", out)
	}
}

func TestEpicWithTestingChildDerivesOpen(t *testing.T) {
	store := centralStore(t, "sh-testing")
	mkCentral(t, store, "epic-t", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, store, "t-done", ticket.TypeFeature, ticket.StatusDone, "epic-t")
	mkCentral(t, store, "t-landed", ticket.TypeFeature, ticket.StatusTesting, "epic-t")

	out := captureShow(t, store, "epic-t", false)
	for _, want := range []string{"status: open", "children: 2 — done 1, closed 0, open 0, testing 1, ready 0, backlog 0"} {
		if !contains(out, want) {
			t.Errorf("show missing %q:\n%s", want, out)
		}
	}
}
