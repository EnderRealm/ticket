package ticket

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testedBody carries three test: criteria among others, so an index among the
// test: criteria is never the index among all of them.
const testedBody = "\nDescription.\n\n## Acceptance Criteria\n\n" +
	"- Builds.\n  verify: go build ./...\n" +
	"- A long answer wraps.\n  test: narrow the window\n" +
	"- Reads plainly.\n" +
	"- The card shows \"why: #1\".\n  test: open the card\n  verify: true\n" +
	"- The list scrolls.\n  test:\n"

const untestedBody = "\nDescription.\n\n## Acceptance Criteria\n\n- Builds.\n  verify: go build ./...\n- Reads plainly.\n"

// landable creates an open ticket on a branch with the given body.
func landable(t *testing.T, s Store, id, body string) {
	t.Helper()
	tk := mk(id, StatusOpen)
	tk.Branch = "work/" + id
	tk.Body = body
	mustCreate(t, s, tk)
}

func act(id, action string, tr Transition) ActionRequest {
	req := ActionRequest{ID: id, ActionID: action, Note: "note " + action, Transition: tr}
	if tr == TransitionLand {
		req.Commit = shaA
	}
	if tr == TransitionFail {
		req.Checks = []int{1}
	}
	return req
}

// refused applies req, requires the refusal to be target (any error when nil)
// and the stored file to be left as it was.
func refused(t *testing.T, s *FileStore, req ActionRequest, target error) error {
	t.Helper()
	before := readTicketFile(t, s, req.ID)
	_, err := ApplyAction(s, req)
	if err == nil || (target != nil && !errors.Is(err, target)) {
		t.Errorf("%s on %s = %v, want it refused (%v)", req.Transition, req.ID, err, target)
	}
	if target == nil && errors.Is(err, ErrTransitionNotPermitted) {
		t.Errorf("%s on %s = %v, want a malformed request", req.Transition, req.ID, err)
	}
	if after := readTicketFile(t, s, req.ID); !bytes.Equal(before, after) {
		t.Errorf("a refused %s changed %s:\n%s", req.Transition, req.ID, after)
	}
	return err
}

func TestLandChoosesTestingOrDone(t *testing.T) {
	s, _ := testStore(t)
	landable(t, s, "tested", testedBody)
	landable(t, s, "untested", untestedBody)
	landable(t, s, "bare", "\nDescription.\n")

	for id, want := range map[string]Status{"tested": StatusTesting, "untested": StatusDone, "bare": StatusDone} {
		req := act(id, "land-1", TransitionLand)
		req.Commit = " " + strings.ToUpper(shaA) + "\n"
		res := mustApply(t, s, req)
		got := mustGet(t, s, id)
		if got.Status != want || res.Receipt.Outcome != string(want) {
			t.Errorf("%s landed %s (outcome %s), want %s", id, got.Status, res.Receipt.Outcome, want)
		}
		if got.Outputs[OutputKeyCommit] != shaA || got.Outputs[OutputKeyBranch] != "work/"+id || got.Actions[0].Commit != shaA {
			t.Errorf("%s: outputs = %v, receipt = %+v, want the commit and the branch", id, got.Outputs, got.Actions)
		}
		if got.ClosedAt().IsZero() != (want == StatusTesting) {
			t.Errorf("%s: closed = %v at %s", id, got.ClosedAt(), want)
		}
		if len(got.Notes) != 1 || got.Notes[0].Text != req.Note || LastTest(got) != nil {
			t.Errorf("%s: notes = %v, last test = %v", id, got.Notes, LastTest(got))
		}
	}

	// A ticket that failed lands again by the same decision, as the new commit.
	mustApply(t, s, act("tested", "fail-1", TransitionFail))
	again := act("tested", "land-2", TransitionLand)
	again.Commit = shaB
	mustApply(t, s, again)
	got := mustGet(t, s, "tested")
	if got.Status != StatusTesting || got.Outputs[OutputKeyCommit] != shaB {
		t.Errorf("re-landed %s at %v, want testing at %s", got.Status, got.Outputs, shaB)
	}
	if last := LastTest(got); last == nil || last.Result != TestFailed || last.Commit != shaA {
		t.Errorf("last test after the re-landing = %+v, want the fail of %s", last, shaA)
	}
	// One already in testing goes through the decision again.
	again.ActionID = "land-3"
	if res := mustApply(t, s, again); res.Ticket.Status != StatusTesting {
		t.Errorf("landing a testing ticket = %s", res.Ticket.Status)
	}

	mustCreate(t, s, mkEpic("epic", StatusBacklog, ""))
	refused(t, s, act("epic", "land", TransitionLand), ErrTransitionNotPermitted)
	for _, commit := range []string{"", "a1a1a1a", strings.Repeat("z", 40)} {
		req := act("untested", "land-bad", TransitionLand)
		req.Commit = commit
		refused(t, s, req, nil)
	}
}

func TestPassMovesTestingToDone(t *testing.T) {
	s, _ := testStore(t)
	landable(t, s, "t-1", testedBody)
	mustApply(t, s, act("t-1", "land", TransitionLand))
	if !mustGet(t, s, "t-1").Completed.IsZero() {
		t.Fatal("landing in testing stamped a closed date")
	}

	before := time.Now().UTC().Truncate(time.Second)
	res := mustApply(t, WithSource(s, "host"), act("t-1", "pass", TransitionPass))
	got := mustGet(t, s, "t-1")
	if got.Status != StatusDone || res.Receipt.Outcome != "done" || res.Receipt.Commit != "" {
		t.Errorf("passed to %s, receipt %+v", got.Status, res.Receipt)
	}
	if got.ClosedAt().Before(before) {
		t.Errorf("closed = %v, want the pass at or after %v", got.ClosedAt(), before)
	}
	want := TestRow{Result: TestPassed, At: res.Receipt.At, By: "host", Commit: shaA}
	if last := LastTest(got); last == nil || !reflect.DeepEqual(*last, want) {
		t.Errorf("last test = %+v, want %+v", last, want)
	}
}

func TestFailRequiresNoteAndReturnsToReady(t *testing.T) {
	root, ms := centralFixture(t, true)
	s := nsStore(root, "warp")
	landable(t, s, "t-1", testedBody)
	mustApply(t, s, act("t-1", "land", TransitionLand))

	for _, note := range []string{"", " \n"} {
		req := act("t-1", "fail", TransitionFail)
		req.Note = note
		refused(t, s, req, nil)
	}
	for _, checks := range [][]int{nil, {}, {0}, {-1, 1}, {2, 2}, {4}, {1, 4}} {
		req := act("t-1", "fail", TransitionFail)
		req.Checks = checks
		refused(t, s, req, nil)
	}

	// The note is free text: a line that would close the frontmatter and text
	// YAML would fold or reinterpret all read back as given.
	req := act("t-1", "fail", TransitionFail)
	req.ID = "warp/t-1"
	req.Checks = []int{3, 2}
	req.Note = "First: line # one\n---\n\n  indented\ttab\r\nstatus: done\nline\u2028sep \"quoted\" \\ end"
	res := mustApply(t, WithSource(ms, "host"), req)
	if !reflect.DeepEqual(req.Checks, []int{3, 2}) {
		t.Errorf("the caller's checks were reordered: %v", req.Checks)
	}
	got := mustGet(t, s, "t-1")
	if got.Status != StatusReady || !got.ClosedAt().IsZero() || res.Receipt.Outcome != "ready" {
		t.Errorf("failed to %s, closed %v, receipt %+v", got.Status, got.ClosedAt(), res.Receipt)
	}
	want := TestRow{
		Result: TestFailed,
		Checks: []TestCheck{{Index: 2, Text: `The card shows "why: #1".`}, {Index: 3, Text: "The list scrolls."}},
		Note:   req.Note, At: res.Receipt.At, By: "host", Commit: shaA,
	}
	if last := LastTest(got); last == nil || !reflect.DeepEqual(*last, want) {
		t.Errorf("last test = %+v, want %+v", last, want)
	}
	if n := len(got.Notes); n != 2 || !strings.HasPrefix(got.Notes[1].Text, "First: line # one") {
		t.Errorf("notes = %v, want the fail's note added", got.Notes)
	}

	// The record keeps the texts the test read, whatever the criteria become.
	if _, err := Mutate(s, "t-1", func(t *Ticket) error {
		t.Body = strings.Replace(t.Body, "The list scrolls.", "The list pages.", 1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if last := LastTest(mustGet(t, s, "t-1")); !reflect.DeepEqual(*last, want) {
		t.Errorf("last test after an edit = %+v, want %+v", last, want)
	}

	// The same checks in another order are the same action.
	req.Checks = []int{2, 3}
	if res := mustApply(t, ms, req); !res.Replayed {
		t.Error("the same checks in another order did not replay")
	}
}

func TestRetestAndRefusalsOutsideTesting(t *testing.T) {
	s, _ := testStore(t)
	landable(t, s, "t-1", testedBody)
	mustApply(t, s, act("t-1", "land", TransitionLand))
	refused(t, s, act("t-1", "x", TransitionRetest), ErrTransitionNotPermitted)

	// A fail undone, then a pass undone, each back to testing with no test left.
	for _, tr := range []Transition{TransitionFail, TransitionPass} {
		mustApply(t, s, act("t-1", string(tr), tr))
		if mustGet(t, s, "t-1").Status == StatusTesting {
			t.Fatalf("%s left the ticket in testing", tr)
		}
		mustApply(t, s, act("t-1", "undo-"+string(tr), TransitionRetest))
		got := mustGet(t, s, "t-1")
		if got.Status != StatusTesting || !got.Completed.IsZero() || LastTest(got) != nil {
			t.Errorf("undoing a %s = %s, completed %v, last test %+v", tr, got.Status, got.Completed, LastTest(got))
		}
		refused(t, s, act("t-1", "x", TransitionRetest), ErrTransitionNotPermitted)
	}

	// Undoing a second test reveals the first, which the re-landing since keeps
	// from being undone in turn.
	mustApply(t, s, act("t-1", "fail-1", TransitionFail))
	reland := act("t-1", "reland", TransitionLand)
	reland.Commit = shaB
	mustApply(t, s, reland)
	refused(t, s, act("t-1", "x", TransitionRetest), ErrTransitionNotPermitted)
	mustApply(t, s, act("t-1", "pass-2", TransitionPass))
	mustApply(t, s, act("t-1", "undo-2", TransitionRetest))
	if last := LastTest(mustGet(t, s, "t-1")); last == nil || last.Note != "note fail-1" || last.Commit != shaA {
		t.Errorf("last test = %+v, want the first fail", last)
	}
	saveStatus(t, s, "t-1", StatusReady)
	refused(t, s, act("t-1", "x", TransitionRetest), ErrTransitionNotPermitted)

	// Nothing is undone once the ticket has left the status the test left.
	landable(t, s, "t-2", testedBody)
	mustApply(t, s, act("t-2", "land", TransitionLand))
	mustApply(t, s, act("t-2", "fail", TransitionFail))
	saveStatus(t, s, "t-2", StatusOpen)
	refused(t, s, act("t-2", "x", TransitionRetest), ErrTransitionNotPermitted)
	saveStatus(t, s, "t-2", StatusDone)
	refused(t, s, act("t-2", "x", TransitionRetest), ErrTransitionNotPermitted)

	// Pass and fail outside testing, an epic included. The status is judged
	// before the checks' range.
	for _, status := range []Status{StatusBacklog, StatusReady, StatusOpen, StatusDone, StatusClosed} {
		id := "at-" + string(status)
		tk := mk(id, status)
		tk.Body = testedBody
		mustCreate(t, s, tk)
		refused(t, s, act(id, "x", TransitionPass), ErrTransitionNotPermitted)
		past := act(id, "x", TransitionFail)
		past.Checks = []int{9}
		refused(t, s, past, ErrTransitionNotPermitted)
		refused(t, s, act(id, "x", TransitionRetest), ErrTransitionNotPermitted)
	}
	mustCreate(t, s, mkEpic("epic", StatusBacklog, ""))
	for _, tr := range []Transition{TransitionPass, TransitionFail, TransitionRetest} {
		refused(t, s, act("epic", "x", tr), ErrTransitionNotPermitted)
	}
}

func TestLandingArgumentsBelongToTheirTransition(t *testing.T) {
	s, _ := testStore(t)
	landable(t, s, "t-1", testedBody)
	mustApply(t, s, act("t-1", "land", TransitionLand))
	for _, tr := range []Transition{TransitionNone, TransitionReopen, TransitionRelease, TransitionPass, TransitionFail, TransitionRetest} {
		req := act("t-1", "x", tr)
		req.Commit = shaB
		refused(t, s, req, nil)
	}
	for _, tr := range []Transition{TransitionNone, TransitionReopen, TransitionRelease, TransitionLand, TransitionPass, TransitionRetest} {
		req := act("t-1", "x", tr)
		req.Checks = []int{1}
		refused(t, s, req, nil)
	}
}

func TestLandingActionsReplay(t *testing.T) {
	s, _ := testStore(t)
	landable(t, s, "t-1", testedBody)
	steps := []ActionRequest{
		act("t-1", "land", TransitionLand),
		act("t-1", "fail", TransitionFail),
		act("t-1", "undo", TransitionRetest),
		act("t-1", "pass", TransitionPass),
	}
	var receipts []ActionReceipt
	for _, req := range steps {
		receipts = append(receipts, mustApply(t, s, req).Receipt)
	}
	before := readTicketFile(t, s, "t-1")
	for i, req := range steps {
		res := mustApply(t, s, req)
		if !res.Replayed || res.Receipt != receipts[i] || res.Ticket.Status != StatusDone {
			t.Errorf("replayed %s = %v %+v at %s, want %+v", req.ActionID, res.Replayed, res.Receipt, res.Ticket.Status, receipts[i])
		}
	}
	land, fail := steps[0], steps[1]
	land.Commit = shaB
	fail.Checks = []int{2}
	for _, req := range []ActionRequest{land, fail} {
		refused(t, s, req, ErrActionConflict)
	}
	if after := readTicketFile(t, s, "t-1"); !bytes.Equal(before, after) {
		t.Errorf("a replay changed the file:\n%s", after)
	}
}

// A receipt written before commit and checks existed is judged by the digest of
// the two-field payload, so it must still be what those actions digest to.
func TestApplyDigestOfEarlierTransitionsIsUnchanged(t *testing.T) {
	s, _ := testStore(t)
	for _, tr := range []Transition{TransitionNone, TransitionReopen, TransitionRelease} {
		id := "t-" + string(tr)
		tk := mk(id, StatusBacklog)
		tk.Extra = map[string]string{WaitingOnField: "x"}
		mustCreate(t, s, tk)
		sum := sha256.Sum256([]byte(`{"note":"seen","transition":"` + string(tr) + `"}`))
		res := mustApply(t, s, ActionRequest{ID: id, ActionID: "obs", Note: "seen", Transition: tr})
		if want := hex.EncodeToString(sum[:]); res.Receipt.Digest != want {
			t.Errorf("%q digest = %s, want %s", tr, res.Receipt.Digest, want)
		}
		data, err := json.Marshal(res.Receipt)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "commit") || bytes.Contains(readTicketFile(t, s, id), []byte("commit:")) {
			t.Errorf("a %q receipt carries a commit: %s", tr, data)
		}
	}
}

// A tests block a decode would lose part of refuses the file, since the next
// write would delete what was lost.
func TestDamagedTestsBlockIsRefused(t *testing.T) {
	s, _ := testStore(t)
	landable(t, s, "t-1", testedBody)
	mustApply(t, s, act("t-1", "land", TransitionLand))
	mustApply(t, s, act("t-1", "fail", TransitionFail))
	mustApply(t, s, act("t-1", "undo", TransitionRetest))
	if data := readTicketFile(t, s, "t-1"); bytes.Contains(data, []byte("\ntests:")) {
		t.Errorf("an undone test left a tests block:\n%s", data)
	}
	mustApply(t, s, act("t-1", "fail-2", TransitionFail))
	data := string(readTicketFile(t, s, "t-1"))
	for from, to := range map[string]string{
		"index: 1":        "index: one",
		`note: "note fai`: `note: ["x"] #`,
		"result:":         "result: [x]\n    was:",
	} {
		if !strings.Contains(data, from) {
			t.Fatalf("no %q in:\n%s", from, data)
		}
		damaged := strings.Replace(data, from, to, 1)
		if _, err := Parse(strings.NewReader(damaged)); err == nil {
			t.Errorf("a tests block with %q parsed", to)
		}
	}
	// A land receipt's commit is held to the same.
	head, rest, _ := strings.Cut(data, "\nactions:")
	if _, err := Parse(strings.NewReader(head + "\nactions:" + strings.Replace(rest, "commit: ", "commit: [x] #", 1))); !errors.Is(err, errDamagedActions) {
		t.Errorf("a receipt with a commit that is no hash = %v, want errDamagedActions", err)
	}
	// Another field's type error costs that field alone.
	tk, err := Parse(strings.NewReader(strings.Replace(data, "priority: 2", "priority: high", 1)))
	if err != nil || len(tk.Tests) != 1 {
		t.Errorf("an unreadable priority refused a ticket with a test: %v", err)
	}
}

// A fail's note and a check's text are free text. Each is stored and reads back
// as given, on its own and beside a field that did not decode, which has the
// parser look at the block a second time.
func TestFailNoteRoundTrips(t *testing.T) {
	for name, note := range map[string]string{
		"leading tab":       "\tfirst\n\tsecond",
		"tab lines":         "\t\n\t\t\n\tx",
		"leading newline":   "\nfirst\n\tsecond",
		"leading spaces":    "   first\nsecond\n      third",
		"trailing spaces":   "first  \nsecond \t\n  ",
		"trailing newlines": "first\n\n\n",
		"frontmatter close": "first\n---\nstatus: done\n...\n--- # x",
		"carriage returns":  "first\r\nsecond\rthird",
		"line separators":   "a\u2028b\u2029c\u0085d\ufeffe",
		"control bytes":     "a\x00b\x1b[2Jc\x7f",
		"invalid utf-8":     strings.Repeat("\tfirst \xff\xfe\n\tsecond \xc3", 20),
		"quoting":           "\"first\" 'second' \\n \\ # x: y\n- [z] {w} %p @q &r *s !t",
		"a number":          "1e3",
		"a null":            "~",
		"non-ascii":         "Über — 日本\n\t🙂",
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := testStore(t)
			landable(t, s, "t-1", "\n## Acceptance Criteria\n\n- one\ttab\u2028\"x\": ~ # y\n  test: look\n")
			mustApply(t, s, act("t-1", "land", TransitionLand))
			req := act("t-1", "fail", TransitionFail)
			req.Note = note
			mustApply(t, s, req)

			want := []TestCheck{{Index: 1, Text: "one\ttab\u2028\"x\": ~ # y"}}
			data := string(readTicketFile(t, s, "t-1"))
			for what, file := range map[string]string{
				"as written":          data,
				"unreadable priority": strings.Replace(data, "\npriority: 2\n", "\npriority: high\n", 1),
			} {
				tk, err := Parse(strings.NewReader(file))
				if err != nil {
					t.Errorf("%s: %v\n%s", what, err, file)
					continue
				}
				if last := LastTest(tk); last == nil || last.Note != note || !reflect.DeepEqual(last.Checks, want) {
					t.Errorf("%s: last test = %+v, want note %q and %v", what, last, note, want)
				}
				again, err := Serialize(tk)
				if err != nil {
					t.Errorf("%s: writing it back: %v", what, err)
				} else if back, err := Parse(bytes.NewReader(again)); err != nil || !testRowsEqual(*LastTest(back), *LastTest(tk)) {
					t.Errorf("%s: written back = %+v, %v", what, back, err)
				}
			}
		})
	}
}

func TestTestsAreLoggedAndDoNotMove(t *testing.T) {
	prior := mk("t-1", StatusTesting)
	next := mk("t-1", StatusTesting)
	next.Tests = []TestRow{{Result: TestFailed, Checks: []TestCheck{{Index: 1, Text: "x"}}, Note: "n"}}
	if got := changedFields(prior, next); !reflect.DeepEqual(got, []string{"tests"}) {
		t.Errorf("changed fields = %v, want tests", got)
	}
	if moved := copyForMove(next, "t-2", Note{}); moved.Tests != nil {
		t.Errorf("a moved copy carries %v", moved.Tests)
	}
	for _, key := range []string{"tests", "last_test"} {
		if ValidateExtraKey(key) == nil {
			t.Errorf("%s was accepted as an extra field", key)
		}
	}
}
