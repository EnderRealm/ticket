package cmd

import (
	"encoding/json"
	"fmt"
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
	out, err := captureAuditErr(t, args...)
	if err != nil {
		t.Fatalf("runAudit: %v", err)
	}
	return out
}

// captureAuditErr runs the audit and returns its output and the error it exits
// with, for the classes that are meant to make the command fail.
func captureAuditErr(t *testing.T, args ...string) (string, error) {
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

	out, _ := io.ReadAll(r)
	return string(out), err
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

	out, err := captureAuditErr(t)
	if !contains(out, "au-broken-0003.md") || !contains(out, "incomplete") {
		t.Errorf("audit should name the unreadable file and call the report incomplete:\n%s", out)
	}
	if !contains(out, "could be any epic's child") {
		t.Errorf("audit should say why no epic reads done or closed:\n%s", out)
	}
	// A finding of its own, counted like the other classes and exited on: a
	// scripted audit reads the exit code, where a zero would call the store clean.
	if !contains(out, "1 file(s) could not be read as tickets") {
		t.Errorf("audit should count the unreadable files as a finding:\n%s", out)
	}
	if err == nil {
		t.Errorf("audit found an unreadable file and exited 0:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut, err := captureAuditErr(t, "project", "alpha")
	if err == nil {
		t.Errorf("audit --json found an unreadable file and exited 0:\n%s", jsonOut)
	}
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

func TestAuditReportsAFileNamingAnotherProject(t *testing.T) {
	// The audit reads such a file in full — it is a ticket the project holding
	// it cannot place, not one nothing could read — so it is reported on its own
	// terms and the report stays complete. It is also no epic's child here,
	// which is why the epic beside it still reads done.
	stores := setupFrontierStore(t, "alpha", "beta")
	auditEpic(t, stores["alpha"], "au-epic-0001", ticket.StatusDone)
	writeLegacyTicket(t, stores["alpha"], &ticket.Ticket{
		ID: "au-child-0002", Status: ticket.StatusDone, Type: ticket.TypeFeature,
		Parent: "au-epic-0001", Created: time.Now(), Title: "Item au-child-0002", Body: "\n",
	})
	alien := &ticket.Ticket{
		ID: "beta/au-alien-0003", Status: ticket.StatusDone, Type: ticket.TypeFeature,
		Parent: "au-epic-0001", Created: time.Now(), Title: "Another project's ticket", Body: "\n",
	}
	data, err := ticket.Serialize(alien)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stores["alpha"].Dir, "au-alien-0003.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	out, auditErr := captureAuditErr(t)
	if auditErr != nil {
		// Only the unreadable class exits non-zero: this file was read in full,
		// so the report is complete and the run succeeded.
		t.Errorf("audit exited non-zero over a file it read in full: %v\n%s", auditErr, out)
	}
	if !contains(out, "au-alien-0003.md") || !contains(out, "naming another project") {
		t.Errorf("audit should report the file as naming another project:\n%s", out)
	}
	// A project name is a directory name in the synced store, so it is quoted
	// like the filename beside it rather than reaching the terminal raw.
	if !contains(out, `project "alpha", file "au-alien-0003.md"`) {
		t.Errorf("audit should quote the project name it prints:\n%s", out)
	}
	if contains(out, "incomplete") || contains(out, "could be any epic's child") {
		t.Errorf("a file the audit read in full must not make the report incomplete:\n%s", out)
	}
	if !contains(out, "No epic reads a different status than its file stores.") {
		t.Errorf("the epic's own children are all done, so nothing about the planted file should degrade it:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "project", "alpha")
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	if len(result.SkippedFiles) != 1 || result.SkippedFiles[0].File != "au-alien-0003.md" {
		t.Fatalf("json skipped_files = %+v, want the planted file reported", result.SkippedFiles)
	}
	if result.SkippedFiles[0].Kind != ticket.FileSkipForeignNamespace {
		t.Errorf("kind = %q, want %q", result.SkippedFiles[0].Kind, ticket.FileSkipForeignNamespace)
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

// auditBodyTicket writes a ticket carrying a body, for the content audit.
func auditBodyTicket(t *testing.T, store *ticket.FileStore, id, body string) {
	t.Helper()
	writeLegacyTicket(t, store, &ticket.Ticket{
		ID: id, Status: ticket.StatusOpen, Type: ticket.TypeFeature,
		Created: time.Now(), Title: "Item " + id, Body: body,
	})
}

func TestAuditReportsMissingBodyContent(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	store := stores["alpha"]
	// Built from its pieces: a terminator spelled out here would corrupt the
	// tool call of any agent that quotes this file.
	terminator := "</" + "antml:invoke" + ">"
	auditBodyTicket(t, store, "au-frag-0001", "\nThe real description text.\n"+terminator+"\n")
	auditBodyTicket(t, store, "au-empty-0002", "\nA description and nothing else.\n")
	auditBodyTicket(t, store, "au-whole-0003", "\nA description.\n\n## Acceptance Criteria\n\nWhat done means.\n")
	// An epic holds children rather than criteria, so a description alone is
	// not a missing contract.
	writeLegacyTicket(t, store, &ticket.Ticket{
		ID: "au-epic-0004", Status: ticket.StatusBacklog, Type: ticket.TypeEpic,
		Created: time.Now(), Title: "Epic au-epic-0004", Body: "\nA description and nothing else.\n",
	})

	out := captureAudit(t)

	if !contains(out, "au-frag-0001") || !contains(out, string(ticket.ContentEnvelopeFragment)) {
		t.Errorf("audit should report the ticket whose description absorbed part of a tool call:\n%s", out)
	}
	if !contains(out, "au-empty-0002") || !contains(out, string(ticket.ContentEmptyAcceptance)) {
		t.Errorf("audit should report the ticket with a description and no acceptance criteria:\n%s", out)
	}
	if contains(out, "au-whole-0003") {
		t.Errorf("audit should not report a ticket carrying both halves of the contract:\n%s", out)
	}
	if contains(out, "au-epic-0004") {
		t.Errorf("audit should not report an epic as missing acceptance criteria:\n%s", out)
	}
	// The fragment ticket is reported twice on purpose: the absorbed text is one
	// fact, and the acceptance criteria it swallowed still being absent is the
	// other — repairing the markup alone would leave the ticket uncontracted.
	if !contains(out, "1 section(s)") || !contains(out, "2 ticket(s) carry a description") {
		t.Errorf("audit should count each class separately:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "project", "alpha")
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	got := map[ticket.ContentIssue]bool{}
	for _, c := range result.Content {
		got[c] = true
	}
	for _, want := range []ticket.ContentIssue{
		{ID: "alpha/au-frag-0001", Kind: ticket.ContentEnvelopeFragment, Field: "description", Detail: "The real description text.\n" + terminator},
		{ID: "alpha/au-frag-0001", Kind: ticket.ContentEmptyAcceptance},
		{ID: "alpha/au-empty-0002", Kind: ticket.ContentEmptyAcceptance},
	} {
		if !got[want] {
			t.Errorf("json content is missing %+v: %+v", want, result.Content)
		}
	}
	if len(result.Content) != 3 {
		t.Errorf("json content = %+v, want exactly the three issues", result.Content)
	}

	// A clean project emits the key as an empty array rather than dropping it:
	// a consumer cannot otherwise tell "nothing to report" from a build that
	// does not report content at all.
	jsonOut = captureAudit(t, "project", "beta")
	if !contains(jsonOut, `"content": []`) {
		t.Errorf("a clean project should still emit content as an empty array:\n%s", jsonOut)
	}
}

// captureContentIssues renders one content section and returns what it printed.
func captureContentIssues(t *testing.T, issues []ticket.ContentIssue) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printContentIssues(issues)

	w.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(r)
	return string(out)
}

// A ticket ID carries its project namespace, and a project name is a store
// directory name or a shared-config key another machine wrote, bounded against
// path separators and nothing else.
func TestContentIssueIDsAreSanitized(t *testing.T) {
	out := captureContentIssues(t, []ticket.ContentIssue{
		{ID: "al\x1b[2Kpha/au-frag-0001", Kind: ticket.ContentEnvelopeFragment, Field: "description", Detail: "text"},
		{ID: "alpha/au-empty-0002", Kind: ticket.ContentEmptyAcceptance},
	})
	if contains(out, "\x1b") {
		t.Errorf("an escape sequence in an ID reached the output raw: %q", out)
	}
	if !contains(out, "alpha/au-empty-0002") {
		t.Errorf("an ordinary ID should print unchanged:\n%s", out)
	}
}

func TestAuditCapsTheEmptyAcceptanceListing(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	for i := 0; i < contentEmptyListLimit+3; i++ {
		auditBodyTicket(t, stores["alpha"], fmt.Sprintf("au-stub-%04d", i), "\nA description and nothing else.\n")
	}

	out := captureAudit(t)

	// A backlog stub is the ordinary state, so the section reports the count and
	// names only the first few rather than burying the sections above it.
	if !contains(out, "... and 3 more") {
		t.Errorf("audit should cap the empty-acceptance listing:\n%s", out)
	}
	if !contains(out, fmt.Sprintf("%d ticket(s) carry a description", contentEmptyListLimit+3)) {
		t.Errorf("audit should still count every empty-acceptance ticket:\n%s", out)
	}
}
