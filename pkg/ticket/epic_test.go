package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mkEpic(id string, status Status, parent string) *Ticket {
	t := mk(id, status)
	t.Type = TypeEpic
	t.Parent = parent
	return t
}

// multiDepStore is depStore's MultiStore twin: it creates the given tickets
// (namespaced IDs) across the named projects.
func multiDepStore(t *testing.T, projects []string, tickets ...*Ticket) *MultiStore {
	t.Helper()
	ms, _ := testMultiStore(t, projects...)
	for _, tk := range tickets {
		if err := ms.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", tk.ID, err)
		}
	}
	return ms
}

// setStatus reads a ticket, moves it to status, and writes it back the way an
// edit that set the status does — through SaveEdit, which is what carries a
// writer's intent.
func setStatus(t *testing.T, s Store, id string, status Status) error {
	t.Helper()
	tk, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	tk.Status = status
	_, err = SaveEdit(s, tk, true)
	return err
}

// ─── The derivation rule ───────────────────────────────────────────────────

func TestDeriveEpicStatus(t *testing.T) {
	cases := []struct {
		name      string
		abandoned bool
		children  []Status
		want      Status
	}{
		{"childless", false, nil, StatusBacklog},
		{"childless and abandoned", true, nil, StatusClosed},
		{"any child open", false, []Status{StatusDone, StatusOpen, StatusReady}, StatusOpen},
		{"every child done", false, []Status{StatusDone, StatusDone}, StatusDone},
		{"every child terminal, one closed", false, []Status{StatusDone, StatusClosed}, StatusClosed},
		{"every child closed", false, []Status{StatusClosed, StatusClosed}, StatusClosed},
		{"children ready but none open", false, []Status{StatusReady, StatusDone}, StatusBacklog},
		{"children backlog", false, []Status{StatusBacklog}, StatusBacklog},
		{"abandoned with every child done", true, []Status{StatusDone, StatusDone}, StatusClosed},
		{"abandoned with a non-terminal child", true, []Status{StatusClosed, StatusOpen}, StatusOpen},
		{"abandoned with a ready child", true, []Status{StatusReady}, StatusBacklog},
	}
	for _, c := range cases {
		var children []*Ticket
		for i, status := range c.children {
			children = append(children, mk(string(rune('a'+i)), status))
		}
		if got := DeriveEpicStatus(c.abandoned, children); got != c.want {
			t.Errorf("%s: DeriveEpicStatus(%v, %v) = %q, want %q", c.name, c.abandoned, c.children, got, c.want)
		}
	}
}

func TestDeriveEpicStatus_NeverReady(t *testing.T) {
	for _, children := range [][]Status{
		{StatusReady},
		{StatusReady, StatusDone},
		{StatusReady, StatusBacklog},
	} {
		var kids []*Ticket
		for i, status := range children {
			kids = append(kids, mk(string(rune('a'+i)), status))
		}
		if got := DeriveEpicStatus(false, kids); got == StatusReady {
			t.Errorf("children %v derived ready; an epic is never picked up directly", children)
		}
	}
}

// ─── Derivation through the store ──────────────────────────────────────────

func TestEpicStatusDerivedOnReadAndList(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusBacklog, "e-1"),
		mkWithParent("c-2", StatusBacklog, "e-1"),
	)

	// Only a child moved; nothing wrote the epic.
	if err := setStatus(t, s, "c-1", StatusOpen); err != nil {
		t.Fatal(err)
	}

	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("Get: epic status = %q, want %q", epic.Status, StatusOpen)
	}
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range all {
		if tk.ID == "e-1" && tk.Status != StatusOpen {
			t.Errorf("List: epic status = %q, want %q", tk.Status, StatusOpen)
		}
	}

	if err := setStatus(t, s, "c-1", StatusDone); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(t, s, "c-2", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, _ = s.Get("e-1")
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q once every child is done", epic.Status, StatusDone)
	}

	// One child abandoned rather than finished: the epic did not complete.
	if err := setStatus(t, s, "c-2", StatusClosed); err != nil {
		t.Fatal(err)
	}
	epic, _ = s.Get("e-1")
	if epic.Status != StatusClosed {
		t.Errorf("epic status = %q, want %q when a child was closed rather than done", epic.Status, StatusClosed)
	}
}

func TestEpicCompletedDerivedFromChildren(t *testing.T) {
	// Nothing writes an epic when its last child finishes, so the completion
	// date it renders is read off the children like the status.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	epic, _ := s.Get("e-1")
	if !epic.Completed.IsZero() {
		t.Errorf("unfinished epic completed = %v, want unset", epic.Completed)
	}

	if err := setStatus(t, s, "c-1", StatusDone); err != nil {
		t.Fatal(err)
	}
	child, _ := s.Get("c-1")
	epic, _ = s.Get("e-1")
	if !epic.Completed.Equal(child.Completed) {
		t.Errorf("epic completed = %v, want its last child's %v", epic.Completed, child.Completed)
	}

	// Reopening the child takes the date with the status: a completion date
	// beside a non-terminal epic is the stale value this replaces.
	if err := setStatus(t, s, "c-1", StatusOpen); err != nil {
		t.Fatal(err)
	}
	epic, _ = s.Get("e-1")
	if !epic.Completed.IsZero() {
		t.Errorf("epic completed = %v once a child reopened, want unset", epic.Completed)
	}
}

func TestEpicChildlessDerivesBacklog(t *testing.T) {
	s := depStore(t, mkEpic("e-1", StatusBacklog, ""))
	epic, _ := s.Get("e-1")
	if epic.Status != StatusBacklog {
		t.Errorf("childless epic status = %q, want %q", epic.Status, StatusBacklog)
	}
}

func TestEpicNeverAppearsInFrontier(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusReady, "e-1"),
	)
	frontier, err := FrontierTickets(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range frontier {
		if tk.Type == TypeEpic {
			t.Errorf("frontier holds epic %s [%s]; an epic never derives ready", tk.ID, tk.Status)
		}
	}
}

func TestEpicStatusDerivedThroughMultiStore(t *testing.T) {
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-1111", StatusBacklog, ""),
		mkWithParent("proj/child-2222", StatusBacklog, "proj/epic-1111"),
	)
	if err := setStatus(t, s, "proj/child-2222", StatusOpen); err != nil {
		t.Fatal(err)
	}
	epic, err := s.Get("proj/epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusOpen)
	}

	if err := setStatus(t, s, "proj/child-2222", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, _ = s.Get("proj/epic-1111")
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusDone)
	}
}

func TestEpicStatusDerivedFromBareStoredParent(t *testing.T) {
	// Tickets written before the namespacing rollout record a bare parent.
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-5555", StatusBacklog, ""),
		mkWithParent("proj/child-6666", StatusOpen, "epic-5555"),
	)
	epic, err := s.Get("proj/epic-5555")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusOpen)
	}
}

func TestEpicStatusIgnoresAnotherProjectsTickets(t *testing.T) {
	// Both projects hold an epic-7777, and only alpha's has a child.
	s := multiDepStore(t, []string{"alpha", "beta"},
		mkEpic("alpha/epic-7777", StatusBacklog, ""),
		mkEpic("beta/epic-7777", StatusBacklog, ""),
		mkWithParent("alpha/child-8888", StatusOpen, "alpha/epic-7777"),
	)
	for id, want := range map[string]Status{"alpha/epic-7777": StatusOpen, "beta/epic-7777": StatusBacklog} {
		epic, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if epic.Status != want {
			t.Errorf("%s status = %q, want %q", id, epic.Status, want)
		}
	}
}

func TestEpicIgnoresAnotherProjectsParentedTicket(t *testing.T) {
	// A ticket naming an epic in another project is not this store's child, even
	// though the bare halves match. The cascade writes what it matches, so a
	// namespace-tolerant match would grant a write over a foreign reference.
	ms, root := testMultiStore(t, "alpha", "beta")
	alpha := NewProjectFileStore(filepath.Join(root, "alpha"), "alpha")
	if err := alpha.Create(mkEpic("epic-7777", StatusBacklog, "")); err != nil {
		t.Fatal(err)
	}
	writeLegacy(t, alpha, mkWithParent("stray-8888", StatusOpen, "beta/epic-7777"))

	epic, err := ms.Get("alpha/epic-7777")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusBacklog {
		t.Errorf("epic status = %q, want %q — a ticket parented in another project is not a child", epic.Status, StatusBacklog)
	}

	if err := setStatus(t, ms, "alpha/epic-7777", StatusClosed); err != nil {
		t.Fatalf("closing the epic should succeed: %v", err)
	}
	stray, err := alpha.getStored("stray-8888")
	if err != nil {
		t.Fatal(err)
	}
	if stray.Status != StatusOpen {
		t.Errorf("stray-8888 = %q, want %q — the cascade wrote a ticket parented in another project", stray.Status, StatusOpen)
	}
}

// ─── Derivation over a store that could not be read in full ────────────────

func TestEpicDerivesFromAChildWithAMistypedField(t *testing.T) {
	// `abandoned: maybe` used to fail the whole parse, dropping a live child out
	// of its epic's derivation and leaving the epic reading done over work in
	// progress. The field is lost; the ticket is not.
	dir := t.TempDir()
	s := NewProjectFileStore(dir, "proj")
	if err := s.Create(mkEpic("epic-1111", StatusBacklog, "")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(mkWithParent("done-2222", StatusDone, "epic-1111")); err != nil {
		t.Fatal(err)
	}
	planted := "---\nid: live-3333\nstatus: open\nabandoned: maybe\ndeps: []\nlinks: []\n" +
		"created: 2026-01-01T00:00:00Z\ntype: feature\npriority: 2\nparent: epic-1111\n---\n# Live child\n"
	if err := os.WriteFile(filepath.Join(dir, "live-3333.md"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}

	epic, err := s.Get("epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q — a live child with a mistyped field is still a live child", epic.Status, StatusOpen)
	}
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]Status{}
	for _, tk := range all {
		listed[tk.ID] = tk.Status
	}
	if listed["live-3333"] != StatusOpen {
		t.Errorf("List = %v, want live-3333 shown as open", listed)
	}
	if listed["epic-1111"] != StatusOpen {
		t.Errorf("List reads the epic %q, want %q — it must agree with Get", listed["epic-1111"], StatusOpen)
	}
}

func TestEpicNeverTerminalWhileAFileCannotBeRead(t *testing.T) {
	// An unreadable file could be any epic's child, so "every child is terminal"
	// is not a claim the store can make. done and closed both degrade to backlog
	// — visibly, beside the warning List emits — rather than reading finished
	// over a ticket nobody could read.
	dir := t.TempDir()
	s := NewProjectFileStore(dir, "proj")
	for _, tk := range []*Ticket{
		mkEpic("done-1111", StatusBacklog, ""),
		mkWithParent("child-2222", StatusDone, "done-1111"),
		mkEpic("closed-3333", StatusBacklog, ""),
		mkWithParent("child-4444", StatusClosed, "closed-3333"),
		mkEpic("live-5555", StatusBacklog, ""),
		mkWithParent("child-6666", StatusOpen, "live-5555"),
	} {
		if err := s.Create(tk); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string]Status{"done-1111": StatusDone, "closed-3333": StatusClosed, "live-5555": StatusOpen} {
		if epic, _ := s.Get(id); epic.Status != want {
			t.Fatalf("%s = %q before the planted file, want %q", id, epic.Status, want)
		}
	}

	plantUnreadable(t, dir, "broken-9999.md")
	captureWarnings(t)

	want := map[string]Status{
		"done-1111":   StatusBacklog,
		"closed-3333": StatusBacklog,
		// Nothing degrades an epic a parsed child already holds open: the phantom
		// child is of unknown status, which cannot make an epic more finished.
		"live-5555": StatusOpen,
	}
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]Status{}
	for _, tk := range all {
		listed[tk.ID] = tk.Status
	}
	for id, status := range want {
		if listed[id] != status {
			t.Errorf("List: %s = %q, want %q", id, listed[id], status)
		}
		// A single read has to agree with the listing, or a `tk show` would
		// contradict the board.
		epic, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if epic.Status != status {
			t.Errorf("Get: %s = %q, want %q", id, epic.Status, status)
		}
		if status == StatusBacklog && !epic.Completed.IsZero() {
			t.Errorf("Get: %s carries completed %v beside a degraded status", id, epic.Completed)
		}
	}
}

func TestEpicNeverTerminalWithAnUnusableChildFile(t *testing.T) {
	// The two shapes yaml.v3 reports as the same TypeError a mistyped local field
	// produces, and that leniency would therefore have waved through. Each would
	// leave the epic reading done over the live child the file actually holds —
	// the doubled key by decoding nothing at all, the sequence parent by decoding
	// everything except the link to the epic. Neither is visible on the ticket,
	// so both count as unreadable and the epic degrades instead.
	cases := []struct {
		name    string
		planted string
	}{
		{
			// What a hand-resolved merge conflict leaves in the synced store.
			name: "duplicate key",
			planted: "---\nid: live-3333\nstatus: open\ndeps: []\nlinks: []\ncreated: 2026-01-01T00:00:00Z\n" +
				"type: feature\npriority: 2\nparent: epic-1111\nstatus: open\n---\n# Merge-conflicted child\n",
		},
		{
			name: "parent as a sequence",
			planted: "---\nid: live-3333\nstatus: open\ndeps: []\nlinks: []\ncreated: 2026-01-01T00:00:00Z\n" +
				"type: feature\npriority: 2\nparent: [epic-1111]\n---\n# Child with a lost parent\n",
		},
	}
	for _, c := range cases {
		dir := t.TempDir()
		s := NewProjectFileStore(dir, "proj")
		if err := s.Create(mkEpic("epic-1111", StatusBacklog, "")); err != nil {
			t.Fatal(err)
		}
		if err := s.Create(mkWithParent("done-2222", StatusDone, "epic-1111")); err != nil {
			t.Fatal(err)
		}
		if epic, _ := s.Get("epic-1111"); epic.Status != StatusDone {
			t.Fatalf("%s: epic = %q before the planted file, want %q", c.name, epic.Status, StatusDone)
		}

		if err := os.WriteFile(filepath.Join(dir, "live-3333.md"), []byte(c.planted), 0o644); err != nil {
			t.Fatal(err)
		}
		warnings := captureWarnings(t)

		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, tk := range all {
			if tk.ID == "" {
				t.Errorf("%s: List holds a blank ticket read from the planted file: %+v", c.name, tk)
			}
			if tk.ID == "epic-1111" && tk.Status != StatusBacklog {
				t.Errorf("%s: List: epic = %q, want %q — a file it could not read may be a child", c.name, tk.Status, StatusBacklog)
			}
		}
		if epic, _ := s.Get("epic-1111"); epic.Status != StatusBacklog {
			t.Errorf("%s: Get: epic = %q, want %q", c.name, epic.Status, StatusBacklog)
		}
		if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "live-3333.md") {
			t.Errorf("%s: warnings = %v, want the planted file named once", c.name, *warnings)
		}
	}
}

func TestEpicFileWithALostTypeIsNotSilentlyStale(t *testing.T) {
	// An epic's own type is load-bearing in a way its other fields are not:
	// `type: [epic]` is a tolerated TypeError that leaves the ticket typeless, so
	// the derivation passes over it entirely and the file's stored status — here
	// a done a previous write baked in — renders unchanged over a live child,
	// with the demotion never consulted. Refusing the file is what puts it back
	// under the rule: it becomes a skip, so nothing shows a stale done and every
	// epic in the project degrades.
	dir := t.TempDir()
	s := NewProjectFileStore(dir, "proj")
	if err := s.Create(mkWithParent("child-2222", StatusOpen, "")); err != nil {
		t.Fatal(err)
	}
	planted := "---\nid: epic-1111\nstatus: done\ndeps: []\nlinks: []\ncreated: 2026-01-01T00:00:00Z\n" +
		"type: [epic]\npriority: 2\n---\n# Epic with a lost type\n"
	if err := os.WriteFile(filepath.Join(dir, "epic-1111.md"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	warnings := captureWarnings(t)

	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range all {
		if tk.ID == "" {
			t.Errorf("List holds a blank ticket read from the planted file: %+v", tk)
		}
		if tk.ID == "epic-1111" {
			t.Errorf("List holds the planted epic as %+v; a file that lost its type is not readable", tk)
		}
	}
	if len(all) != 1 || all[0].ID != "child-2222" {
		t.Errorf("List = %d tickets, want only the child that parses", len(all))
	}
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "epic-1111.md") {
		t.Errorf("warnings = %v, want the planted file named once", *warnings)
	}
}

func TestAuditReportsUnreadableFile(t *testing.T) {
	// The audit reads through the same listing, so without this it would call a
	// store clean that it never read in full — and it would compare against a
	// derived status no reader is shown.
	dir := t.TempDir()
	s := NewProjectFileStore(dir, "proj")
	writeLegacy(t, s, mkEpic("epic-1111", StatusDone, ""))
	writeLegacy(t, s, mkWithParent("child-2222", StatusDone, "epic-1111"))
	plantUnreadable(t, dir, "broken-9999.md")
	captureWarnings(t)

	report, err := Audit(s)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(report.SkippedFiles) != 1 {
		t.Fatalf("skipped files = %+v, want the one file the audit could not read", report.SkippedFiles)
	}
	skip := report.SkippedFiles[0]
	if skip.File != "broken-9999.md" || skip.Project != "proj" || skip.Error == "" {
		t.Errorf("skip = %+v, want the project, the file and a reason", skip)
	}
	// The epic stores done and its parsed children are all done, so without the
	// degradation there would be no drift to report at all.
	if len(report.EpicStatus) != 1 {
		t.Fatalf("epic status = %+v, want the epic reported against the degraded value", report.EpicStatus)
	}
	if got := report.EpicStatus[0]; got.ID != "epic-1111" || got.Derived != StatusBacklog || got.Stored != StatusDone {
		t.Errorf("drift = %+v, want epic-1111 stored done reading backlog", got)
	}
}

// ─── Manual status ─────────────────────────────────────────────────────────

func TestEpicManualStatusRejectedOnUpdate(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	for _, status := range []Status{StatusDone, StatusReady, StatusBacklog} {
		err := setStatus(t, s, "e-1", status)
		if err == nil {
			t.Fatalf("expected setting epic status to %q to be rejected, got nil", status)
		}
		if !strings.Contains(err.Error(), "e-1") || !strings.Contains(err.Error(), "derived from its children") {
			t.Errorf("error should name the epic and say the status is derived, got: %v", err)
		}
		if !strings.Contains(err.Error(), "closed") {
			t.Errorf("error should say the epic can be closed to abandon it, got: %v", err)
		}
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("rejected writes changed the epic to %q, want the derived %q", epic.Status, StatusOpen)
	}
}

func TestEpicManualStatusRejectedOnCreate(t *testing.T) {
	s := depStore(t)
	err := s.Create(mkEpic("e-1", StatusReady, ""))
	if err == nil {
		t.Fatal("expected creating an epic as ready to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "derived from its children") {
		t.Errorf("error should say the status is derived, got: %v", err)
	}
}

func TestEpicRoundTripWriteAccepted(t *testing.T) {
	// A read-modify-write carries the status the epic derives to. That is not a
	// status change and must not be refused — otherwise no field of an epic
	// could be edited.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	epic, _ := s.Get("e-1")
	epic.Title = "Renamed"
	if err := s.Update(epic); err != nil {
		t.Fatalf("editing an epic should not be refused: %v", err)
	}
	got, _ := s.Get("e-1")
	if got.Title != "Renamed" || got.Status != StatusOpen {
		t.Errorf("epic = %q [%s], want the edit applied and status %q", got.Title, got.Status, StatusOpen)
	}
}

func TestEpicPromotionKeepsTheStatusItWasRead(t *testing.T) {
	// Promoting a ticket to an epic is one ordinary edit. The status it carries
	// back is the one it was read with, not a status chosen for an epic, so
	// nothing about it may be refused — and the epic then reads what a childless
	// epic derives.
	s := depStore(t, mk("feat-1111", StatusOpen))
	tk, _ := s.Get("feat-1111")
	tk.Type = TypeEpic
	if _, err := SaveEdit(s, tk, false); err != nil {
		t.Fatalf("promoting a ticket to an epic should not be refused: %v", err)
	}
	got, _ := s.Get("feat-1111")
	if got.Type != TypeEpic {
		t.Errorf("type = %q, want %q", got.Type, TypeEpic)
	}
	if got.Status != StatusBacklog {
		t.Errorf("status = %q, want %q — a childless epic derives backlog", got.Status, StatusBacklog)
	}
}

func TestEpicPromotionRecordsAStatusSetWithIt(t *testing.T) {
	// The promotion carries a status the writer set in the same edit, which is a
	// status set on the epic this write is making. closed is the abandon it
	// looks like, recorded rather than dropped on the way through.
	s := depStore(t, mk("feat-1111", StatusOpen))
	tk, _ := s.Get("feat-1111")
	tk.Type = TypeEpic
	tk.Status = StatusClosed
	if _, err := SaveEdit(s, tk, true); err != nil {
		t.Fatalf("promoting a ticket and abandoning it should be accepted: %v", err)
	}
	if stored, _ := s.getStored("feat-1111"); !stored.Abandoned {
		t.Error("a closed set with the promotion recorded no abandon intent")
	}
	got, _ := s.Get("feat-1111")
	if got.Status != StatusClosed {
		t.Errorf("status = %q, want %q — the epic was abandoned as it was promoted", got.Status, StatusClosed)
	}
}

func TestEpicPromotionRefusesAnotherStatusSetWithIt(t *testing.T) {
	// Any other status set on the epic the write is making is refused the same
	// way it is on an epic that already existed — an epic's status is not a
	// thing to set, whichever edit made it an epic.
	s := depStore(t, mk("feat-2222", StatusOpen))
	tk, _ := s.Get("feat-2222")
	tk.Type = TypeEpic
	tk.Status = StatusReady
	_, err := SaveEdit(s, tk, true)
	if err == nil {
		t.Fatal("expected a status set alongside the promotion to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "feat-2222") || !strings.Contains(err.Error(), "derived from its children") {
		t.Errorf("error should name the ticket and say the status is derived, got: %v", err)
	}
	stored, _ := s.getStored("feat-2222")
	if stored.Type == TypeEpic {
		t.Error("the refused write promoted the ticket anyway")
	}
}

func TestEpicStatusRefusedWhenItEqualsTheDerivedOne(t *testing.T) {
	// Refusing is right — an epic's status is still not settable — but the
	// remedy "change the children" is nonsense for a value the children already
	// produce, so that case says what it means instead.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	err := setStatus(t, s, "e-1", StatusOpen)
	if err == nil {
		t.Fatal("expected setting an epic to the status it already reads to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "already reads open") {
		t.Errorf("error should say the epic already reads that status, got: %v", err)
	}
	if !strings.Contains(err.Error(), "e-1") || !strings.Contains(err.Error(), "derived from its children") {
		t.Errorf("error should name the epic and say the status is derived, got: %v", err)
	}
}

func TestEpicCreateAsClosedNamesTheEditThatAbandonsIt(t *testing.T) {
	// closed is the one status changing the children cannot produce on a new
	// epic, so the refusal cannot name them as the remedy.
	s := depStore(t)
	err := s.Create(mkEpic("e-1", StatusClosed, ""))
	if err == nil {
		t.Fatal("expected creating an epic as closed to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "tk edit e-1 --status closed") {
		t.Errorf("error should name the edit that abandons it, got: %v", err)
	}
}

func TestNonEpicStatusUnaffected(t *testing.T) {
	// A feature with a child (only a store predating the one-level rule holds
	// one) still moves freely: derivation is an epic rule.
	s := depStore(t, mk("feat", StatusOpen))
	writeLegacy(t, s, mkWithParent("sub", StatusOpen, "feat"))
	tk, _ := s.Get("feat")
	tk.Status = StatusDone
	if err := s.Update(tk); err != nil {
		t.Fatalf("non-epic status should not be derived, got: %v", err)
	}
}

// ─── Closing an epic ───────────────────────────────────────────────────────

func TestEpicCloseCascadesToChildren(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
		mkWithParent("c-2", StatusReady, "e-1"),
		mkWithParent("c-3", StatusDone, "e-1"),
	)
	if err := setStatus(t, s, "e-1", StatusClosed); err != nil {
		t.Fatalf("closing an epic should succeed: %v", err)
	}

	want := map[string]Status{"c-1": StatusClosed, "c-2": StatusClosed, "c-3": StatusDone}
	for id, status := range want {
		child, _ := s.Get(id)
		if child.Status != status {
			t.Errorf("child %s = %q, want %q", id, child.Status, status)
		}
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusClosed {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusClosed)
	}
}

func TestEpicCloseReportsTheChildrenItClosed(t *testing.T) {
	// A write that mutates other tickets says which on success too, not only
	// when part of the cascade fails.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
		mkWithParent("c-2", StatusDone, "e-1"),
	)
	epic, _ := s.Get("e-1")
	epic.Status = StatusClosed
	closed, err := SaveEdit(s, epic, true)
	if err != nil {
		t.Fatalf("closing an epic should succeed: %v", err)
	}
	if len(closed) != 1 || closed[0] != "c-1" {
		t.Errorf("closed = %v, want [c-1] — the finished child kept its record", closed)
	}
	note := ClosedChildrenNote(closed)
	if !strings.Contains(note, "c-1") || !strings.Contains(note, "1 child ticket(s)") {
		t.Errorf("note = %q, want it to count and name the closed children", note)
	}

	// An edit that closed nothing adds nothing to what its caller reports.
	epic, _ = s.Get("e-1")
	epic.Title = "Renamed"
	closed, err = SaveEdit(s, epic, false)
	if err != nil {
		t.Fatal(err)
	}
	if note := ClosedChildrenNote(closed); note != "" {
		t.Errorf("note = %q for an edit that closed nothing, want empty", note)
	}
}

func TestEpicClosePartialFailureNamesChildren(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only file mode this test relies on")
	}
	dir := t.TempDir()
	s := NewFileStore(dir)
	for _, tk := range []*Ticket{
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
		mkWithParent("c-2", StatusOpen, "e-1"),
	} {
		if err := s.Create(tk); err != nil {
			t.Fatal(err)
		}
	}
	unwritable := filepath.Join(dir, "c-2.md")
	if err := os.Chmod(unwritable, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unwritable, 0o644) })

	err := setStatus(t, s, "e-1", StatusClosed)
	if err == nil {
		t.Fatal("expected a partial cascade to report an error, got nil")
	}
	if !strings.Contains(err.Error(), "c-2") {
		t.Errorf("error should name the child that was not closed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "c-1") {
		t.Errorf("error should name the child that was closed, got: %v", err)
	}
	if closed, _ := s.Get("c-1"); closed.Status != StatusClosed {
		t.Errorf("c-1 = %q, want the cascade to have closed it before it failed on c-2", closed.Status)
	}
}

func TestEpicCloseIsUnrepresentableWithNonTerminalChild(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	if err := setStatus(t, s, "e-1", StatusClosed); err != nil {
		t.Fatal(err)
	}

	// Reopening a child un-closes the epic: the abandon intent is honoured only
	// while every child is terminal.
	if err := setStatus(t, s, "c-1", StatusOpen); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q while a child is not terminal", epic.Status, StatusOpen)
	}

	// Finishing the child again brings the intent back — it was never cleared.
	if err := setStatus(t, s, "c-1", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, _ = s.Get("e-1")
	if epic.Status != StatusClosed {
		t.Errorf("epic status = %q, want %q once every child is terminal again", epic.Status, StatusClosed)
	}
}

func TestEpicAbandonSurvivesAnUnrelatedEdit(t *testing.T) {
	// The flag is the whole record of the abandon, and an edit to any other
	// field carries back whatever the epic derived to when it was read. Dropping
	// the flag in favour of that status would silently un-abandon the epic.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	if err := setStatus(t, s, "e-1", StatusClosed); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(t, s, "c-1", StatusOpen); err != nil {
		t.Fatal(err)
	}

	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Fatalf("epic status = %q, want %q — the reopened child is what an editor reads", epic.Status, StatusOpen)
	}
	// Both write paths: the edit path a person or agent goes through, and the
	// generic read-modify-write behind ticket_add_note, ticket_dep and tk verify.
	epic.Notes = append(epic.Notes, Note{Text: "unrelated"})
	if _, err := SaveEdit(s, epic, false); err != nil {
		t.Fatalf("editing an abandoned epic should not be refused: %v", err)
	}
	epic, _ = s.Get("e-1")
	epic.Tags = append(epic.Tags, "unrelated")
	if err := s.Update(epic); err != nil {
		t.Fatalf("updating an abandoned epic should not be refused: %v", err)
	}
	if stored, _ := s.getStored("e-1"); !stored.Abandoned {
		t.Error("an unrelated edit dropped the abandon intent")
	}
	if child, _ := s.Get("c-1"); child.Status != StatusOpen {
		t.Errorf("child = %q after an edit to the epic, want %q — only an abandon cascades", child.Status, StatusOpen)
	}

	if err := setStatus(t, s, "c-1", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, _ = s.Get("e-1")
	if epic.Status != StatusClosed {
		t.Errorf("epic status = %q, want %q — an unrelated edit must not drop the abandon intent", epic.Status, StatusClosed)
	}
}

func TestEpicUnrelatedEditInventsNoAbandon(t *testing.T) {
	// An epic whose children are every one terminal, one of them closed, derives
	// closed with nobody having abandoned it. That is the status every reader is
	// handed and every read-modify-write carries back, so recording it as an
	// intent would forge one — and the forgery only surfaces later, when the
	// closed child is reopened and finished and the epic still reads closed.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusDone, "e-1"),
		mkWithParent("c-2", StatusClosed, "e-1"),
	)
	epic, _ := s.Get("e-1")
	if epic.Status != StatusClosed {
		t.Fatalf("epic status = %q, want %q from its children alone", epic.Status, StatusClosed)
	}

	epic.Notes = append(epic.Notes, Note{Text: "unrelated"})
	if _, err := SaveEdit(s, epic, false); err != nil {
		t.Fatalf("editing the epic should not be refused: %v", err)
	}
	epic, _ = s.Get("e-1")
	epic.Tags = append(epic.Tags, "unrelated")
	if err := s.Update(epic); err != nil {
		t.Fatalf("updating the epic should not be refused: %v", err)
	}
	if stored, _ := s.getStored("e-1"); stored.Abandoned {
		t.Error("an unrelated edit recorded an abandon intent nobody expressed")
	}

	if err := setStatus(t, s, "c-2", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, _ = s.Get("e-1")
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q once every child finished", epic.Status, StatusDone)
	}
}

func TestEpicCompletedIsNeverStored(t *testing.T) {
	// The completion date is derived from the children like the status, so the
	// epic's own file holds none: a date stamped from the writer's clock would
	// surface on an epic that never finished anything.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Fatalf("epic status = %q, want %q", epic.Status, StatusOpen)
	}
	// A writer carrying a completion date of its own — the shape a status field
	// stale enough to read terminal used to leave behind.
	epic.Completed = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Update(epic); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.getStored("e-1")
	if !stored.Completed.IsZero() {
		t.Errorf("stored completed = %v, want unset — an epic's is derived", stored.Completed)
	}
	epic, _ = s.Get("e-1")
	if !epic.Completed.IsZero() {
		t.Errorf("epic completed = %v while it reads %q, want unset", epic.Completed, epic.Status)
	}
}

func TestEpicUnabandonTakesItBackUp(t *testing.T) {
	// Abandoning an epic whose children had all finished closes nothing, so the
	// stored intent is all that makes it read closed. Setting it to what the
	// children imply is the way back.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusDone, "e-1"),
	)
	if err := setStatus(t, s, "e-1", StatusClosed); err != nil {
		t.Fatal(err)
	}
	if epic, _ := s.Get("e-1"); epic.Status != StatusClosed {
		t.Fatalf("epic status = %q, want %q", epic.Status, StatusClosed)
	}
	if child, _ := s.Get("c-1"); child.Status != StatusDone {
		t.Errorf("child = %q, want %q — a finished child keeps its record", child.Status, StatusDone)
	}

	if err := setStatus(t, s, "e-1", StatusDone); err != nil {
		t.Fatalf("un-abandoning an epic should be accepted: %v", err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q once the intent is taken back", epic.Status, StatusDone)
	}
}

func TestEpicUnabandonWhileAChildIsLive(t *testing.T) {
	// An abandoned epic with a reopened child derives open, so the abandon is
	// nowhere in the status a writer sees. Taking it back has to work from there
	// too — otherwise the flag is unreachable until every child is terminal
	// again, at which point the epic snaps back to closed.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusDone, "e-1"),
	)
	if err := setStatus(t, s, "e-1", StatusClosed); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(t, s, "c-1", StatusOpen); err != nil {
		t.Fatal(err)
	}

	if err := setStatus(t, s, "e-1", StatusOpen); err != nil {
		t.Fatalf("un-abandoning an epic with a live child should be accepted: %v", err)
	}
	if stored, _ := s.getStored("e-1"); stored.Abandoned {
		t.Error("the abandon intent survived a status the writer set to take it back")
	}
	if err := setStatus(t, s, "c-1", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q — the abandon was taken back before the child finished", epic.Status, StatusDone)
	}
}

func TestEpicEditCarryingAStaleStatusDecidesNothing(t *testing.T) {
	// An editor reads an epic, the children move under it, and the edit lands
	// carrying the status the editor was shown. That status is a decision about
	// nothing: a stale closed must not abandon the epic and cascade into the
	// child that just reopened, and a stale open must not be refused as a status
	// set by hand on a field the writer never touched.
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusDone, "e-1"),
		mkWithParent("c-2", StatusClosed, "e-1"),
	)
	epic, _ := s.Get("e-1")
	if epic.Status != StatusClosed {
		t.Fatalf("epic status = %q, want %q from its children alone", epic.Status, StatusClosed)
	}

	// The child reopens between the read and the write.
	if err := setStatus(t, s, "c-2", StatusOpen); err != nil {
		t.Fatal(err)
	}
	epic.Title = "Renamed"
	if _, err := SaveEdit(s, epic, false); err != nil {
		t.Fatalf("an edit carrying a stale status should not be refused: %v", err)
	}
	if stored, _ := s.getStored("e-1"); stored.Abandoned {
		t.Error("an edit carrying a stale closed recorded an abandon nobody expressed")
	}
	if child, _ := s.Get("c-2"); child.Status != StatusOpen {
		t.Errorf("child = %q, want %q — a stale status cascaded", child.Status, StatusOpen)
	}

	// The mirror: the epic read open, its last live child finished, and the edit
	// lands carrying open on an epic that now reads done.
	epic, _ = s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Fatalf("epic status = %q, want %q", epic.Status, StatusOpen)
	}
	if err := setStatus(t, s, "c-2", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic.Notes = append(epic.Notes, Note{Text: "unrelated"})
	if _, err := SaveEdit(s, epic, false); err != nil {
		t.Fatalf("an edit carrying a status the children moved past should not be refused: %v", err)
	}
	if got, _ := s.Get("e-1"); got.Status != StatusDone {
		t.Errorf("epic status = %q, want %q", got.Status, StatusDone)
	}
}

func TestEpicCloseCascadesThroughMultiStore(t *testing.T) {
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-1111", StatusBacklog, ""),
		mkWithParent("proj/child-2222", StatusOpen, "proj/epic-1111"),
	)
	epic, err := s.Get("proj/epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	epic.Status = StatusClosed
	closed, err := SaveEdit(s, epic, true)
	if err != nil {
		t.Fatalf("closing an epic should succeed: %v", err)
	}
	if len(closed) != 1 || closed[0] != "proj/child-2222" {
		t.Errorf("closed = %v, want [proj/child-2222] — reported as namespaced, like every ID this store hands back", closed)
	}
	child, err := s.Get("proj/child-2222")
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != StatusClosed {
		t.Errorf("child status = %q, want %q", child.Status, StatusClosed)
	}
	epic, _ = s.Get("proj/epic-1111")
	if epic.Status != StatusClosed {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusClosed)
	}
}

// ─── Migration ─────────────────────────────────────────────────────────────

func TestAuditReportsEpicStatusDrift(t *testing.T) {
	// Stored statuses were left in place and are no longer read, so the audit is
	// the only thing that can still compare the two.
	dir := t.TempDir()
	s := NewProjectFileStore(dir, "proj")
	writeLegacy(t, s, mkEpic("abandoned-1111", StatusClosed, ""))
	writeLegacy(t, s, mkEpic("stale-2222", StatusDone, ""))
	writeLegacy(t, s, mkWithParent("child-3333", StatusOpen, "stale-2222"))
	writeLegacy(t, s, mkEpic("agrees-4444", StatusBacklog, ""))
	recorded := mkEpic("recorded-5555", StatusClosed, "")
	recorded.Abandoned = true
	writeLegacy(t, s, recorded)

	before := storeFiles(t, dir)

	report, err := Audit(s)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}

	got := map[string]EpicStatusDrift{}
	for _, d := range report.EpicStatus {
		got[d.ID] = d
	}
	want := map[string]EpicStatusDrift{
		// Stores closed with no flag beside it — possibly a hand-close under the
		// old model, possibly a derived value a write carried into the file. The
		// audit separates the candidates out without calling them abandons.
		"abandoned-1111": {ID: "abandoned-1111", Stored: StatusClosed, Derived: StatusBacklog, Kind: EpicDriftStoredClosed},
		"stale-2222":     {ID: "stale-2222", Stored: StatusDone, Derived: StatusOpen, Kind: EpicDriftStale},
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("drift for %s = %+v, want %+v", id, got[id], w)
		}
	}
	// recorded-5555 is the remedy already applied: a re-recorded abandon derives
	// the status its file stores and drops out of the report.
	for _, id := range []string{"agrees-4444", "recorded-5555", "child-3333"} {
		if _, ok := got[id]; ok {
			t.Errorf("audit reported %s, which reads what its file stores", id)
		}
	}
	if len(report.EpicStatus) != len(want) {
		t.Errorf("audit reported %d epics, want %d: %+v", len(report.EpicStatus), len(want), report.EpicStatus)
	}

	after := storeFiles(t, dir)
	for name, data := range before {
		if after[name] != data {
			t.Errorf("audit rewrote %s", name)
		}
	}
}

// storeFiles reads every ticket file in dir, keyed by filename, so an audit can
// be shown to have rewritten none of them.
func storeFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
	}
	return files
}
