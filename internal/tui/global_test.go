package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
)

// globalFixture is a central store with Root and cross-project parents
// activated over four namespaces, and the machine-local config that binds
// two of them to a checkout: warp and loom have one, ticket is registered
// with none, Root can have none. The tickets are one shared outcome —
// warp/epic-0001 with a child in every namespace, closed one included — and
// loom's own epic-0001, whose bare ID collides with warp's.
type globalFixture struct {
	root     string
	cfg      project.Config
	warpRepo string
	loomRepo string
}

func newGlobalFixture(t *testing.T) globalFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	f := globalFixture{
		root:     filepath.Join(home, "central"),
		warpRepo: filepath.Join(home, "warp-repo"),
		loomRepo: filepath.Join(home, "loom-repo"),
	}
	for _, dir := range []string{f.warpRepo, f.loomRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	f.cfg = project.Config{
		CentralRoot: f.root,
		Projects: map[string]project.ProjectConfig{
			"warp":   {Path: f.warpRepo, Store: "central"},
			"loom":   {Path: f.loomRepo, Store: "central"},
			"ticket": {Store: "central"},
		},
	}
	if err := project.Save(f.cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	catalog := "required_features: [root-namespace, cross-project-parents]\nnamespaces:\n  _root: {kind: root}\n  warp: {kind: project}\n  loom: {kind: project}\n  ticket: {kind: project}\n"
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "catalog.yaml"), []byte(catalog), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	for _, ns := range []string{project.RootNamespace, "warp", "loom", "ticket"} {
		if err := os.MkdirAll(f.ticketsDir(ns), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	now := time.Now()
	leaf := func(id string, status ticket.Status, parent, title string) *ticket.Ticket {
		return &ticket.Ticket{ID: id, Title: title, Type: ticket.TypeFeature, Status: status, Parent: parent, Priority: 2, Created: now}
	}
	f.create(t, "warp", &ticket.Ticket{ID: "epic-0001", Title: "Warp outcome", Type: ticket.TypeEpic, Status: ticket.StatusBacklog, Priority: 2, Created: now})
	f.create(t, "loom", &ticket.Ticket{ID: "epic-0001", Title: "Loom outcome", Type: ticket.TypeEpic, Status: ticket.StatusBacklog, Priority: 2, Created: now})
	f.create(t, "warp", leaf("a-0002", ticket.StatusDone, "epic-0001", "Warp's own child"))
	f.create(t, "loom", leaf("b-0003", ticket.StatusOpen, "warp/epic-0001", "Loom child of warp's epic"))
	f.create(t, "loom", leaf("c-0004", ticket.StatusClosed, "warp/epic-0001", "Loom child warp dropped"))
	f.create(t, "loom", leaf("d-0005", ticket.StatusDone, "epic-0001", "Loom's own child"))
	f.create(t, project.RootNamespace, leaf("idea-0006", ticket.StatusBacklog, "warp/epic-0001", "An idea with no home yet"))
	f.create(t, "ticket", leaf("e-0007", ticket.StatusReady, "warp/epic-0001", "Ticket child of warp's epic"))
	return f
}

func (f globalFixture) ticketsDir(ns string) string {
	return filepath.Join(f.root, "tickets", ns)
}

func (f globalFixture) store(ns string) *ticket.FileStore {
	return ticket.NewProjectFileStore(f.ticketsDir(ns), ns)
}

func (f globalFixture) create(t *testing.T, ns string, tk *ticket.Ticket) {
	t.Helper()
	if err := f.store(ns).Create(tk); err != nil {
		t.Fatalf("create %s/%s: %v", ns, tk.ID, err)
	}
}

// setStatus writes a leaf's status through its own project store, the way
// any other surface would.
func (f globalFixture) setStatus(t *testing.T, ns, id string, status ticket.Status) {
	t.Helper()
	store := f.store(ns)
	tk, err := store.Get(id)
	if err != nil {
		t.Fatalf("get %s/%s: %v", ns, id, err)
	}
	tk.Status = status
	if err := store.Update(tk); err != nil {
		t.Fatalf("update %s/%s: %v", ns, id, err)
	}
}

// execDir is the real checkout resolver over the fixture's config.
func (f globalFixture) execDir(ns string) (string, error) {
	return project.ExecutionDir(f.cfg, ns)
}

// app is `tk ui` over one namespace of the fixture, loaded and sized the way
// the program does it. workDir is what cmd/ui.go would hand it: the registered
// path, or nothing for Root.
func (f globalFixture) app(t *testing.T, ns, spawnCommand string) App {
	t.Helper()
	workDir, _ := f.execDir(ns)
	a := New(f.ticketsDir(ns), ns, "v0", spawnCommand, workDir, false, f.execDir)
	return f.reload(t, a)
}

// reload re-reads the store into the App and re-sizes it.
func (f globalFixture) reload(t *testing.T, a App) App {
	t.Helper()
	a = drain(a, loadTickets(a.store))
	if a.err != nil {
		t.Fatalf("load: %v", a.err)
	}
	model, _ := a.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return model.(App)
}

// onTab switches the App to tab with the cursor on the row for id.
func onTab(t *testing.T, a App, tab tabID, id string) App {
	t.Helper()
	a.activeTab = tab
	a.syncDashboardTab()
	for i, r := range a.dashboard.rows {
		if r.item.Ticket.ID == id {
			a.dashboard.cursor = i
			a.dashboard.clampOffset()
			return a
		}
	}
	t.Fatalf("%s tab lists no %s: %v", tabNames[tab], id, itemIDs(a.dashboard.items))
	return a
}

// cursorRow renders the dashboard row at the cursor.
func cursorRow(a App) string {
	return a.dashboard.renderRow(a.dashboard.rows[a.dashboard.cursor], false)
}

// AC1: a project board's epics tab is the local slice of the epic — the same
// bare epic ID in another namespace claims none of its children and lends
// none — and the group row says how much of the epic that slice is, with done
// and closed told apart.
func TestEpicsTabIsTheLocalSliceOfAGlobalEpic(t *testing.T) {
	f := newGlobalFixture(t)

	warp := onTab(t, f.app(t, "warp", ""), tabEpics, "epic-0001")
	warp.dashboard.toggleExpand()
	if got := childIDs(warp.dashboard); !slices.Equal(got, []string{"a-0002"}) {
		t.Errorf("warp's epic-0001 expands to %v, want only its local child a-0002 — loom/d-0005 belongs to loom's epic-0001", got)
	}
	row := cursorRow(warp)
	for _, want := range []string{"2/5", "1 done, 1 closed", "slice 1 of 5 local"} {
		if !strings.Contains(row, want) {
			t.Errorf("warp group row lacks %q:\n%s", want, row)
		}
	}

	loom := f.app(t, "loom", "")
	loomEpic := boardTicket(t, loom.tickets, "epic-0001")
	if got := loom.dashboard.epicChildren(loomEpic); len(got) != 1 || got[0].ID != "d-0005" {
		t.Errorf("loom's epic-0001 lists %v, want only d-0005", itemIDsOf(got))
	}
	if p := loom.snap.Progress("loom/epic-0001"); p.Total != 1 || p.Done != 1 {
		t.Errorf("loom/epic-0001 progress = %+v, want 1 child, done — not warp's five", p)
	}
	if loomEpic.Status != ticket.StatusDone {
		t.Errorf("loom/epic-0001 = %s, want done from its one child", loomEpic.Status)
	}

	// The ticket board's inbox row for e-0007 names its foreign epic.
	tk := onTab(t, f.app(t, "ticket", ""), tabInbox, "e-0007")
	if row := cursorRow(tk); !strings.Contains(row, "warp/0001") {
		t.Errorf("EPIC column should name the foreign epic as warp/0001:\n%s", row)
	}
}

// AC1: Root is a board like any project's, selectable with no checkout, and
// a Root idea under a foreign epic keeps its backlog row.
func TestRootBoardRendersWithoutACheckout(t *testing.T) {
	f := newGlobalFixture(t)
	a := f.app(t, project.RootNamespace, "")
	if a.workDir != "" {
		t.Fatalf("Root board workDir = %q, want none", a.workDir)
	}
	a = onTab(t, a, tabBacklog, "idea-0006")
	out := a.View()
	header := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(header, "Root") {
		t.Errorf("header should name the board Root:\n%s", header)
	}
	if !strings.Contains(out, "An idea with no home yet") {
		t.Errorf("Root backlog should list idea-0006:\n%s", out)
	}
	if a.tabCounts()[tabBacklog] != 1 {
		t.Errorf("Root backlog count = %d, want 1: a child of a foreign epic is not rolled up here", a.tabCounts()[tabBacklog])
	}
	// The move picker has no repo to list siblings of, and must not read the
	// process working directory's parent instead.
	a = press(t, a, "m")
	if got := a.detail.pickerItems; !slices.Equal(got, []string{enterPathOption}) {
		t.Errorf("Root move picker lists %v, want only the path prompt", got)
	}
}

// AC1: an epic's detail is its complete membership, foreign and closed
// children included, with the counts its status was derived from.
func TestEpicDetailListsEveryChildInEveryNamespace(t *testing.T) {
	f := newGlobalFixture(t)
	a := onTab(t, f.app(t, "warp", ""), tabDone, "a-0002")
	a = press(t, a, "u")
	if a.overlay != overlayDetail || a.detail.qid != "warp/epic-0001" {
		t.Fatalf("u on a-0002 opened overlay %v detail %q, want warp/epic-0001", a.overlay, a.detail.qid)
	}
	if a.detail.ticket.ID != "epic-0001" {
		t.Errorf("a local epic's detail shows ID %q, want the bare ID the board lists", a.detail.ticket.ID)
	}
	out := strings.Join(a.detail.lines, "\n")
	for _, want := range []string{
		"## Children",
		"warp/a-0002 [done]", "loom/b-0003 [open]", "loom/c-0004 [closed]", "_root/idea-0006 [backlog]", "ticket/e-0007 [ready]",
		"## Progress",
		"children: 5 — done 1, closed 1, open 1, ready 1, backlog 1",
		"complete: yes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("epic detail lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "loom/d-0005") {
		t.Errorf("epic detail lists loom/d-0005, a child of loom's same-slug epic:\n%s", out)
	}
	if help := strings.Join(a.detail.helpLines(), " "); !strings.Contains(help, "enter children") || !strings.Contains(help, "(u)p") {
		t.Errorf("epic detail help = %q, want the child picker and parent keys advertised", help)
	}
}

// AC2: a child in another project holds its epic open, finishes it and
// reopens it, and the board reads the global result. An unreadable file
// anywhere degrades the figure visibly rather than settling it.
func TestForeignChildDrivesTheEpicsGlobalState(t *testing.T) {
	f := newGlobalFixture(t)
	f.setStatus(t, project.RootNamespace, "idea-0006", ticket.StatusDone)
	f.setStatus(t, "ticket", "e-0007", ticket.StatusDone)
	f.setStatus(t, "loom", "c-0004", ticket.StatusDone)

	a := f.app(t, "warp", "")
	epic := boardTicket(t, a.tickets, "epic-0001")
	if epic.Status != ticket.StatusOpen || !epic.Completed.IsZero() {
		t.Fatalf("epic = %s completed %v, want open and uncompleted: loom/b-0003 is still open", epic.Status, epic.Completed)
	}

	f.setStatus(t, "loom", "b-0003", ticket.StatusDone)
	a = f.reload(t, a)
	epic = boardTicket(t, a.tickets, "epic-0001")
	if epic.Status != ticket.StatusDone || epic.Completed.IsZero() {
		t.Fatalf("epic = %s completed %v, want done with Completed set once the foreign child finished", epic.Status, epic.Completed)
	}

	// A write from the TUI itself: the loom child's detail, reached through
	// the epic, writes through loom's store and not warp's.
	a = onTab(t, a, tabDone, "epic-0001")
	a = press(t, a, "enter")
	a = press(t, a, "enter")
	a = pickChild(t, a, "loom/c-0004")
	a = press(t, a, "p")
	if got, err := f.store("loom").Get("c-0004"); err != nil || got.Priority != 3 {
		t.Errorf("p on the foreign child's detail: loom/c-0004 = %+v, %v; want priority cycled 2 -> 3", got, err)
	}
	if !strings.Contains(a.status, "loom/c-0004") {
		t.Errorf("status = %q, want it to name the foreign ticket written", a.status)
	}
	f.setStatus(t, "loom", "c-0004", ticket.StatusOpen)
	a = f.reload(t, a)
	epic = boardTicket(t, a.tickets, "epic-0001")
	if epic.Status != ticket.StatusOpen || !epic.Completed.IsZero() {
		t.Fatalf("epic = %s completed %v, want reopened and uncompleted by the foreign child", epic.Status, epic.Completed)
	}

	// Every child finished, but loom holds a file no parse can structure: the
	// total is a lower bound, and the board says so instead of reading 5/5.
	f.setStatus(t, "loom", "c-0004", ticket.StatusClosed)
	if err := os.WriteFile(filepath.Join(f.ticketsDir("loom"), "bad.md"), []byte("---\nid: [\n"), 0o644); err != nil {
		t.Fatalf("write bad.md: %v", err)
	}
	a = f.reload(t, a)
	epic = boardTicket(t, a.tickets, "epic-0001")
	if epic.Status == ticket.StatusDone || !epic.Completed.IsZero() {
		t.Errorf("epic = %s completed %v over an incomplete read, want neither done nor completed", epic.Status, epic.Completed)
	}
	a = onTab(t, a, tabEpics, "epic-0001")
	row := cursorRow(a)
	if !strings.Contains(row, "5/5") || !strings.Contains(row, "incomplete") {
		t.Errorf("group row should carry the count and the incomplete marker together:\n%s", row)
	}
	a = onTab(t, a, tabBacklog, "epic-0001")
	if row := cursorRow(a); !strings.Contains(row, "(5 children)") || !strings.Contains(row, "incomplete") {
		t.Errorf("backlog rollup should carry the count and the incomplete marker together:\n%s", row)
	}
	a = onTab(t, a, tabDone, "a-0002")
	a = press(t, a, "u")
	out := strings.Join(a.detail.lines, "\n")
	for _, want := range []string{"complete: no", "loom/bad.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("epic detail lacks %q:\n%s", want, out)
		}
	}
}

// AC3: a spawn is refused before any process is created when the selected
// ticket's own project has no checkout here — Root by definition, a project
// registered without a path — whichever board it was reached from.
func TestSpawnRefusesRootAndMissingBindingsBeforeProcessCreation(t *testing.T) {
	f := newGlobalFixture(t)
	marker := filepath.Join(t.TempDir(), "spawned")
	template := "touch " + marker

	a := onTab(t, f.app(t, project.RootNamespace, template), tabBacklog, "idea-0006")
	a = press(t, a, "w")
	assertRefused(t, a, marker, "Root has no repository")

	a = onTab(t, f.app(t, "ticket", template), tabInbox, "e-0007")
	a = press(t, a, "w")
	assertRefused(t, a, marker, "no checkout registered")

	// From warp's board, whose own checkout exists: the epic's Root child
	// reached through the detail is still Root's, and an epic is not run.
	a = onTab(t, f.app(t, "warp", template), tabDone, "a-0002")
	a = press(t, a, "u")
	a = press(t, a, "w")
	assertRefused(t, a, marker, "an epic is not run")
	a = press(t, a, "enter")
	a = pickChild(t, a, "_root/idea-0006")
	a = press(t, a, "w")
	assertRefused(t, a, marker, "Root has no repository")
}

// AC3: the checkout a spawn runs in is the selected leaf's own, resolved by
// its namespace: navigated to from the Root board through a warp epic, a loom
// child spawns in loom's registered path — not the board's (none) and not the
// epic's.
func TestSpawnResolvesTheSelectedLeafsOwnCheckout(t *testing.T) {
	f := newGlobalFixture(t)
	out := filepath.Join(t.TempDir(), "dir")
	template := "printf '%s' '{dir}' > " + out

	a := onTab(t, f.app(t, project.RootNamespace, template), tabBacklog, "idea-0006")
	a = press(t, a, "enter")
	if a.overlay != overlayDetail || a.detail.qid != "_root/idea-0006" {
		t.Fatalf("enter opened %v %q, want idea-0006's detail", a.overlay, a.detail.qid)
	}
	a = press(t, a, "u")
	if a.detail.qid != "warp/epic-0001" || a.detail.ticket.ID != "warp/epic-0001" {
		t.Fatalf("u opened %q (ID %q), want the foreign parent warp/epic-0001 under its qualified ID", a.detail.qid, a.detail.ticket.ID)
	}
	a = press(t, a, "enter")
	a = pickChild(t, a, "loom/b-0003")
	a = press(t, a, "w")
	if strings.Contains(a.status, "refusing") {
		t.Fatalf("spawn refused: %s", a.status)
	}
	var got []byte
	for i := 0; i < 50; i++ {
		if got, _ = os.ReadFile(out); len(got) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if string(got) != f.loomRepo {
		t.Errorf("spawned in %q, want loom's registered checkout %q", got, f.loomRepo)
	}

	// esc walks back through the details it came by, then closes.
	a = press(t, a, "esc")
	if a.detail.qid != "warp/epic-0001" {
		t.Errorf("esc returned to %q, want the epic", a.detail.qid)
	}
	a = press(t, a, "esc")
	if a.detail.qid != "_root/idea-0006" {
		t.Errorf("esc returned to %q, want the idea", a.detail.qid)
	}
	a = press(t, a, "esc")
	if a.overlay != overlayNone {
		t.Errorf("esc on the first detail left overlay %v open", a.overlay)
	}
}

// AC3: the resolved checkout is the process working directory, not only a
// placeholder: a template that never interpolates {dir} still runs in the
// loom child's own checkout, reached from the Root board through warp's
// epic, rather than wherever `tk ui` was launched.
func TestSpawnRunsInTheSelectedLeafsCheckoutWithoutInterpolatingDir(t *testing.T) {
	f := newGlobalFixture(t)
	out := filepath.Join(t.TempDir(), "pwd")
	template := "pwd -P > " + out

	a := onTab(t, f.app(t, project.RootNamespace, template), tabBacklog, "idea-0006")
	a = press(t, a, "enter")
	a = press(t, a, "u")
	a = press(t, a, "enter")
	a = pickChild(t, a, "loom/b-0003")
	a = press(t, a, "w")
	if strings.Contains(a.status, "refusing") {
		t.Fatalf("spawn refused: %s", a.status)
	}
	var got []byte
	for i := 0; i < 50; i++ {
		if got, _ = os.ReadFile(out); len(got) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	want, err := filepath.EvalSymlinks(f.loomRepo)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if strings.TrimSpace(string(got)) != want {
		t.Errorf("spawn ran in %q, want loom's registered checkout %q", strings.TrimSpace(string(got)), want)
	}
}

// A nested detail left through the edit form leaves nothing behind: the
// next detail opened from the board is the first, and esc closes it rather
// than returning to the epic the stack still held.
func TestDetailStackResetsAfterLeavingThroughTheEditForm(t *testing.T) {
	f := newGlobalFixture(t)
	a := onTab(t, f.app(t, "warp", ""), tabDone, "a-0002")
	a = press(t, a, "u")
	a = press(t, a, "enter")
	a = pickChild(t, a, "loom/b-0003")
	a = press(t, a, "e")
	if a.overlay != overlayForm {
		t.Fatalf("e on the child detail opened overlay %v, want the edit form", a.overlay)
	}
	a = press(t, a, "esc")
	if a.overlay != overlayNone {
		t.Fatalf("esc on the form left overlay %v, want the board", a.overlay)
	}
	a = press(t, a, "u")
	if a.overlay != overlayDetail || a.detail.qid != "warp/epic-0001" {
		t.Fatalf("u opened overlay %v detail %q, want warp/epic-0001", a.overlay, a.detail.qid)
	}
	a = press(t, a, "esc")
	if a.overlay != overlayNone {
		t.Errorf("esc on the first detail returned to %q instead of the board", a.detail.qid)
	}
}

// pickChild drives the open child picker to qid and confirms it.
func pickChild(t *testing.T, a App, qid string) App {
	t.Helper()
	if a.detail.input != inputChildPicker {
		t.Fatalf("child picker not open (input %v) on %q", a.detail.input, a.detail.qid)
	}
	idx := -1
	for i, id := range a.detail.pickerIDs {
		if id == qid {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("child picker lists %v, want %s among them", a.detail.pickerIDs, qid)
	}
	for i := 0; i < idx; i++ {
		a = press(t, a, "j")
	}
	a = press(t, a, "enter")
	if a.detail.qid != qid {
		t.Fatalf("child picker opened %q, want %s", a.detail.qid, qid)
	}
	return a
}

func assertRefused(t *testing.T, a App, marker, reason string) {
	t.Helper()
	if !strings.Contains(a.status, "refusing to spawn") || !strings.Contains(a.status, reason) {
		t.Errorf("status = %q, want the spawn refused for %q", a.status, reason)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("the spawn command ran: %s exists", marker)
	}
}

func itemIDsOf(tickets []*ticket.Ticket) []string {
	ids := make([]string, len(tickets))
	for i, tk := range tickets {
		ids[i] = tk.ID
	}
	return ids
}
