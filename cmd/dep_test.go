package cmd

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

// depStore creates a central-store project holding two ready tickets.
func depStore(t *testing.T, ids ...string) *ticket.FileStore {
	t.Helper()
	store := centralStore(t, "dp-dep")
	for _, id := range ids {
		tk := &ticket.Ticket{
			ID:      id,
			Status:  ticket.StatusReady,
			Type:    ticket.TypeFeature,
			Created: time.Now(),
			Title:   "Ticket " + id,
			Body:    "\n",
		}
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	return store
}

// runDepWith runs the dep command against a fresh flag set, since flag values
// on the shared depCmd persist across calls. setCargo is distinct from a
// non-empty cargo: the command keys on Changed, so --cargo "" clears.
func runDepWith(t *testing.T, id, depID, cargo string, setCargo bool) error {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("cargo", "", "")
	if setCargo {
		if err := cmd.Flags().Set("cargo", cargo); err != nil {
			t.Fatalf("set cargo: %v", err)
		}
	}
	return runDep(cmd, []string{id, depID})
}

func TestDepStoresCargo(t *testing.T) {
	store := depStore(t, "dp-1111", "dp-2222")

	if err := runDepWith(t, "dp-1111", "dp-2222", "event schema: the ingest table", true); err != nil {
		t.Fatalf("runDep: %v", err)
	}
	tk, err := store.Get("dp-1111")
	if err != nil {
		t.Fatal(err)
	}
	if got := ticket.CargoFor(tk, "dp-2222"); got != "event schema: the ingest table" {
		t.Fatalf("CargoFor = %q, want the cargo", got)
	}

	// Annotating an already-present dep still records the cargo.
	if err := runDepWith(t, "dp-1111", "dp-2222", "migration doc", true); err != nil {
		t.Fatalf("runDep re-annotate: %v", err)
	}
	tk, err = store.Get("dp-1111")
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.Deps) != 1 {
		t.Errorf("Deps = %v, want a single dep", tk.Deps)
	}
	if got := ticket.CargoFor(tk, "dp-2222"); got != "migration doc" {
		t.Errorf("CargoFor = %q, want migration doc", got)
	}
}

func TestDepRejectsInvalidCargo(t *testing.T) {
	depStore(t, "dp-3333", "dp-4444")

	if err := runDepWith(t, "dp-3333", "dp-4444", "two\nlines", true); err == nil {
		t.Error("expected error for cargo containing a newline")
	}
}

func TestDepWithoutCargoUnchanged(t *testing.T) {
	store := depStore(t, "dp-5555", "dp-6666")

	if err := runDepWith(t, "dp-5555", "dp-6666", "", false); err != nil {
		t.Fatalf("runDep: %v", err)
	}
	tk, err := store.Get("dp-5555")
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.Deps) != 1 || tk.Deps[0] != "dp-6666" {
		t.Errorf("Deps = %v, want [dp-6666]", tk.Deps)
	}
	if len(tk.DepCargo) != 0 {
		t.Errorf("DepCargo = %v, want empty", tk.DepCargo)
	}
}

func TestDepClearsCargo(t *testing.T) {
	store := depStore(t, "dp-cccc", "dp-dddd")

	if err := runDepWith(t, "dp-cccc", "dp-dddd", "event schema", true); err != nil {
		t.Fatalf("runDep: %v", err)
	}
	if err := runDepWith(t, "dp-cccc", "dp-dddd", "", true); err != nil {
		t.Fatalf("runDep clear: %v", err)
	}

	tk, err := store.Get("dp-cccc")
	if err != nil {
		t.Fatal(err)
	}
	if got := ticket.CargoFor(tk, "dp-dddd"); got != "" {
		t.Errorf("CargoFor = %q, want cleared", got)
	}
	if len(tk.Deps) != 1 || tk.Deps[0] != "dp-dddd" {
		t.Errorf("Deps = %v, want the dep retained", tk.Deps)
	}
}

func TestDepTreeRendersCargo(t *testing.T) {
	depStore(t, "dp-7777", "dp-8888", "dp-9999")

	if err := runDepWith(t, "dp-7777", "dp-8888", "event schema", true); err != nil {
		t.Fatalf("runDep: %v", err)
	}
	if err := runDepWith(t, "dp-7777", "dp-9999", "", false); err != nil {
		t.Fatalf("runDep: %v", err)
	}

	out := captureDepTree(t, "dp-7777")
	for _, want := range []string{
		"dp-8888 [ready] Ticket dp-8888  ← carries: event schema\n",
		"dp-9999 [ready] Ticket dp-9999  ← no cargo\n",
		"dp-7777 [ready] Ticket dp-7777\n",
	} {
		if !contains(out, want) {
			t.Errorf("dep tree output missing %q:\n%s", want, out)
		}
	}
}

func TestShowRendersDepCargo(t *testing.T) {
	store := depStore(t, "dp-aaaa", "dp-bbbb")

	if err := runDepWith(t, "dp-aaaa", "dp-bbbb", "event schema: the ingest table", true); err != nil {
		t.Fatalf("runDep: %v", err)
	}

	out := captureShow(t, store, "dp-aaaa", false)
	for _, want := range []string{
		"dep-cargo:\n  dp-bbbb: 'event schema: the ingest table'\n",
		"## Dep Cargo\n\n- dp-bbbb: event schema: the ingest table\n",
	} {
		if !contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func captureDepTree(t *testing.T, id string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := &cobra.Command{}
	cmd.Flags().Bool("full", false, "")
	err := runDepTree(cmd, []string{id})

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runDepTree: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}
