package ticket

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// generic creates a generic ticket in the given status.
func generic(t *testing.T, s Store, id string, status Status, deps ...string) {
	t.Helper()
	tk := mk(id, status, deps...)
	tk.WorkflowKind = WorkflowGeneric
	mustCreate(t, s, tk)
}

func markReq(id, action string, result MarkResult) ActionRequest {
	return ActionRequest{ID: id, ActionID: action, Note: "note " + action, Transition: TransitionMark, Mark: result}
}

func TestMarkDoneFinishesGenericWithNote(t *testing.T) {
	root, ms := centralFixture(t, true)
	s := nsStore(root, "warp")
	generic(t, s, "g-1", StatusReady)
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Truncate(time.Second)
	req := markReq("warp/g-1", "mark-1", MarkDone)
	req.Note = "Installed build 412 on the phone.\n---\nstatus: open \"quoted\""
	res := mustApply(t, WithSource(ms, "weft"), req)
	got := mustGet(t, s, "g-1")
	if got.Status != StatusDone || res.Receipt.Outcome != "done" || got.ClosedAt().Before(before) {
		t.Errorf("marked done to %s, closed %v, receipt %+v", got.Status, got.ClosedAt(), res.Receipt)
	}
	want := MarkRow{Result: MarkDone, Note: req.Note, At: res.Receipt.At, By: "weft", Host: host, Prior: StatusReady}
	if last := LastMark(got); last == nil || *last != want {
		t.Errorf("last mark = %+v, want %+v", last, want)
	}
	if at, err := time.Parse(time.RFC3339, want.At); err != nil || at.Before(before) {
		t.Errorf("mark at %q: %v", want.At, err)
	}
	if len(got.Notes) != 1 || got.Notes[0].Text != req.Note {
		t.Errorf("notes = %v, want the mark's note", got.Notes)
	}
	if !bytes.Contains(readTicketFile(t, s, "g-1"), []byte("\nmarks:\n")) {
		t.Errorf("no marks block in:\n%s", readTicketFile(t, s, "g-1"))
	}
	if again := mustApply(t, ms, req); !again.Replayed || again.Receipt != res.Receipt {
		t.Errorf("a retry = %+v, want the recorded receipt replayed", again)
	}
	other := req
	other.Mark = MarkNotNeeded
	if _, err := ApplyAction(ms, other); !errors.Is(err, ErrActionConflict) {
		t.Errorf("another result under the same id = %v, want an action conflict", err)
	}
}

func TestMarkNeedsNote(t *testing.T) {
	s, _ := testStore(t)
	generic(t, s, "g-1", StatusReady)
	for _, result := range []MarkResult{MarkDone, MarkFailed, MarkNotNeeded} {
		for _, note := range []string{"", " \n\t"} {
			req := markReq("g-1", "mark", result)
			req.Note = note
			refused(t, s, req, nil)
		}
	}
	if got := mustGet(t, s, "g-1"); got.Status != StatusReady || LastMark(got) != nil || len(got.Actions) != 0 {
		t.Errorf("a refused mark left %s, %+v, %v", got.Status, LastMark(got), got.Actions)
	}
}

func TestMarkFailedLeavesStatus(t *testing.T) {
	s, _ := testStore(t)
	for _, status := range []Status{StatusBacklog, StatusReady, StatusOpen} {
		id := "g-" + string(status)
		generic(t, s, id, status)
		res := mustApply(t, WithSource(s, "weft"), markReq(id, "fail", MarkFailed))
		got := mustGet(t, s, id)
		if got.Status != status || res.Receipt.Outcome != string(status) || !got.ClosedAt().IsZero() {
			t.Errorf("a failed mark moved %s to %s (outcome %s)", id, got.Status, res.Receipt.Outcome)
		}
		if last := LastMark(got); last == nil || last.Result != MarkFailed || last.Note != "note fail" || last.Prior != status || last.By != "weft" || last.Host == "" {
			t.Errorf("%s: last mark = %+v", id, last)
		}
	}
}

func TestMarkNotNeededCloses(t *testing.T) {
	s, _ := testStore(t)
	generic(t, s, "g-1", StatusReady)
	mustCreate(t, s, mk("c-1", StatusReady, "g-1"))
	if !IsBlocked(s, mustGet(t, s, "c-1")) {
		t.Fatal("c-1 is not held by the generic ticket it depends on")
	}

	res := mustApply(t, s, markReq("g-1", "skip", MarkNotNeeded))
	got := mustGet(t, s, "g-1")
	if got.Status != StatusClosed || res.Receipt.Outcome != "closed" || got.ClosedAt().IsZero() {
		t.Errorf("not needed left %s, closed %v", got.Status, got.ClosedAt())
	}
	if last := LastMark(got); last == nil || last.Result != MarkNotNeeded || last.Prior != StatusReady {
		t.Errorf("last mark = %+v", last)
	}
	if IsBlocked(s, mustGet(t, s, "c-1")) {
		t.Error("c-1 is still held by a ticket marked not needed")
	}
	ready, err := ReadyTickets(s)
	if err != nil {
		t.Fatal(err)
	}
	if !listedIDs(ready)["c-1"] {
		t.Errorf("ready = %v, want c-1 listed", listedIDs(ready))
	}
}

func TestMarkRefusedOnCodingTicket(t *testing.T) {
	s, _ := testStore(t)
	mustCreate(t, s, mk("absent", StatusReady))
	coding := mk("coding", StatusOpen)
	coding.WorkflowKind = WorkflowCoding
	mustCreate(t, s, coding)
	epic := mkEpic("epic", StatusBacklog, "")
	epic.WorkflowKind = WorkflowGeneric
	mustCreate(t, s, epic)
	for _, id := range []string{"absent", "coding", "epic"} {
		for _, result := range []MarkResult{MarkDone, MarkFailed, MarkNotNeeded} {
			refused(t, s, markReq(id, "mark", result), ErrTransitionNotPermitted)
		}
		unmark := markReq(id, "unmark", "")
		unmark.Transition = TransitionUnmark
		refused(t, s, unmark, ErrTransitionNotPermitted)
	}

	// A mark result belongs to mark alone, and mark needs one tk records.
	generic(t, s, "g-1", StatusReady)
	for _, result := range []MarkResult{"", "passed", "not-needed", "Done"} {
		refused(t, s, markReq("g-1", "mark", result), nil)
	}
	for _, tr := range []Transition{TransitionNone, TransitionReopen, TransitionUnmark} {
		req := markReq("g-1", "x", MarkDone)
		req.Transition = tr
		refused(t, s, req, nil)
	}
}

func TestUnmarkRestores(t *testing.T) {
	s, _ := testStore(t)
	generic(t, s, "g-1", StatusOpen)
	unmark := func(action string) ActionRequest {
		req := markReq("g-1", action, "")
		req.Transition = TransitionUnmark
		return req
	}
	refused(t, s, unmark("x"), ErrTransitionNotPermitted)

	mustApply(t, s, markReq("g-1", "fail", MarkFailed))
	failed := *LastMark(mustGet(t, s, "g-1"))
	mustApply(t, s, markReq("g-1", "done", MarkDone))

	// Undoing the done restores open and leaves the failed mark standing.
	res := mustApply(t, s, unmark("undo-1"))
	got := mustGet(t, s, "g-1")
	if got.Status != StatusOpen || res.Receipt.Outcome != "open" || !got.Completed.IsZero() {
		t.Errorf("unmark left %s, completed %v", got.Status, got.Completed)
	}
	if last := LastMark(got); last == nil || *last != failed {
		t.Errorf("last mark after unmark = %+v, want %+v", last, failed)
	}
	if !reflect.DeepEqual(got.Marks, []MarkRow{failed}) {
		t.Errorf("marks = %+v", got.Marks)
	}

	mustApply(t, s, unmark("undo-2"))
	got = mustGet(t, s, "g-1")
	if got.Status != StatusOpen || LastMark(got) != nil {
		t.Errorf("second unmark left %s, %+v", got.Status, LastMark(got))
	}
	if bytes.Contains(readTicketFile(t, s, "g-1"), []byte("\nmarks:")) {
		t.Errorf("an undone mark left a marks block:\n%s", readTicketFile(t, s, "g-1"))
	}
	refused(t, s, unmark("x"), ErrTransitionNotPermitted)

	// Nothing is undone once the ticket has left the status the mark left.
	mustApply(t, s, markReq("g-1", "skip", MarkNotNeeded))
	saveStatus(t, s, "g-1", StatusReady)
	refused(t, s, unmark("x"), ErrTransitionNotPermitted)
	saveStatus(t, s, "g-1", StatusClosed)
	mustApply(t, s, unmark("undo-3"))
	if got := mustGet(t, s, "g-1"); got.Status != StatusOpen || got.ClosedAt() != (time.Time{}) {
		t.Errorf("unmarking not needed left %s", got.Status)
	}
}

// A marks block a decode would lose part of refuses the file, a note reads
// back as given, and the marks are logged, reserved and left behind by a move.
func TestMarksBlockRoundTripsAndIsGuarded(t *testing.T) {
	s, _ := testStore(t)
	generic(t, s, "g-1", StatusReady)
	req := markReq("g-1", "fail", MarkFailed)
	req.Note = "\tfirst\n---\nstatus: done\n- [x] # y"
	mustApply(t, s, req)
	data := string(readTicketFile(t, s, "g-1"))
	tk, err := Parse(strings.NewReader(strings.Replace(data, "\npriority: 2\n", "\npriority: high\n", 1)))
	if err != nil || LastMark(tk) == nil || LastMark(tk).Note != req.Note {
		t.Errorf("beside an unreadable priority: %v, %+v", err, tk)
	}
	for from, to := range map[string]string{
		"    note: ":   `    note: ["x"] #`,
		"  - result: ": "  - result: [x]\n    was: ",
		"    prior: ":  "    prior: [x] #",
	} {
		if !strings.Contains(data, from) {
			t.Fatalf("no %q in:\n%s", from, data)
		}
		damaged := strings.Replace(data, from, to, 1)
		if _, err := Parse(strings.NewReader(damaged)); err == nil {
			t.Errorf("a marks block with %q parsed", to)
		}
	}

	prior := mk("g-1", StatusReady)
	next := mk("g-1", StatusReady)
	next.Marks = []MarkRow{{Result: MarkFailed, Note: "n"}}
	if got := changedFields(prior, next); !reflect.DeepEqual(got, []string{"marks"}) {
		t.Errorf("changed fields = %v, want marks", got)
	}
	if moved := copyForMove(next, "g-2", Note{}); moved.Marks != nil {
		t.Errorf("a moved copy carries %v", moved.Marks)
	}
	for _, key := range []string{"marks", "last_mark"} {
		if ValidateExtraKey(key) == nil {
			t.Errorf("%s was accepted as an extra field", key)
		}
	}
}
