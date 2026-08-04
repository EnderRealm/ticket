package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// writeLegacyTicket writes a ticket file straight into a project's store
// directory, bypassing the write-path parent validation, to stand in for a
// store written before the one-level rule.
func writeLegacyTicket(t *testing.T, store *ticket.FileStore, tk *ticket.Ticket) {
	t.Helper()
	data, err := ticket.Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize %s: %v", tk.ID, err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, tk.ID+".md"), data, 0o644); err != nil {
		t.Fatalf("write %s: %v", tk.ID, err)
	}
}

func auditTicket(t *testing.T, store *ticket.FileStore, id string, typ ticket.TicketType, parent string) {
	t.Helper()
	writeLegacyTicket(t, store, &ticket.Ticket{
		ID: id, Status: ticket.StatusOpen, Type: typ, Parent: parent,
		Created: time.Now(), Title: "Item " + id, Body: "\n",
	})
}

func captureAudit(t *testing.T, args ...string) string {
	t.Helper()
	if err := auditCmd.Flags().Set("project", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(args); i += 2 {
		if err := auditCmd.Flags().Set(args[i], args[i+1]); err != nil {
			t.Fatal(err)
		}
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAudit(auditCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runAudit: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}

func TestAuditReportsViolations(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	store := stores["alpha"]
	auditTicket(t, store, "au-epic-0001", ticket.TypeEpic, "")
	auditTicket(t, store, "au-good-0002", ticket.TypeFeature, "au-epic-0001")
	auditTicket(t, store, "au-bad-0003", ticket.TypeFeature, "au-good-0002")

	out := captureAudit(t)

	if !contains(out, "au-bad-0003") || !contains(out, string(ticket.ViolationParentNotEpic)) {
		t.Errorf("audit output should report au-bad-0003 as parent-not-epic:\n%s", out)
	}
	if !contains(out, "1 ticket(s) violate") {
		t.Errorf("audit should report exactly the one violation, not the valid child:\n%s", out)
	}
}

func TestAuditJSONAndProjectFilter(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	auditTicket(t, stores["alpha"], "au-alpha-0001", ticket.TypeFeature, "gone-9999")
	auditTicket(t, stores["beta"], "au-beta-0001", ticket.TypeFeature, "gone-9999")

	jsonOutput = true
	defer func() { jsonOutput = false }()

	out := captureAudit(t, "project", "beta")

	var result ticket.ParentAudit
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("audit --project=beta returned %d violations, want 1: %s", len(result.Violations), out)
	}
	if result.Violations[0].ID != "beta/au-beta-0001" {
		t.Errorf("id = %q, want beta/au-beta-0001", result.Violations[0].ID)
	}
	if result.Violations[0].Kind != ticket.ViolationParentMissing {
		t.Errorf("kind = %q, want %q", result.Violations[0].Kind, ticket.ViolationParentMissing)
	}
}

func TestAuditWarnsAboutUnreadableProject(t *testing.T) {
	// "No parent violations." must never speak for a store the audit could not
	// read in full, in either output mode.
	stores := setupFrontierStore(t, "alpha", "beta")
	unreadable := stores["beta"].Dir
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o755) })

	out := captureAudit(t)
	if !contains(out, "No parent violations.") {
		t.Errorf("alpha is clean, so the report should say so:\n%s", out)
	}
	if !contains(out, "beta") || !contains(out, "incomplete") {
		t.Errorf("audit should warn that beta could not be read:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.ParentAudit
	jsonOut := captureAudit(t)
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Project != "beta" {
		t.Errorf("json skipped = %v, want beta reported as unreadable", result.Skipped)
	}
}
