package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// captureAudit runs the audit for one cause — or, with an empty cause, the
// summary of every cause — and returns what it printed.
func captureAudit(t *testing.T, cause string, flags ...string) string {
	t.Helper()
	out, err := captureAuditErr(t, cause, flags...)
	if err != nil {
		t.Fatalf("runAudit: %v", err)
	}
	return out
}

// captureAuditErr runs the audit and returns its output and the error it exits
// with, for the classes that are meant to make the command fail.
func captureAuditErr(t *testing.T, cause string, flags ...string) (string, error) {
	t.Helper()
	if err := auditCmd.Flags().Set("project", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(flags); i += 2 {
		if err := auditCmd.Flags().Set(flags[i], flags[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	var args []string
	if cause != "" {
		args = []string{cause}
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAudit(auditCmd, args)

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

	out := captureAudit(t, string(ticket.ViolationParentNotEpic))

	if !contains(out, "au-bad-0003") || !contains(out, string(ticket.ViolationParentNotEpic)) {
		t.Errorf("audit output should report au-bad-0003 as parent-not-epic:\n%s", out)
	}
	if !contains(out, "1 ticket(s) violate") {
		t.Errorf("audit should report exactly the one violation, not the valid child:\n%s", out)
	}
	// The listing is the drill-in's alone: the summary counts it and names the
	// command that prints it.
	summary := captureAudit(t, "")
	if contains(summary, "au-bad-0003") {
		t.Errorf("the summary should list no tickets:\n%s", summary)
	}
	count, command := summaryRow(t, summary, string(ticket.ViolationParentNotEpic))
	if count != 1 {
		t.Errorf("summary counts %d parent-not-epic violations, want 1:\n%s", count, summary)
	}
	if command != "tk audit parent-not-epic" {
		t.Errorf("summary drills in with %q, want `tk audit parent-not-epic`", command)
	}
}

// summaryRow reads one cause's line off the summary: the count it carries and
// the command it names for listing that cause's tickets.
func summaryRow(t *testing.T, out, cause string) (int, string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != cause {
			continue
		}
		count, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatalf("summary line %q carries no count: %v", line, err)
		}
		return count, strings.Join(fields[2:], " ")
	}
	t.Fatalf("summary has no line for %s:\n%s", cause, out)
	return 0, ""
}

func TestAuditJSONAndProjectFilter(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	auditTicket(t, stores["alpha"], "au-alpha-0001", ticket.TypeFeature, "gone-9999")
	auditTicket(t, stores["beta"], "au-beta-0001", ticket.TypeFeature, "gone-9999")

	jsonOutput = true
	defer func() { jsonOutput = false }()

	out := captureAudit(t, "", "project", "beta")

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

	// The two classes are separate causes, each reached by its own command.
	out := captureAudit(t, string(ticket.EpicDriftStoredClosed))

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
	if contains(out, "au-drift-0002") {
		t.Errorf("a drill-in should list its own cause and no other:\n%s", out)
	}

	stale := captureAudit(t, string(ticket.EpicDriftStale))

	if !contains(stale, "au-drift-0002") || !contains(stale, string(ticket.EpicDriftStale)) {
		t.Errorf("audit should report the epic whose stored status its children never agreed with:\n%s", stale)
	}
	if contains(stale, "au-hand-0001") || contains(stale, "tk edit <id> --status closed") {
		t.Errorf("a stale-status drill-in should carry nothing of the stored-closed class:\n%s", stale)
	}
	for _, out := range []string{out, stale} {
		if contains(out, "au-agrees-0004") {
			t.Errorf("audit should not report an epic that reads what its file stores:\n%s", out)
		}
		if !contains(out, "1 epic(s) read the status") {
			t.Errorf("each drill-in should count its own epics:\n%s", out)
		}
	}

	summary := captureAudit(t, "")
	for _, cause := range []string{string(ticket.EpicDriftStoredClosed), string(ticket.EpicDriftStale)} {
		if count, _ := summaryRow(t, summary, cause); count != 1 {
			t.Errorf("summary counts %d %s epics, want 1:\n%s", count, cause, summary)
		}
	}
}

// Every printer degrades to a zero-findings line rather than reading its class
// off the first finding, so nothing but runAudit's own zero branch stands
// between an empty listing and a panic.
func TestEpicStatusDriftPrinterTakesItsKindRatherThanTheFirstFinding(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printEpicStatusDrift(ticket.EpicDriftStale, nil)

	w.Close()
	os.Stdout = oldStdout
	raw, _ := io.ReadAll(r)
	out := string(raw)

	if !contains(out, "0 epic(s) read the status") {
		t.Errorf("an empty listing should report zero findings:\n%s", out)
	}
	if !contains(out, string(ticket.EpicDriftStale)) {
		t.Errorf("an empty listing should still name the class it was called for:\n%s", out)
	}
	if contains(out, "with no abandon flag") {
		t.Errorf("the stored-closed remedy belongs to its own class only:\n%s", out)
	}
}

func TestAuditEpicStatusJSONAndProjectFilter(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	auditEpic(t, stores["alpha"], "au-alpha-0001", ticket.StatusClosed)
	auditEpic(t, stores["beta"], "au-beta-0001", ticket.StatusClosed)

	jsonOutput = true
	defer func() { jsonOutput = false }()

	out := captureAudit(t, "", "project", "beta")

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

	// The summary lists no file, but it still says the report is incomplete —
	// the warning qualifies every count in it — and it still exits non-zero.
	out, err := captureAuditErr(t, "")
	if !contains(out, "incomplete") {
		t.Errorf("audit should call the report incomplete:\n%s", out)
	}
	if contains(out, "au-broken-0003.md") {
		t.Errorf("the summary should list no file:\n%s", out)
	}
	if count, command := summaryRow(t, out, string(ticket.FileSkipUnreadable)); count != 1 || command != "tk audit unreadable" {
		t.Errorf("summary reports %d unreadable file(s) listed by %q, want 1 and `tk audit unreadable`:\n%s", count, command, out)
	}
	if err == nil {
		t.Errorf("audit found an unreadable file and exited 0:\n%s", out)
	}

	drill, err := captureAuditErr(t, string(ticket.FileSkipUnreadable))
	if !contains(drill, "au-broken-0003.md") {
		t.Errorf("the drill-in should name the unreadable file:\n%s", drill)
	}
	if !contains(drill, "could be any epic's child") {
		t.Errorf("audit should say why no epic reads done or closed:\n%s", drill)
	}
	// A finding of its own, counted like the other classes and exited on: a
	// scripted audit reads the exit code, where a zero would call the store clean.
	if !contains(drill, "1 file(s) could not be read as tickets") {
		t.Errorf("audit should count the unreadable files as a finding:\n%s", drill)
	}
	if err == nil {
		t.Errorf("audit drilled into an unreadable file and exited 0:\n%s", drill)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut, err := captureAuditErr(t, "", "project", "alpha")
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
	jsonOut = captureAudit(t, "", "project", "beta")
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

	out, auditErr := captureAuditErr(t, string(ticket.FileSkipForeignNamespace))
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

	summary, auditErr := captureAuditErr(t, "")
	if auditErr != nil {
		t.Errorf("audit exited non-zero over a file it read in full: %v\n%s", auditErr, summary)
	}
	if contains(summary, "incomplete") || contains(summary, "could be any epic's child") {
		t.Errorf("a file the audit read in full must not make the report incomplete:\n%s", summary)
	}
	if count, _ := summaryRow(t, summary, string(ticket.FileSkipForeignNamespace)); count != 1 {
		t.Errorf("summary counts %d files naming another project, want 1:\n%s", count, summary)
	}
	// The epic's own children are all done, so nothing about the planted file
	// should have degraded it.
	for _, cause := range []string{string(ticket.EpicDriftStoredClosed), string(ticket.EpicDriftStale)} {
		if count, _ := summaryRow(t, summary, cause); count != 0 {
			t.Errorf("summary reports %d %s epics, want none:\n%s", count, cause, summary)
		}
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "", "project", "alpha")
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
	// A summary of zeros must never speak for a store the audit could not read
	// in full, in either output mode.
	stores := setupFrontierStore(t, "alpha", "beta")
	unreadable := stores["beta"].Dir
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o755) })

	out := captureAudit(t, "")
	if !contains(out, "No findings — every cause above is clear.") {
		t.Errorf("alpha is clean, so the report should say so:\n%s", out)
	}
	if !contains(out, "beta") || !contains(out, "incomplete") {
		t.Errorf("audit should warn that beta could not be read:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "")
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

	fragments := captureAudit(t, string(ticket.ContentEnvelopeFragment))
	empty := captureAudit(t, string(ticket.ContentEmptyAcceptance))

	if !contains(fragments, "au-frag-0001") || !contains(fragments, string(ticket.ContentEnvelopeFragment)) {
		t.Errorf("audit should report the ticket whose description absorbed part of a tool call:\n%s", fragments)
	}
	if !contains(empty, "au-empty-0002") || !contains(empty, string(ticket.ContentEmptyAcceptance)) {
		t.Errorf("audit should report the ticket with a description and no acceptance criteria:\n%s", empty)
	}
	for _, out := range []string{fragments, empty} {
		if contains(out, "au-whole-0003") {
			t.Errorf("audit should not report a ticket carrying both halves of the contract:\n%s", out)
		}
		if contains(out, "au-epic-0004") {
			t.Errorf("audit should not report an epic as missing acceptance criteria:\n%s", out)
		}
	}
	// The fragment ticket is reported twice on purpose: the absorbed text is one
	// fact, and the acceptance criteria it swallowed still being absent is the
	// other — repairing the markup alone would leave the ticket uncontracted.
	if !contains(fragments, "1 section(s)") || !contains(empty, "2 ticket(s) carry a description") {
		t.Errorf("audit should count each class separately:\n%s\n%s", fragments, empty)
	}
	if !contains(empty, "au-frag-0001") {
		t.Errorf("the fragment ticket also states no contract, so it belongs in the empty-acceptance listing:\n%s", empty)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "", "project", "alpha")
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
	jsonOut = captureAudit(t, "", "project", "beta")
	if !contains(jsonOut, `"content": []`) {
		t.Errorf("a clean project should still emit content as an empty array:\n%s", jsonOut)
	}
}

func TestAuditReportsLegacyReviewLogs(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	store := stores["alpha"]
	section := "\n## Review Log\n\n**2026-02-25T12:00:00Z [agent:design-reviewer]**\nAPPROVED — All file paths verified.\n"
	contract := "\nA description.\n\n## Acceptance Criteria\n\nWhat done means.\n"
	auditBodyTicket(t, store, "au-rlog-0001", contract+section)
	auditBodyTicket(t, store, "au-clean-0002", contract)

	out := captureAudit(t, string(ticket.ContentLegacyReviewLog))

	if !contains(out, "au-rlog-0001") || !contains(out, string(ticket.ContentLegacyReviewLog)) {
		t.Errorf("audit should report the ticket still storing a Review Log:\n%s", out)
	}
	if contains(out, "au-clean-0002") {
		t.Errorf("audit should not report a ticket carrying no Review Log:\n%s", out)
	}
	if !contains(out, "1 ticket(s) still store") {
		t.Errorf("audit should count the tickets still storing one:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "", "project", "alpha")
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	want := ticket.ContentIssue{ID: "alpha/au-rlog-0001", Kind: ticket.ContentLegacyReviewLog, Bytes: len(section)}
	found := false
	for _, c := range result.Content {
		if c == want {
			found = true
		}
		if c.Kind == ticket.ContentLegacyReviewLog && c.ID != want.ID {
			t.Errorf("json content reports %+v, want only the ticket that stores a section", c)
		}
	}
	if !found {
		t.Errorf("json content is missing %+v: %+v", want, result.Content)
	}
}

// captureContentIssues renders one content cause's listing and returns what it
// printed.
func captureContentIssues(t *testing.T, kind ticket.ContentIssueKind, issues []ticket.ContentIssue) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printContentIssues(kind, issues)

	w.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(r)
	return string(out)
}

// A ticket ID carries its project namespace, and a project name is a store
// directory name or a shared-config key another machine wrote, bounded against
// path separators and nothing else.
func TestContentIssueIDsAreSanitized(t *testing.T) {
	out := captureContentIssues(t, ticket.ContentEnvelopeFragment, []ticket.ContentIssue{
		{ID: "al\x1b[2Kpha/au-frag-0001", Kind: ticket.ContentEnvelopeFragment, Field: "description", Detail: "text"},
	})
	out += captureContentIssues(t, ticket.ContentEmptyAcceptance, []ticket.ContentIssue{
		{ID: "be\x1b[2Kta/au-empty-0001", Kind: ticket.ContentEmptyAcceptance},
		{ID: "alpha/au-empty-0002", Kind: ticket.ContentEmptyAcceptance},
	})
	if contains(out, "\x1b") {
		t.Errorf("an escape sequence in an ID reached the output raw: %q", out)
	}
	if !contains(out, "alpha/au-empty-0002") {
		t.Errorf("an ordinary ID should print unchanged:\n%s", out)
	}
}

func TestAuditDoesNotCapTheEmptyAcceptanceListing(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	const count = 13
	for i := 0; i < count; i++ {
		auditBodyTicket(t, stores["alpha"], fmt.Sprintf("au-stub-%04d", i), "\nA description and nothing else.\n")
	}

	out := captureAudit(t, string(ticket.ContentEmptyAcceptance))

	// The cap this class alone used to carry existed to stop the listing burying
	// the rest of the report. Nothing lists per-ticket unless it was asked to
	// now, so the cap has nothing to do and the drill-in names every ticket.
	for i := 0; i < count; i++ {
		if !contains(out, fmt.Sprintf("au-stub-%04d  empty-acceptance", i)) {
			t.Errorf("audit should name every empty-acceptance ticket, and did not name au-stub-%04d:\n%s", i, out)
		}
	}
	if contains(out, "... and ") {
		t.Errorf("audit should not cap the empty-acceptance listing:\n%s", out)
	}
	if !contains(out, fmt.Sprintf("%d ticket(s) carry a description", count)) {
		t.Errorf("audit should still count every empty-acceptance ticket:\n%s", out)
	}
}

func TestAuditReportsBareAcceptanceCriteria(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	store := stores["alpha"]
	auditBodyTicket(t, store, "au-bare-0001", "\nA description.\n\n## Acceptance Criteria\n\n- Nothing checks this.\n- Nor this.\n- This one is checked.\n  verify: go test ./cmd/\n")
	auditBodyTicket(t, store, "au-checked-0002", "\nA description.\n\n## Acceptance Criteria\n\n- Checked.\n  verify: go test ./cmd/\n- Marked.\n  unverifiable: no command can decide a reading of prose.\n")
	auditBodyTicket(t, store, "au-unver-0003", "\nA description.\n\n## Acceptance Criteria\n\n- Marked.\n  unverifiable: no command can decide a reading of prose.\n")
	// No acceptance section at all: a stub states no criteria, so it has none
	// that could be bare. Reported as empty-acceptance and nothing else.
	auditBodyTicket(t, store, "au-stub-0004", "\nA description and nothing else.\n")
	// The same for an epic, which the empty-acceptance half also passes over.
	writeLegacyTicket(t, store, &ticket.Ticket{
		ID: "au-epic-0005", Status: ticket.StatusBacklog, Type: ticket.TypeEpic,
		Created: time.Now(), Title: "Epic au-epic-0005", Body: "\nA description and nothing else.\n",
	})
	// An epic that does state criteria is reported like anything else: the
	// empty-acceptance check exempts epics because a container carries no
	// contract of its own, but criteria that were written and cannot be checked
	// are the same gap whatever the type.
	writeLegacyTicket(t, store, &ticket.Ticket{
		ID: "au-epicbare-0006", Status: ticket.StatusBacklog, Type: ticket.TypeEpic,
		Created: time.Now(), Title: "Epic au-epicbare-0006",
		Body: "\nA description.\n\n## Acceptance Criteria\n\n- Nothing checks this.\n",
	})

	out := captureAudit(t, string(ticket.ContentBareAcceptance))

	if !contains(out, "au-bare-0001  bare-acceptance  2 bare criterion(s)") {
		t.Errorf("audit should name the ticket and how many of its criteria are bare:\n%s", out)
	}
	if contains(out, "au-checked-0002") || contains(out, "au-unver-0003") {
		t.Errorf("audit should not report criteria carrying a verify or unverifiable line:\n%s", out)
	}
	for _, id := range []string{"au-stub-0004", "au-epic-0005"} {
		if contains(out, id+"  "+string(ticket.ContentBareAcceptance)) {
			t.Errorf("audit should not report %s, which states no criteria at all:\n%s", id, out)
		}
	}
	if !contains(out, "au-epicbare-0006  bare-acceptance  1 bare criterion(s)") {
		t.Errorf("audit should report an epic that states criteria and leaves them bare:\n%s", out)
	}
	if !contains(out, "2 ticket(s) carry 3 acceptance criterion(s) with neither") {
		t.Errorf("audit should count the tickets and their bare criteria:\n%s", out)
	}
	if !contains(out, "`unverifiable: <reason>` line saying why no command can exist") {
		t.Errorf("audit should name the remedy for a bare criterion:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	var result ticket.AuditReport
	jsonOut := captureAudit(t, "", "project", "alpha")
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, jsonOut)
	}
	want := map[ticket.ContentIssue]bool{
		{ID: "alpha/au-bare-0001", Kind: ticket.ContentBareAcceptance, Bare: 2}:     false,
		{ID: "alpha/au-epicbare-0006", Kind: ticket.ContentBareAcceptance, Bare: 1}: false,
	}
	for _, c := range result.Content {
		if c.Kind != ticket.ContentBareAcceptance {
			continue
		}
		if _, ok := want[c]; !ok {
			t.Errorf("json content reports %+v, want only the tickets with bare criteria", c)
			continue
		}
		want[c] = true
	}
	for issue, found := range want {
		if !found {
			t.Errorf("json content is missing %+v: %+v", issue, result.Content)
		}
	}
}

func TestAuditDoesNotCapTheBareAcceptanceListing(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	const count = 13
	for i := 0; i < count; i++ {
		auditBodyTicket(t, stores["alpha"], fmt.Sprintf("au-bare-%04d", i),
			"\nA description.\n\n## Acceptance Criteria\n\n- Nothing checks this.\n")
	}

	out := captureAudit(t, string(ticket.ContentBareAcceptance))

	// The drill-in is never summarised as a count: a bare criterion is repaired
	// one ticket at a time, so a ticket left off the listing is one nobody can
	// act on. The overview it used to bury is the summary form's job.
	for i := 0; i < count; i++ {
		if !contains(out, fmt.Sprintf("au-bare-%04d  bare-acceptance  1 bare criterion(s)", i)) {
			t.Errorf("audit should name every ticket carrying a bare criterion, and did not name au-bare-%04d:\n%s", i, out)
		}
	}
	// No fixture here yields an empty-acceptance issue, so any "... and N more"
	// in this output would be a cap applied to the bare class.
	if contains(out, "... and ") {
		t.Errorf("audit should not cap the bare-acceptance listing:\n%s", out)
	}
	if !contains(out, fmt.Sprintf("%d ticket(s) carry %d acceptance criterion(s) with neither", count, count)) {
		t.Errorf("audit should count every ticket carrying a bare criterion:\n%s", out)
	}
}

func TestAuditSummaryNamesEveryCauseItAccepts(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	auditBodyTicket(t, stores["alpha"], "au-stub-0001", "\nA description and nothing else.\n")

	summary := captureAudit(t, "")

	// Driven off the registry in both directions, because that is the property:
	// the summary prints one line per cause and the argument accepts exactly
	// those names, so neither set can grow a name the other does not have.
	printed := map[string]bool{}
	for _, line := range strings.Split(summary, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.HasPrefix(strings.Join(fields[2:], " "), "tk audit ") {
			continue
		}
		printed[fields[0]] = true
	}
	for _, cause := range auditCauseNames() {
		if !printed[cause] {
			t.Errorf("the summary does not name %s:\n%s", cause, summary)
		}
		delete(printed, cause)
		if _, err := captureAuditErr(t, cause); err != nil {
			t.Errorf("audit %s: %v", cause, err)
		}
	}
	for cause := range printed {
		t.Errorf("the summary names %q, which the argument does not accept:\n%s", cause, summary)
	}
}

func TestAuditRejectsAnUnknownCause(t *testing.T) {
	setupFrontierStore(t, "alpha")

	// A name nothing can list is the caller's error: an empty report exiting 0
	// would read as a clean store.
	out, err := captureAuditErr(t, "bare-acceptence")
	if err == nil {
		t.Fatalf("audit accepted a cause it cannot list:\n%s", out)
	}
	if !contains(err.Error(), string(ticket.ContentBareAcceptance)) || !contains(err.Error(), string(ticket.FileSkipUnreadable)) {
		t.Errorf("the error should name the valid causes: %v", err)
	}
	if out != "" {
		t.Errorf("a rejected run should print no report:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	jsonOut, err := captureAuditErr(t, "bare-acceptence")
	if err == nil || jsonOut != "" {
		t.Errorf("--json accepted a cause it cannot list: %v\n%s", err, jsonOut)
	}
}

func TestAuditSummaryScopesItsDrillInCommands(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	auditBodyTicket(t, stores["alpha"], "au-stub-0001", "\nA description and nothing else.\n")
	auditBodyTicket(t, stores["beta"], "au-stub-0002", "\nA description and nothing else.\n")

	summary := captureAudit(t, "", "project", "alpha")

	count, command := summaryRow(t, summary, string(ticket.ContentEmptyAcceptance))
	if count != 1 {
		t.Errorf("summary counts %d empty-acceptance tickets in alpha, want 1:\n%s", count, summary)
	}
	// The command as printed has to list the tickets the count was taken over.
	if command != "tk audit --project=alpha empty-acceptance" {
		t.Errorf("summary drills in with %q, want the project scope carried:\n%s", command, summary)
	}

	drill := captureAudit(t, string(ticket.ContentEmptyAcceptance), "project", "alpha")
	if !contains(drill, "alpha/au-stub-0001") || contains(drill, "beta/au-stub-0002") {
		t.Errorf("the drill-in should list what the summary counted:\n%s", drill)
	}
}

func TestAuditSummaryQuotesAProjectAShellWouldRead(t *testing.T) {
	// The drill-in line is the one string in this report printed to be copied
	// into a shell and run, so a name carrying anything a shell or a terminal
	// reads is quoted or stripped rather than printed as it stands.
	for _, tc := range []struct {
		name string
		proj string
		want string
	}{
		{"shell metacharacters", "alpha$(id)", `tk audit --project='alpha$(id)' empty-acceptance`},
		{"a quote", "al'pha", `tk audit --project='al'\''pha' empty-acceptance`},
		{"a control character", "alpha\x01beta", "tk audit --project='alpha\ufffdbeta' empty-acceptance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stores := setupFrontierStore(t, tc.proj)
			auditBodyTicket(t, stores[tc.proj], "au-stub-0001", "\nA description and nothing else.\n")

			summary := captureAudit(t, "", "project", tc.proj)

			_, command := summaryRow(t, summary, string(ticket.ContentEmptyAcceptance))
			if command != tc.want {
				t.Errorf("summary drills in with %q, want %q:\n%s", command, tc.want, summary)
			}
		})
	}
}

func TestAuditJSONIsTheWholeReportInBothForms(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	auditTicket(t, stores["alpha"], "au-bad-0001", ticket.TypeFeature, "gone-9999")
	auditBodyTicket(t, stores["alpha"], "au-stub-0002", "\nA description and nothing else.\n")

	jsonOutput = true
	defer func() { jsonOutput = false }()

	summary := captureAudit(t, "")
	drill := captureAudit(t, string(ticket.ContentEmptyAcceptance))

	// The cause argument never filters --json: it is machine-readable, it buries
	// nothing, and a scripted caller reads the whole object.
	if summary != drill {
		t.Errorf("the cause argument filtered --json:\n%s\n%s", summary, drill)
	}
	var result ticket.AuditReport
	if err := json.Unmarshal([]byte(drill), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, drill)
	}
	if len(result.Violations) != 1 || len(result.Content) != 1 {
		t.Errorf("--json should carry the whole report whatever cause was named: %+v", result)
	}
}

func TestAuditSaysACleanStoreIsClean(t *testing.T) {
	setupFrontierStore(t, "alpha")

	out := captureAudit(t, "")

	if !contains(out, "No findings — every cause above is clear.") {
		t.Errorf("a store with nothing to report should say so:\n%s", out)
	}
	if count, _ := summaryRow(t, out, string(ticket.ContentBareAcceptance)); count != 0 {
		t.Errorf("summary counts %d bare-acceptance tickets in a clean store:\n%s", count, out)
	}

	drill := captureAudit(t, string(ticket.ContentBareAcceptance))
	if !contains(drill, "No bare-acceptance findings.") {
		t.Errorf("a drill-in with nothing to list should say so:\n%s", drill)
	}
}
