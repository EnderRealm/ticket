package ticket

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// saveStatus writes a status through the store, as an edit does.
func saveStatus(t *testing.T, s Store, id string, status Status) *Ticket {
	t.Helper()
	tk := mustGet(t, s, id)
	tk.Status = status
	if _, err := SaveEdit(s, tk, true); err != nil {
		t.Fatalf("set %s to %s: %v", id, status, err)
	}
	return mustGet(t, s, id)
}

func listedIDs(tickets []*Ticket) map[string]bool {
	got := map[string]bool{}
	for _, tk := range tickets {
		got[tk.ID] = true
	}
	return got
}

func TestTestingStatusIsValid(t *testing.T) {
	if err := ValidateStatus(StatusTesting); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStatus("needs_testing"); err == nil {
		t.Error("needs_testing was accepted as a status to write")
	}
	if !(StatusOrder(StatusOpen) < StatusOrder(StatusTesting) && StatusOrder(StatusTesting) < StatusOrder(StatusDone)) {
		t.Errorf("order open %d, testing %d, done %d, want testing between", StatusOrder(StatusOpen), StatusOrder(StatusTesting), StatusOrder(StatusDone))
	}

	s := depStore(t, mk("t-done", StatusDone), mk("t-testing", StatusTesting), mk("t-open", StatusOpen))
	tickets, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	SortByStatusPriorityID(tickets)
	if got, want := ids(tickets), []string{"t-open", "t-testing", "t-done"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sorted = %v, want %v", got, want)
	}

	legacy := "---\nid: t-legacy\nstatus: needs_testing\ndeps: []\nlinks: []\ntype: feature\npriority: 2\n---\n# Legacy\n"
	if err := os.WriteFile(filepath.Join(s.Dir, "t-legacy.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, s, "t-legacy").Status; got != StatusOpen {
		t.Errorf("stored needs_testing reads %s, want open", got)
	}
}

func TestTestingDepSatisfiesDependents(t *testing.T) {
	s := depStore(t, mk("dep", StatusOpen), mk("waiter", StatusReady, "dep"))
	if !IsBlocked(s, mustGet(t, s, "waiter")) {
		t.Fatal("an open dep does not block its dependent")
	}
	saveStatus(t, s, "dep", StatusTesting)

	waiter := mustGet(t, s, "waiter")
	if IsBlocked(s, waiter) {
		t.Error("a testing dep blocks its dependent")
	}
	if deps := BlockingDeps(s, waiter); len(deps) != 0 {
		t.Errorf("blocking deps = %v, want none", deps)
	}
	if !IsReady(s, waiter) {
		t.Error("the dependent of a testing ticket is not ready")
	}
	frontier, err := FrontierTickets(s)
	if err != nil {
		t.Fatal(err)
	}
	if !listedIDs(frontier)["waiter"] {
		t.Error("the dependent of a testing ticket is not in the frontier")
	}
	blocked, err := BlockedTickets(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 0 {
		t.Errorf("blocked = %v, want none", ids(blocked))
	}
	items, err := Inbox(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Ticket.ID == "waiter" && item.Action != ActionWork {
			t.Errorf("dependent's inbox action = %s, want work", item.Action)
		}
	}

	// Back to open, the dep holds its dependent again.
	saveStatus(t, s, "dep", StatusOpen)
	if !IsBlocked(s, mustGet(t, s, "waiter")) {
		t.Error("a dep sent back to open does not block its dependent")
	}
}

// The snapshot's own rule and a dep in another namespace answer the same way.
func TestTestingDepSatisfiesForeignDependents(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mk("loom/dep", StatusTesting),
		mk("warp/waiter", StatusReady, "loom/dep"),
	)
	snap, err := ms.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.IsBlocked("warp/waiter") {
		t.Error("the snapshot reads a testing dep as blocking")
	}
	if !listedIDs(FrontierOf(ms, snap.Tickets))["warp/waiter"] {
		t.Error("the dependent of a foreign testing ticket is not in the frontier")
	}
}

func TestTestingTicketIsNotReady(t *testing.T) {
	s := depStore(t, mk("dep", StatusOpen), mk("free", StatusReady), mk("waits", StatusReady, "dep"))
	saveStatus(t, s, "free", StatusTesting)
	saveStatus(t, s, "waits", StatusTesting)

	for name, list := range map[string]func(Store) ([]*Ticket, error){
		"ready": ReadyTickets, "ready open": ReadyTicketsOpen, "frontier": FrontierTickets, "blocked": BlockedTickets,
	} {
		tickets, err := list(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := listedIDs(tickets); got["free"] || got["waits"] {
			t.Errorf("%s lists a testing ticket: %v", name, ids(tickets))
		}
	}
	if free := mustGet(t, s, "free"); IsReady(s, free) || IsReadyOpen(s, free) {
		t.Error("a testing ticket is ready")
	}
}

// A failed test sends the ticket back to open, so a loop through it is still
// one to report.
func TestFindCyclesReportsLoopThroughTesting(t *testing.T) {
	s := depStore(t, mk("a", StatusOpen, "b"))
	writeLegacy(t, s, mk("b", StatusTesting, "a"))
	cycles, err := FindCycles(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 1 {
		t.Errorf("cycles = %v, want the one through the testing ticket", cycles)
	}
}

func TestEpicWithTestingChildDerivesOpen(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic", StatusBacklog, ""),
		mkWithParent("warp/done", StatusDone, "warp/epic"),
		mkWithParent("loom/landed", StatusOpen, "warp/epic"),
	)
	progress := func() (Status, EpicProgress) {
		t.Helper()
		snap, err := ms.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		epic, _ := snap.Get("warp/epic")
		return epic.Status, snap.Progress("warp/epic")
	}

	saveStatus(t, ms, "loom/landed", StatusTesting)
	status, got := progress()
	if status != StatusOpen {
		t.Errorf("epic reads %s, want open", status)
	}
	if want := (EpicProgress{Total: 2, Done: 1, Testing: 1, Complete: true}); !reflect.DeepEqual(got, want) {
		t.Errorf("progress = %+v, want %+v", got, want)
	}
	if epic := mustGet(t, ms, "warp/epic"); !epic.ClosedAt().IsZero() {
		t.Errorf("epic closed at %v while a child is in testing", epic.ClosedAt())
	}

	saveStatus(t, ms, "loom/landed", StatusDone)
	if status, _ = progress(); status != StatusDone {
		t.Errorf("epic reads %s once the child is done, want done", status)
	}

	// The other mixes: never backlog, done or closed while a child waits.
	for _, rest := range [][]Status{
		{}, {StatusClosed}, {StatusDone, StatusClosed}, {StatusReady}, {StatusBacklog}, {StatusOpen},
	} {
		children := []*Ticket{mk("c", StatusTesting)}
		for _, s := range rest {
			children = append(children, mk("c", s))
		}
		for _, abandoned := range []bool{false, true} {
			if got := DeriveEpicStatus(abandoned, children); got != StatusOpen {
				t.Errorf("testing with %v, abandoned %v, derives %s, want open", rest, abandoned, got)
			}
		}
	}
}

func TestAbandonClosesTestingChild(t *testing.T) {
	s := depStore(t,
		mkEpic("epic", StatusBacklog, ""),
		mkWithParent("done", StatusDone, "epic"),
		mkWithParent("landed", StatusTesting, "epic"),
	)
	epic := mustGet(t, s, "epic")
	epic.Status = StatusClosed
	closed, err := SaveEdit(s, epic, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(closed, []string{"landed"}) {
		t.Errorf("abandon closed %v, want the testing child", closed)
	}
	landed := mustGet(t, s, "landed")
	if landed.Status != StatusClosed || landed.ClosedAt().IsZero() {
		t.Errorf("testing child reads %s closed at %v, want closed with a date", landed.Status, landed.ClosedAt())
	}
	if got := mustGet(t, s, "epic").Status; got != StatusClosed {
		t.Errorf("abandoned epic reads %s, want closed", got)
	}

	// Sent back to testing, the child un-closes the epic.
	saveStatus(t, s, "landed", StatusTesting)
	if got := mustGet(t, s, "epic").Status; got != StatusOpen {
		t.Errorf("abandoned epic with a testing child reads %s, want open", got)
	}
}

// An unfinished child in another project refuses the abandon, and a testing
// one is unfinished.
func TestAbandonRefusesForeignTestingChild(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic", StatusBacklog, ""),
		mkWithParent("loom/landed", StatusTesting, "warp/epic"),
	)
	epic := mustGet(t, ms, "warp/epic")
	epic.Status = StatusClosed
	if _, err := SaveEdit(ms, epic, true); err == nil {
		t.Error("abandon over a foreign testing child was not refused")
	}
	if got := mustGet(t, ms, "loom/landed").Status; got != StatusTesting {
		t.Errorf("foreign child reads %s, want testing", got)
	}
}

func TestClosedDateStampedAtDoneNotTesting(t *testing.T) {
	s := depStore(t, mk("item", StatusOpen))
	if got := saveStatus(t, s, "item", StatusTesting); !got.Completed.IsZero() || !got.ClosedAt().IsZero() {
		t.Errorf("testing stamped completed %v", got.Completed)
	}
	if got := saveStatus(t, s, "item", StatusDone); got.ClosedAt().IsZero() {
		t.Error("done did not stamp the closed date")
	}
	if got := saveStatus(t, s, "item", StatusTesting); !got.Completed.IsZero() {
		t.Errorf("done back to testing kept completed %v", got.Completed)
	}
}

func TestInboxListsTestingAsTest(t *testing.T) {
	question := mk("question", StatusTesting)
	question.Extra = map[string]string{QuestionField: "Which phone?"}
	waiting := mk("waiting", StatusTesting)
	waiting.Extra = map[string]string{WaitingOnField: "observer/q-1"}
	s := depStore(t,
		mkEpic("epic", StatusBacklog, ""),
		mk("dep", StatusOpen),
		mkWithParent("landed", StatusTesting, "epic", "dep"),
		question, waiting,
	)
	items, err := Inbox(s)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ActionKind{}
	for _, item := range items {
		got[item.Ticket.ID] = item.Action
	}
	for id, want := range map[string]ActionKind{"landed": ActionTest, "question": ActionBlocked, "waiting": ActionBlocked} {
		if got[id] != want {
			t.Errorf("inbox action for %s = %q, want %q", id, got[id], want)
		}
	}

	projects, err := Projects(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || len(projects[0].NextActions) != 1 || projects[0].NextActions[0].Action != ActionTest {
		t.Fatalf("projects = %+v, want the epic with the test outstanding", projects)
	}
	if p := projects[0]; p.CompletionPct != 0 || p.StatusBreakdown[StatusTesting] != 1 {
		t.Errorf("completion = %v, breakdown = %v, want 0%% and one testing", p.CompletionPct, p.StatusBreakdown)
	}
}
