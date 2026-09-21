package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// findingsTickets is a clean leaf and a subject under it — a parent that is
// no epic, which the write path refuses and an older store holds — carrying
// criteria that name no verify command.
func findingsTickets() []*ticket.Ticket {
	return []*ticket.Ticket{
		{ID: "sf-leaf-0001", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: time.Now(), Title: "Leaf", Body: "\n"},
		{
			ID: "sf-subject-0002", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Parent: "sf-leaf-0001",
			Created: time.Now(), Title: "Subject",
			Body: "\nA description.\n\n## Acceptance Criteria\n\n- Something happens.\n- Something else happens.\n",
		},
	}
}

// appOverDir is `tk ui` over a project directory, loaded and sized the way
// the program does it: the store New wires, not a fixture's stand-in.
func appOverDir(t *testing.T, dir, ns string) App {
	t.Helper()
	a := New(dir, ns, "v0", "", "", false, nil)
	a = drain(a, loadTickets(a.store))
	if a.err != nil {
		t.Fatalf("load: %v", a.err)
	}
	model, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return model.(App)
}

// openDetail opens id's detail from the all tab and returns its rendered
// lines, ANSI stripped.
func openDetail(t *testing.T, a App, id string) (App, string) {
	t.Helper()
	a = press(t, onTab(t, a, tabAll, id), "enter")
	if a.overlay != overlayDetail {
		t.Fatalf("enter on %s opened overlay %v, want the detail", id, a.overlay)
	}
	return a, ansi.Strip(strings.Join(a.detail.lines, "\n"))
}

// The detail names every finding the audit holds against the ticket — through
// the store a save from the detail writes to, whether or not the store is a
// central one — and a clean ticket gets no section.
func TestDetailReportsFindings(t *testing.T) {
	for name, load := range map[string]func(t *testing.T) App{
		"central store board": func(t *testing.T) App {
			return appOverDir(t, writeBoardFixture(t, "proj", findingsTickets()...), "proj")
		},
		"single project store": func(t *testing.T) App {
			dir := filepath.Join(t.TempDir(), "tickets")
			writeTicketFiles(t, dir, findingsTickets()...)
			return appOverDir(t, dir, "proj")
		},
		"store with no project": func(t *testing.T) App {
			dir := filepath.Join(t.TempDir(), "tickets")
			writeTicketFiles(t, dir, findingsTickets()...)
			return appOverDir(t, dir, "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := load(t)
			_, out := openDetail(t, a, "sf-subject-0002")
			idx := strings.Index(out, "## Findings")
			if idx < 0 {
				t.Fatalf("detail missing Findings section:\n%s", out)
			}
			findings := out[idx:]
			for _, want := range []string{
				string(ticket.ViolationParentNotEpic) + "  parent: sf-leaf-0001  (parent ",
				"is type feature)",
				string(ticket.ContentBareAcceptance) + "  2 bare criterion(s)",
			} {
				if !strings.Contains(findings, want) {
					t.Errorf("Findings section missing %q:\n%s", want, findings)
				}
			}

			_, clean := openDetail(t, a, "sf-leaf-0001")
			for _, unwanted := range []string{"## Findings", string(ticket.ViolationParentNotEpic), string(ticket.ContentBareAcceptance), "not checked"} {
				if strings.Contains(clean, unwanted) {
					t.Errorf("clean ticket's detail carries %q:\n%s", unwanted, clean)
				}
			}
		})
	}
}

// A foreign ticket opened from an epic's detail is audited through the
// central store under its qualified ID, the way a save from that detail
// writes.
func TestDetailReportsForeignFindings(t *testing.T) {
	f := newGlobalFixture(t)
	writeTicketFiles(t, f.ticketsDir("loom"), &ticket.Ticket{
		ID: "bad-0008", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Parent: "d-0005",
		Created: time.Now(), Title: "Under a leaf", Body: "\n",
	})
	a := f.app(t, "warp", "")
	detail, ok := a.detailFor("loom/bad-0008")
	if !ok {
		t.Fatal("loom/bad-0008 does not resolve from warp's board")
	}
	out := ansi.Strip(strings.Join(detail.lines, "\n"))
	if want := string(ticket.ViolationParentNotEpic) + "  parent: d-0005  (parent loom/d-0005 is type feature)"; !strings.Contains(out, want) {
		t.Errorf("foreign detail missing %q:\n%s", want, out)
	}
}

// Text the audit read off a stored file — a parent naming an escape sequence,
// a body section holding one — never reaches the terminal raw.
func TestDetailFindingsSanitizeStoredText(t *testing.T) {
	dir := writeBoardFixture(t, "proj", findingsTickets()...)
	// The serializer writes a parent plain, which yaml refuses to read back
	// with a control character in it; a remote can hand over the quoted form.
	raw := "---\nid: sf-raw-0003\nstatus: open\ntype: feature\nparent: \"\\e[2Jsf-leaf-0001\\u202e\"\ncreated: 2026-01-01T00:00:00Z\n---\n# Raw\n\nA description.\n\n## Acceptance Criteria\n\n- Something \x1b[31mred\x1b[0m happens.\n"
	if err := os.WriteFile(filepath.Join(dir, "sf-raw-0003.md"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write sf-raw-0003: %v", err)
	}
	_, out := openDetail(t, appOverDir(t, dir, "proj"), "sf-raw-0003")
	if !strings.Contains(out, string(ticket.ViolationParentMissing)) {
		t.Fatalf("detail does not report the unresolved parent:\n%s", out)
	}
	for _, r := range []rune{'\x1b', '\u202e'} {
		if strings.ContainsRune(out, r) {
			t.Errorf("detail contains control or format character %U:\n%q", r, out)
		}
	}
}

// An audit that could not evaluate the ticket says so in the same section,
// so an unchecked ticket is never read as a clean one.
func TestDetailReportsUncheckedTicket(t *testing.T) {
	tk := findingsTickets()[0]
	out := ansi.Strip(strings.Join(newDetailModel(tk, tk.ID, nil, ticket.Findings{}, errors.New("boom \x1b[2J"), 100, 30).lines, "\n"))
	if !strings.Contains(out, "## Findings") || !strings.Contains(out, "not checked: boom") {
		t.Errorf("detail does not report the failed audit:\n%s", out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("audit error reached the detail raw:\n%q", out)
	}
}

// Findings are content like any other section: on a narrow terminal the view
// stays inside the frame, a long detail wraps within the width, and scrolling
// reaches the last finding.
func TestDetailFindingsFitNarrowFrame(t *testing.T) {
	const w, h = 30, 10
	tk := findingsTickets()[1]
	detail := "parent sf-leaf-0001 is type feature and this detail runs well past a narrow terminal's width"
	findings := ticket.Findings{
		Parent:  &ticket.ParentViolation{ID: tk.ID, Parent: tk.Parent, Kind: ticket.ViolationParentNotEpic, Detail: detail},
		Content: []ticket.ContentIssue{{ID: tk.ID, Kind: ticket.ContentBareAcceptance, Bare: 2}},
	}
	m := newDetailModel(tk, tk.ID, nil, findings, nil, w, h)

	check := func(step string) {
		t.Helper()
		if lines := strings.Split(m.view(), "\n"); len(lines) > h {
			t.Errorf("%s: view is %d rows, frame is %d", step, len(lines), h)
		}
	}
	check("top")
	inFindings := false
	for _, l := range m.lines {
		if strings.Contains(ansi.Strip(l), "## Findings") {
			inFindings = true
			continue
		}
		if inFindings && ansi.StringWidth(l) > w {
			t.Errorf("finding row wider than %d: %q", w, l)
		}
	}
	if !inFindings {
		t.Fatalf("detail missing Findings section:\n%s", strings.Join(m.lines, "\n"))
	}

	all := ansi.Strip(strings.Join(m.lines, "\n"))
	if strings.Contains(all, detail) {
		t.Errorf("long detail was not wrapped:\n%s", all)
	}
	// The wrapped rows between the parent finding's first row and the next
	// finding rejoin to the whole detail: wrapped, not clipped.
	var joined []string
	for _, l := range strings.Split(all, "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, string(ticket.ContentBareAcceptance)) {
			break
		}
		if len(joined) > 0 || strings.HasPrefix(l, string(ticket.ViolationParentNotEpic)) {
			joined = append(joined, l)
		}
	}
	if got := strings.Join(joined, " "); len(joined) < 2 || !strings.Contains(got, detail) {
		t.Errorf("wrapped detail rows do not rejoin to the detail:\n%q", got)
	}

	// The last line is the final finding's last wrapped row.
	last := ansi.Strip(m.lines[len(m.lines)-1])
	if !strings.HasSuffix(strings.TrimSpace(last), "criterion(s)") {
		t.Fatalf("last line is not the final finding: %q", last)
	}
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	check("end")
	if !strings.Contains(ansi.Strip(m.view()), last) {
		t.Errorf("G does not reach the last finding:\n%s", m.view())
	}

	m.offset = 0
	for i := 0; i < len(m.lines); i++ {
		m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	check("scrolled")
	if !strings.Contains(ansi.Strip(m.view()), last) {
		t.Errorf("j does not reach the last finding:\n%s", m.view())
	}
}
