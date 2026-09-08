package cmd

import (
	"io"
	"os"
	"testing"
)

// The CLI has no acceptance flag: a description carrying an `## Acceptance
// Criteria` section is how this path produces criteria, and the warning has to
// reach the operator on the same terms ticket_create reports it.
func TestCreateWarnsOnBareAcceptanceCriteria(t *testing.T) {
	store := centralStore(t, "cr-bare")

	description := "Why it matters.\n\n## Acceptance Criteria\n\n- Checked.\n  verify: go test ./...\n- Bare one.\n"
	f := createCmd.Flags()
	if err := f.Set("description", description); err != nil {
		t.Fatalf("set description: %v", err)
	}
	defer func() { _ = f.Set("description", "") }()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = devNull, errW
	// Restored on cleanup: a panic in runCreate would otherwise leave the
	// package's stderr on a closed pipe and swallow every later test's output.
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		errR.Close()
	})

	createErr := runCreate(createCmd, []string{"Bare criteria ticket"})

	errW.Close()
	devNull.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	warning, _ := io.ReadAll(errR)

	if createErr != nil {
		t.Fatalf("runCreate: %v", createErr)
	}

	tickets, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("store holds %d tickets, want the created one — the warning must not refuse the create", len(tickets))
	}

	for _, want := range []string{tickets[0].ID, "Bare one.", "verify:", "unverifiable:"} {
		if !contains(string(warning), want) {
			t.Errorf("warning = %q, want to contain %q", string(warning), want)
		}
	}
	if contains(string(warning), "Checked.") {
		t.Errorf("warning names a criterion that carries a verify command: %q", string(warning))
	}
}
