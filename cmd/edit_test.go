package cmd

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

// editStore creates a central-store project holding one open ticket on a
// branch.
func editStore(t *testing.T, id string) *ticket.FileStore {
	t.Helper()
	store := centralStore(t, "ed-edit")
	tk := &ticket.Ticket{
		ID:      id,
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Branch:  "feature-x",
		Created: time.Now(),
		Title:   "Editable ticket",
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store
}

// runEditWith runs the edit command against a fresh flag set, since flag
// values on the shared editCmd persist across calls.
func runEditWith(t *testing.T, id string, flags ...string) error {
	t.Helper()
	cmd := &cobra.Command{}
	registerEditFlags(cmd)
	f := cmd.Flags()
	for i := 0; i+1 < len(flags); i += 2 {
		if err := f.Set(flags[i], flags[i+1]); err != nil {
			t.Fatalf("set %s: %v", flags[i], err)
		}
	}
	return runEdit(cmd, []string{id})
}

func TestEditSetsAndRemovesOutputs(t *testing.T) {
	store := editStore(t, "ed-1234")

	if err := runEditWith(t, "ed-1234", "output", "branch=feature-x", "output", "commit=abc1234"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err := store.Get("ed-1234")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Outputs["branch"] != "feature-x" || tk.Outputs["commit"] != "abc1234" {
		t.Fatalf("Outputs = %v, want branch and commit set", tk.Outputs)
	}

	if err := runEditWith(t, "ed-1234", "output", "commit="); err != nil {
		t.Fatalf("runEdit remove: %v", err)
	}
	tk, err = store.Get("ed-1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tk.Outputs["commit"]; ok {
		t.Errorf("Outputs[commit] still present: %v", tk.Outputs)
	}
	if tk.Outputs["branch"] != "feature-x" {
		t.Errorf("Outputs[branch] = %q, want feature-x", tk.Outputs["branch"])
	}
}

func TestEditRejectsInvalidOutput(t *testing.T) {
	editStore(t, "ed-bad")

	if err := runEditWith(t, "ed-bad", "output", "noequals"); err == nil {
		t.Error("expected error for --output without key=value")
	}
	if err := runEditWith(t, "ed-bad", "output", "bad key=value"); err == nil {
		t.Error("expected error for invalid output key")
	}
	if err := runEditWith(t, "ed-bad", "output", "commit=has:colon"); err == nil {
		t.Error("expected error for invalid output value")
	}
}

func TestEditToDonePopulatesBranchOutput(t *testing.T) {
	store := editStore(t, "ed-done")

	if err := runEditWith(t, "ed-done", "status", "done"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err := store.Get("ed-done")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Outputs["branch"] != "feature-x" {
		t.Errorf("Outputs[branch] = %q, want feature-x", tk.Outputs["branch"])
	}
}

func TestEditToDoneKeepsExistingBranchOutput(t *testing.T) {
	store := editStore(t, "ed-keep")

	if err := runEditWith(t, "ed-keep", "output", "branch=manual-branch"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	if err := runEditWith(t, "ed-keep", "status", "done"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err := store.Get("ed-keep")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Outputs["branch"] != "manual-branch" {
		t.Errorf("Outputs[branch] = %q, want manual-branch", tk.Outputs["branch"])
	}
}

func TestEditPromotesToEpic(t *testing.T) {
	// `tk edit --type epic` sends the status the ticket was read with alongside
	// the new type. That status was never chosen for an epic, so promotion has
	// to stay one ordinary command rather than needing --status backlog with it.
	store := editStore(t, "ed-epic")

	if err := runEditWith(t, "ed-epic", "type", "epic"); err != nil {
		t.Fatalf("promoting a ticket to an epic should not be refused: %v", err)
	}
	tk, err := store.Get("ed-epic")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Type != ticket.TypeEpic {
		t.Errorf("type = %q, want %q", tk.Type, ticket.TypeEpic)
	}
	if tk.Status != ticket.StatusBacklog {
		t.Errorf("status = %q, want %q — a childless epic derives backlog", tk.Status, ticket.StatusBacklog)
	}
}

func TestEditPromotesToEpicAndCloses(t *testing.T) {
	// `tk edit --type epic --status closed` sets the status explicitly, so it is
	// the abandon it looks like: recorded, not dropped along with the promotion.
	store := editStore(t, "ed-promote-close")

	if err := runEditWith(t, "ed-promote-close", "type", "epic", "status", "closed"); err != nil {
		t.Fatalf("promoting a ticket and abandoning it should be accepted: %v", err)
	}
	tk, err := store.Get("ed-promote-close")
	if err != nil {
		t.Fatal(err)
	}
	if !tk.Abandoned {
		t.Error("a closed set with the promotion recorded no abandon intent")
	}
	if tk.Status != ticket.StatusClosed {
		t.Errorf("status = %q, want %q", tk.Status, ticket.StatusClosed)
	}
}

func TestEditPromotingToEpicRefusesAnotherStatus(t *testing.T) {
	store := editStore(t, "ed-promote-ready")

	err := runEditWith(t, "ed-promote-ready", "type", "epic", "status", "ready")
	if err == nil {
		t.Fatal("expected a status set alongside the promotion to be refused, got nil")
	}
	if !contains(err.Error(), "derived from its children") {
		t.Errorf("error should say an epic's status is derived, got: %v", err)
	}
	tk, getErr := store.Get("ed-promote-ready")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if tk.Type == ticket.TypeEpic {
		t.Error("the refused edit promoted the ticket anyway")
	}
}

func TestEditReportsTheChildrenAnAbandonClosed(t *testing.T) {
	// Closing an epic writes its children too, so the line reporting the edit
	// names them — the failure case already did, and success said nothing.
	store := centralStore(t, "ed-abandon")
	if err := store.Create(&ticket.Ticket{
		ID: "ed-epic-0001", Status: ticket.StatusBacklog, Type: ticket.TypeEpic,
		Created: time.Now(), Title: "An epic", Body: "\n",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(&ticket.Ticket{
		ID: "ed-child-0002", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Parent: "ed-epic-0001",
		Created: time.Now(), Title: "A child", Body: "\n",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	out := captureEdit(t, "ed-epic-0001", "status", "closed")
	if !contains(out, "ed-child-0002") || !contains(out, "closed 1 child ticket(s)") {
		t.Errorf("edit should report the children it closed:\n%s", out)
	}

	// An edit that closes nothing reports nothing extra.
	out = captureEdit(t, "ed-child-0002", "title", "Renamed")
	if contains(out, "child ticket(s)") {
		t.Errorf("an edit that closed nothing reported a cascade:\n%s", out)
	}
}

func captureEdit(t *testing.T, id string, flags ...string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runEditWith(t, id, flags...)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}

func TestShowRendersOutputs(t *testing.T) {
	store := editStore(t, "ed-show")

	if err := runEditWith(t, "ed-show", "output", "branch=feature-x", "output", "commit=abc1234"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	out := captureShow(t, store, "ed-show", false)
	for _, want := range []string{
		"outputs:\n  branch: feature-x\n  commit: abc1234\n",
		"## Outputs\n\n- branch: feature-x\n- commit: abc1234\n",
	} {
		if !contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}
