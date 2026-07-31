package cmd

import (
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

// editStore creates a temp store holding one open ticket on a branch.
func editStore(t *testing.T, id string) *ticket.FileStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TICKETS_DIR", dir)

	store := ticket.NewFileStore(dir)
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
