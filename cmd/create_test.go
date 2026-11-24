package cmd

import (
	"io"
	"os"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// captureCreateStderr creates a ticket from a description and returns its ID
// and what the create printed on stderr.
func captureCreateStderr(t *testing.T, store *ticket.FileStore, title, description string) (string, string) {
	t.Helper()
	f := createCmd.Flags()
	if err := f.Set("description", description); err != nil {
		t.Fatalf("set description: %v", err)
	}
	defer func() { _ = f.Set("description", "") }()

	before := map[string]bool{}
	tickets, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, tk := range tickets {
		before[tk.ID] = true
	}

	oldStdout, oldStderr := os.Stdout, os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	r, w, _ := os.Pipe()
	os.Stdout, os.Stderr = devNull, w
	// Restored on cleanup: a panic in runCreate would otherwise leave the
	// package's stderr on a closed pipe and swallow every later test's output.
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		r.Close()
	})

	createErr := runCreate(createCmd, []string{title})

	w.Close()
	devNull.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	out, _ := io.ReadAll(r)

	if createErr != nil {
		t.Fatalf("runCreate: %v", createErr)
	}
	tickets, err = store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, tk := range tickets {
		if !before[tk.ID] {
			return tk.ID, string(out)
		}
	}
	t.Fatalf("create stored no ticket")
	return "", ""
}

// A numbered list under `## Acceptance Criteria` parses to no criteria; without
// its own warning the create would look complete.
func TestCreateWarnsOnUnparsedAcceptance(t *testing.T) {
	store := centralStore(t, "cr-unparsed")

	description := "Why it matters.\n\n## Acceptance Criteria\n\n1. First.\n   verify: go test ./a\n2. Second.\n   verify: go test ./b\n"
	id, warning := captureCreateStderr(t, store, "Numbered criteria ticket", description)

	tickets, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("store holds %d tickets, want the created one — the warning must not refuse the create", len(tickets))
	}

	if want := ticket.UnparsedAcceptanceWarning(id); !contains(warning, want) {
		t.Errorf("stderr = %q, want to contain %q", warning, want)
	}
}

// Create regenerates the ID when a concurrent create claimed it between the
// check and the write. The ticket rendered afterwards has to be the one this
// create wrote, not the one that won the original ID.
func TestCreateShowsRegeneratedID(t *testing.T) {
	store := centralStore(t, "cr-regen")

	realCreate := createTicket
	t.Cleanup(func() { createTicket = realCreate })
	var original, settled string
	createTicket = func(s *ticket.FileStore, tk *ticket.Ticket) error {
		original = tk.ID
		planted := &ticket.Ticket{
			ID:       original,
			Status:   ticket.StatusBacklog,
			Type:     ticket.TypeFeature,
			Priority: 2,
			Deps:     []string{},
			Links:    []string{},
			Created:  tk.Created,
			Title:    "Planted rival ticket",
			Body:     "\n",
		}
		if err := realCreate(s, planted); err != nil {
			return err
		}
		for tk.ID == original {
			tk.ID = ticket.GenerateID(tk.Title)
		}
		settled = tk.ID
		return realCreate(s, tk)
	}

	oldStdout := os.Stdout
	outR, outW, _ := os.Pipe()
	os.Stdout = outW
	t.Cleanup(func() {
		os.Stdout = oldStdout
		outR.Close()
	})

	createErr := runCreate(createCmd, []string{"Regenerated id ticket"})

	outW.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(outR)

	if createErr != nil {
		t.Fatalf("runCreate: %v", createErr)
	}
	if _, err := store.Get(settled); err != nil {
		t.Fatalf("Get(%s): %v", settled, err)
	}
	for _, want := range []string{settled, "Regenerated id ticket"} {
		if !contains(string(out), want) {
			t.Errorf("stdout = %q, want to contain %q", string(out), want)
		}
	}
	if contains(string(out), "Planted rival ticket") {
		t.Errorf("stdout renders the ticket holding the original ID %s, not the one written as %s: %q", original, settled, string(out))
	}
}
