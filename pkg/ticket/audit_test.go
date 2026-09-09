package ticket

import (
	"reflect"
	"testing"
)

// mkBody is mk with a body to audit the content of.
func mkBody(id, body string) *Ticket {
	t := mk(id, StatusOpen)
	t.Body = body
	return t
}

// auditFixtureStore holds one of everything the audit reports: each parent
// violation class, an epic whose stored status drifted from the one it derives,
// a body in each shape the content audit names, and a file that yields no
// ticket at all.
func auditFixtureStore(t *testing.T) *countingStore {
	t.Helper()
	dir := t.TempDir()
	s := NewProjectFileStore(dir, "proj")
	// Stores backlog and derives open from the child below.
	writeLegacy(t, s, mkEpic("epic-1111", StatusBacklog, ""))
	writeLegacy(t, s, mkWithParent("good-2222", StatusOpen, "epic-1111"))
	writeLegacy(t, s, mkWithParent("notepic-3333", StatusOpen, "good-2222"))
	writeLegacy(t, s, mkWithParent("missing-4444", StatusOpen, "gone-9999"))
	writeLegacy(t, s, mkEpic("subepic-5555", StatusOpen, "epic-1111"))
	writeLegacy(t, s, mkWithParent("cyc-6666", StatusOpen, "cyc-7777"))
	writeLegacy(t, s, mkWithParent("cyc-7777", StatusOpen, "cyc-6666"))
	// Built from its pieces: a terminator spelled out here would corrupt the
	// tool call of any agent that quotes this file.
	terminator := "</" + "antml:invoke" + ">"
	writeLegacy(t, s, mkBody("frag-8888", "\nThe real description text.\n"+terminator+"\n"))
	writeLegacy(t, s, mkBody("bare-9999", "\nA description.\n\n## Acceptance Criteria\n\n- Something happens.\n"))
	writeLegacy(t, s, mkBody("rlog-0001", "\nA description.\n\n## Review Log\n\n**2026-02-25T12:00:00Z [agent:design-reviewer]**\nAPPROVED\n"))
	plantUnreadable(t, dir, "broken-0002.md")
	return &countingStore{FileStore: s}
}

func TestAuditorTicketReadsNothingAfterPreparation(t *testing.T) {
	// The listing, the parent index and the children map are built once by
	// NewAuditor, so answering a ticket touches the store not at all. The one
	// read a ticket can still cost is parentLookup's fallback, for a parent no
	// listed ticket matches — a single-ticket resolution the store-wide audit
	// makes identically, which is why this store holds only parents the listing
	// answers and TestAuditorPerTicketCostsWhatAuditCosts counts the rest.
	fs := NewProjectFileStore(t.TempDir(), "proj")
	writeLegacy(t, fs, mkEpic("epic-1111", StatusBacklog, ""))
	writeLegacy(t, fs, mkWithParent("good-2222", StatusOpen, "epic-1111"))
	writeLegacy(t, fs, mkWithParent("notepic-3333", StatusOpen, "good-2222"))
	s := &countingStore{FileStore: fs}

	auditor, err := NewAuditor(s)
	if err != nil {
		t.Fatalf("NewAuditor: %v", err)
	}
	tickets, _, err := fs.listStored()
	if err != nil {
		t.Fatalf("listStored: %v", err)
	}
	lists, reads := s.lists, s.gets

	found := 0
	for _, tk := range tickets {
		f, err := auditor.Ticket(tk)
		if err != nil {
			t.Fatalf("Ticket %s: %v", tk.ID, err)
		}
		if !f.Empty() {
			found++
		}
	}
	if found == 0 {
		t.Fatal("the fixture holds a parent violation and a drifted epic, so the loop should have found something to report")
	}
	if s.lists != lists {
		t.Errorf("Auditor.Ticket listed the store %d time(s) after preparation", s.lists-lists)
	}
	if s.gets != reads {
		t.Errorf("Auditor.Ticket read %d ticket(s) after preparation", s.gets-reads)
	}
}

func TestAuditorPerTicketCostsWhatAuditCosts(t *testing.T) {
	// Auditing a store one ticket at a time is the same reads as auditing it
	// whole: preparing once is what keeps the per-ticket entry point from being
	// a listing per ticket.
	s := auditFixtureStore(t)
	if _, err := Audit(s); err != nil {
		t.Fatalf("Audit: %v", err)
	}
	wantLists, wantReads := s.lists, s.gets
	s.lists, s.gets = 0, 0

	auditor, err := NewAuditor(s)
	if err != nil {
		t.Fatalf("NewAuditor: %v", err)
	}
	tickets, _, err := s.FileStore.listStored()
	if err != nil {
		t.Fatalf("listStored: %v", err)
	}
	for _, tk := range tickets {
		if _, err := auditor.Ticket(tk); err != nil {
			t.Fatalf("Ticket %s: %v", tk.ID, err)
		}
	}

	if s.lists != wantLists || s.gets != wantReads {
		t.Errorf("auditing %d tickets one at a time cost %d listing(s) and %d read(s), want the %d and %d a whole-store Audit costs",
			len(tickets), s.lists, s.gets, wantLists, wantReads)
	}
}

func TestAuditReportIsThePerTicketFindings(t *testing.T) {
	// One reading of what is wrong with a ticket: the store-wide report is the
	// per-ticket check fanned out, not a second pass that can drift from it.
	s := auditFixtureStore(t)
	report, err := Audit(s)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(report.Violations) == 0 || len(report.EpicStatus) == 0 || len(report.Content) == 0 || len(report.SkippedFiles) != 1 {
		t.Fatalf("fixture should present every class, got %+v", report)
	}

	auditor, err := NewAuditor(s)
	if err != nil {
		t.Fatalf("NewAuditor: %v", err)
	}
	tickets, skips, err := s.FileStore.listStored()
	if err != nil {
		t.Fatalf("listStored: %v", err)
	}
	var assembled AuditReport
	for _, tk := range tickets {
		f, err := auditor.Ticket(tk)
		if err != nil {
			t.Fatalf("Ticket %s: %v", tk.ID, err)
		}
		if f.Parent != nil {
			assembled.Violations = append(assembled.Violations, *f.Parent)
		}
		if f.EpicStatus != nil {
			assembled.EpicStatus = append(assembled.EpicStatus, *f.EpicStatus)
		}
		assembled.Content = append(assembled.Content, f.Content...)
	}
	// The skipped file is no ticket's finding — it is carried from the listing,
	// which is exactly why Findings has nowhere to put it.
	for i := range skips {
		skips[i].Project = s.Project
	}
	assembled.SkippedFiles = skips

	if !reflect.DeepEqual(report, assembled) {
		t.Errorf("Audit reported\n%+v\nbut the per-ticket path reports\n%+v", report, assembled)
	}
}

func TestFindingsCannotExpressAFileLevelSkip(t *testing.T) {
	// unreadable and foreign-namespace describe a file that never became a
	// ticket, so a ticket handed to the per-ticket path can never carry one.
	// Unexpressible rather than always empty: an empty field would read as a
	// check that ran and found nothing.
	fileLevel := map[reflect.Type]bool{
		reflect.TypeOf(FileSkip{}):    true,
		reflect.TypeOf(ProjectSkip{}): true,
	}
	var reaches func(reflect.Type, map[reflect.Type]bool) bool
	reaches = func(typ reflect.Type, seen map[reflect.Type]bool) bool {
		if fileLevel[typ] {
			return true
		}
		if seen[typ] {
			return false
		}
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Array:
			return reaches(typ.Elem(), seen)
		case reflect.Map:
			return reaches(typ.Key(), seen) || reaches(typ.Elem(), seen)
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				if reaches(typ.Field(i).Type, seen) {
					return true
				}
			}
		}
		return false
	}

	findings := reflect.TypeOf(Findings{})
	for i := 0; i < findings.NumField(); i++ {
		field := findings.Field(i)
		if reaches(field.Type, map[reflect.Type]bool{}) {
			t.Errorf("Findings.%s (%s) can hold a FileSkip or a ProjectSkip", field.Name, field.Type)
		}
	}
}

func TestAuditorTicketTellsCleanFromUnevaluable(t *testing.T) {
	s := NewProjectFileStore(t.TempDir(), "proj")
	if err := s.Create(mkEpic("epic-1111", StatusBacklog, "")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(mkWithParent("good-2222", StatusOpen, "epic-1111")); err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(s)
	if err != nil {
		t.Fatalf("NewAuditor: %v", err)
	}
	clean, err := s.getStored("good-2222")
	if err != nil {
		t.Fatal(err)
	}
	findings, err := auditor.Ticket(clean)
	if err != nil {
		t.Fatalf("a clean ticket should audit: %v", err)
	}
	if !findings.Empty() {
		t.Errorf("clean ticket reported %+v", findings)
	}
	if _, err := auditor.Ticket(mk("beta/elsewhere-3333", StatusOpen)); err == nil {
		t.Error("a ticket from another project audited clean against this store's audit, rather than erroring")
	}

	// Across a central store, a ticket is judged by its own project's rules, so
	// a project this audit never prepared and a bare ID naming no project are
	// both refused rather than answered with no findings.
	ms, _ := testMultiStore(t, "alpha")
	multi, err := NewAuditor(ms)
	if err != nil {
		t.Fatalf("NewAuditor: %v", err)
	}
	for _, id := range []string{"beta/ghost-4444", "ghost-4444"} {
		findings, err := multi.Ticket(mk(id, StatusOpen))
		if err == nil {
			t.Errorf("ticket %s audited clean, want an error saying it could not be evaluated", id)
		}
		if !findings.Empty() {
			t.Errorf("ticket %s that could not be evaluated came back with %+v", id, findings)
		}
	}
}

func TestEpicStatusDriftIsIndependentOfHowTheEpicWasRead(t *testing.T) {
	// The stored side comes from the audit's own listing, so an epic that
	// arrived through an exported read — Get and List both derive, and they are
	// all a caller outside this package has — reports the drift its file holds
	// rather than comparing a derived status against itself and coming back
	// clean.
	s := NewProjectFileStore(t.TempDir(), "proj")
	writeLegacy(t, s, mkEpic("epic-1111", StatusBacklog, ""))
	writeLegacy(t, s, mkWithParent("good-2222", StatusOpen, "epic-1111"))

	auditor, err := NewAuditor(s)
	if err != nil {
		t.Fatalf("NewAuditor: %v", err)
	}
	stored, err := s.getStored("epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	want, err := auditor.Ticket(stored)
	if err != nil {
		t.Fatalf("Ticket: %v", err)
	}
	if want.EpicStatus == nil {
		t.Fatalf("the fixture stores backlog on an epic that derives otherwise, so the stored read should report drift, got %+v", want)
	}

	got, err := s.Get("epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	reads := map[string]*Ticket{"Get": got}
	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range listed {
		if tk.ID == "epic-1111" {
			reads["List"] = tk
		}
	}
	if reads["List"] == nil {
		t.Fatal("List did not yield the epic")
	}
	for name, tk := range reads {
		findings, err := auditor.Ticket(tk)
		if err != nil {
			t.Fatalf("Ticket after %s: %v", name, err)
		}
		if !reflect.DeepEqual(findings, want) {
			t.Errorf("epic read through %s audited to\n%+v\nbut the same epic read stored audits to\n%+v", name, findings, want)
		}
	}
}
