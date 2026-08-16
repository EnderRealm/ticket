package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
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

// auditEpic writes an epic carrying a stored status, the way a store written
// before epic statuses were derived holds one.
func auditEpic(t *testing.T, store *ticket.FileStore, id string, status ticket.Status) {
	t.Helper()
	writeLegacyTicket(t, store, &ticket.Ticket{
		ID: id, Status: status, Type: ticket.TypeEpic,
		Created: time.Now(), Title: "Epic " + id, Body: "\n",
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

	var result ticket.AuditReport
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

func TestAuditReportsEpicsReadingADifferentStatus(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	store := stores["alpha"]
	auditEpic(t, store, "au-hand-0001", ticket.StatusClosed)
	auditEpic(t, store, "au-drift-0002", ticket.StatusDone)
	auditTicket(t, store, "au-child-0003", ticket.TypeFeature, "au-drift-0002")
	auditEpic(t, store, "au-agrees-0004", ticket.StatusBacklog)

	out := captureAudit(t)

	if !contains(out, "au-hand-0001") || !contains(out, string(ticket.EpicDriftStoredClosed)) {
		t.Errorf("audit should call out the epic storing closed separately:\n%s", out)
	}
	if !contains(out, "tk edit <id> --status closed") {
		t.Errorf("audit should name the remedy for an epic storing closed:\n%s", out)
	}
	if !contains(out, "before editing the epic") {
		t.Errorf("audit should say the stored value is lost to the next write of the epic:\n%s", out)
	}
	if !contains(out, "ordinary edits made since") {
		t.Errorf("audit should say neither class is bounded to files written before the change:\n%s", out)
	}
	if !contains(out, "older than derived statuses") {
		t.Errorf("audit should say a stored value is evidence of intent only on an older file:\n%s", out)
	}
	if !contains(out, "au-drift-0002") || !contains(out, string(ticket.EpicDriftStale)) {
		t.Errorf("audit should report the epic whose stored status its children never agreed with:\n%s", out)
	}
	if contains(out, "au-agrees-0004") {
		t.Errorf("audit should not report an epic that reads what its file stores:\n%s", out)
	}
	if !contains(out, "2 epic(s)") {
		t.Errorf("audit should count exactly the two epics whose displayed status moved:\n%s", out)
	}
}

func TestAuditEpicStatusJSONAndProjectFilter(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	auditEpic(t, stores["alpha"], "au-alpha-0001", ticket.StatusClosed)
	auditEpic(t, stores["beta"], "au-beta-0001", ticket.StatusClosed)

	jsonOutput = true
	defer func() { jsonOutput = false }()

	out := captureAudit(t, "project", "beta")

	var result ticket.AuditReport
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if len(result.EpicStatus) != 1 {
		t.Fatalf("audit --project=beta returned %d epics, want 1: %s", len(result.EpicStatus), out)
	}
	got := result.EpicStatus[0]
	want := ticket.EpicStatusDrift{
		ID: "beta/au-beta-0001", Stored: ticket.StatusClosed,
		Derived: ticket.StatusBacklog, Kind: ticket.EpicDriftStoredClosed,
	}
	if got != want {
		t.Errorf("epic_status[0] = %+v, want %+v", got, want)
	}
}

func TestAuditWarnsAboutUnreadableFile(t *testing.T) {
	// A file the store could not read is a ticket the audit never saw, so the
	// report must not read clean — and since that ticket could be any epic's
	// child, the epic section reports the degraded value and says why.
	stores := setupFrontierStore(t, "alpha", "beta")
	auditEpic(t, stores["alpha"], "au-epic-0001", ticket.StatusDone)
	auditTicket(t, stores["alpha"], "au-child-0002", ticket.TypeFeature, "au-epic-0001")
	broken := filepath.Join(stores["alpha"].Dir, "au-broken-0003.md")
	if err := os.WriteFile(broken, []byte("---\nid: x\n  status: open\n---\n# Broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureAudit(t)
	if !contains(out, "au-broken-0003.md") || !contains(out, "incomplete") {
		t.Errorf("audit should name the unreadable file and call the report incomplete:\n%s", out)
	}
	if !contains(out, "could be any epic's child") {
		t.Errorf("audit should say why no epic reads done or closed:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "project", "alpha")
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	if len(result.SkippedFiles) != 1 || result.SkippedFiles[0].File != "au-broken-0003.md" || result.SkippedFiles[0].Project != "alpha" {
		t.Errorf("json skipped_files = %+v, want the alpha file reported", result.SkippedFiles)
	}

	// Scoped to the project that read in full, there is nothing to report.
	result = ticket.AuditReport{}
	jsonOut = captureAudit(t, "project", "beta")
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	if len(result.SkippedFiles) != 0 {
		t.Errorf("--project=beta reported %+v, want no skipped files", result.SkippedFiles)
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

	var result ticket.AuditReport
	jsonOut := captureAudit(t)
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Project != "beta" {
		t.Errorf("json skipped = %v, want beta reported as unreadable", result.Skipped)
	}
}
