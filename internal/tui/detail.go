package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Aliases to centralized styles (styles.go)
var (
	fieldKeyStyle   = StyleFieldKey
	fieldValStyle   = StyleFieldVal
	sectionStyle    = StyleSection
	timestampStyle  = StyleTimestamp
	detailHelpStyle = StyleHelp
	inputLabelStyle = StyleInputLabel
)

type inputMode int

const (
	inputNone inputMode = iota
	inputNote
	inputMove
	inputMovePicker
	inputChildPicker
)

const enterPathOption = "enter path..."

type detailModel struct {
	ticket *ticket.Ticket
	// qid is the ticket's qualified ID, the key the graph and a mutation of
	// a foreign ticket answer to; ns is its namespace, which its own
	// references are read relative to.
	qid  string
	ns   string
	snap *ticket.Snapshot // the graph relationships are read off; nil renders the ticket alone
	// findings is what the audit holds against the ticket; auditErr is why it
	// could not be checked, which is never presented as clean.
	findings     ticket.Findings
	auditErr     error
	lines        []string
	offset       int
	width        int
	height       int
	input        inputMode
	inputText    string
	pickerItems  []string // repo paths for the move picker, child lines for the child picker
	pickerIDs    []string // the child picker's qualified IDs, parallel to pickerItems
	pickerCursor int
	pickerOffset int // first picker item drawn; the list scrolls like the body does
}

// newDetailModel builds the detail of t, whose qualified ID is qid. t.ID is
// whatever the caller presents — bare for a board's own ticket, qualified for
// a foreign one — and qid is always the graph's key. findings and auditErr
// are the audit's answer for t, computed by the caller through the store a
// write to t goes through.
func newDetailModel(t *ticket.Ticket, qid string, snap *ticket.Snapshot, findings ticket.Findings, auditErr error, w, h int) detailModel {
	ns, _ := ticket.ParseNamespacedID(qid)
	m := detailModel{
		ticket:   t,
		qid:      qid,
		ns:       ns,
		snap:     snap,
		findings: findings,
		auditErr: auditErr,
		width:    w,
		height:   h,
	}
	m.lines = m.render()
	return m
}

// children is the epic's children off the graph: every namespace, closed
// included, in listing order. None without a graph.
func (m detailModel) children() []*ticket.Ticket {
	if m.snap == nil {
		return nil
	}
	return m.snap.Children(m.qid)
}

// childLine is one child as the detail and the child picker list it.
func childLine(t *ticket.Ticket) string {
	return fmt.Sprintf("%s [%s] %s", ticket.SanitizeControl(t.ID), ticket.SanitizeControl(string(t.Status)), ticket.SanitizeControl(t.Title))
}

// startChildPicker opens the picker over the epic's children and reports
// whether there were any to pick from.
func (m *detailModel) startChildPicker() bool {
	children := m.children()
	if len(children) == 0 {
		return false
	}
	m.pickerItems, m.pickerIDs = nil, nil
	for _, c := range children {
		m.pickerItems = append(m.pickerItems, childLine(c))
		m.pickerIDs = append(m.pickerIDs, c.ID)
	}
	m.pickerCursor, m.pickerOffset = 0, 0
	m.input = inputChildPicker
	m.inputText = ""
	return true
}

func (m *detailModel) setSize(w, h int) {
	m.width = w
	m.height = h
	m.clampPickerOffset()
}

func (m detailModel) inputActive() bool {
	return m.input != inputNone
}

func (m *detailModel) startInput(mode inputMode) {
	m.input = mode
	m.inputText = ""
}

func (m *detailModel) startMovePicker(repoRoot string) {
	repos := discoverSiblingRepos(repoRoot)
	repos = append(repos, enterPathOption)
	m.pickerItems = repos
	m.pickerCursor, m.pickerOffset = 0, 0
	m.input = inputMovePicker
	m.inputText = ""
}

// discoverSiblingRepos finds git repositories adjacent to the given repo. The
// repo is the caller's working directory, not the tickets directory: a central
// project's tickets live under the store root, whose siblings are other
// projects' ticket directories rather than repos.
func discoverSiblingRepos(repoRoot string) []string {
	// No repo, no siblings: the Root board has no checkout, and filepath.Dir
	// of "" would be the process working directory's parent.
	if repoRoot == "" {
		return nil
	}
	parentDir := filepath.Dir(repoRoot) // /path/to/repo -> /path/to
	repoName := filepath.Base(repoRoot)

	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil
	}

	var repos []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == repoName {
			continue
		}
		candidate := filepath.Join(parentDir, e.Name())
		if info, err := os.Stat(filepath.Join(candidate, ".git")); err == nil && info.IsDir() {
			repos = append(repos, candidate)
		}
	}
	return repos
}

// helpLines returns the footer help wrapped to the view width so a narrow
// terminal shows every shortcut instead of clipping the trailing ones.
func (m detailModel) helpLines() []string {
	var help string
	if m.input == inputMovePicker || m.input == inputChildPicker {
		help = "↑↓ select  enter confirm  esc cancel"
	} else if m.input != inputNone {
		help = "enter confirm  esc cancel"
	} else {
		children := ""
		if m.ticket != nil && m.ticket.Type == ticket.TypeEpic {
			children = "enter children  "
		}
		help = "↑↓/jk scroll  │  " + children + "(e)dit (p)riority (n)ote (m)ove (y)ank (w)ork (c)apture (u)p  │  esc back  (q)uit"
	}
	return wrapHelp(help, m.width)
}

// pickerRows is how many picker items the frame draws: what the height leaves
// after the help bar, the label and one body row, so an epic with more
// children than the terminal has rows scrolls inside the frame instead of
// pushing the cursor and the help bar off it. Never fewer than three, so a
// tiny terminal still shows the cursor with a neighbour either side.
func (m detailModel) pickerRows() int {
	rows := m.height - len(m.helpLines()) - 2
	if rows < 3 {
		rows = 3
	}
	if rows > len(m.pickerItems) {
		rows = len(m.pickerItems)
	}
	return rows
}

// clampPickerOffset scrolls the picker window so the cursor is inside it.
func (m *detailModel) clampPickerOffset() {
	rows := m.pickerRows()
	if rows == 0 {
		m.pickerOffset = 0
		return
	}
	if m.pickerCursor < m.pickerOffset {
		m.pickerOffset = m.pickerCursor
	}
	if m.pickerCursor >= m.pickerOffset+rows {
		m.pickerOffset = m.pickerCursor - rows + 1
	}
	// A resize can grow rows while the offset still sits near the end of
	// the list; pull it back so the window never runs past the last item.
	if maxOffset := len(m.pickerItems) - rows; m.pickerOffset > maxOffset {
		m.pickerOffset = maxOffset
	}
	if m.pickerOffset < 0 {
		m.pickerOffset = 0
	}
}

func (m detailModel) visibleRows() int {
	rows := m.height - len(m.helpLines())
	if m.input == inputMovePicker || m.input == inputChildPicker {
		rows -= m.pickerRows() + 1 // label + items
	} else if m.input != inputNone {
		rows-- // input bar
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m detailModel) update(msg tea.Msg) (detailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.input == inputMovePicker || m.input == inputChildPicker {
			return m.updatePicker(msg)
		}
		if m.input != inputNone {
			return m.updateInput(msg)
		}

		maxOffset := max(0, len(m.lines)-m.visibleRows())
		switch msg.String() {
		case "up", "k":
			if m.offset > 0 {
				m.offset--
			}
		case "down", "j":
			if m.offset < maxOffset {
				m.offset++
			}
		case "pgup", "b":
			m.offset -= m.visibleRows()
			if m.offset < 0 {
				m.offset = 0
			}
		case "pgdown", "f", " ":
			m.offset += m.visibleRows()
			if m.offset > maxOffset {
				m.offset = maxOffset
			}
		case "g":
			m.offset = 0
		case "G":
			m.offset = maxOffset
		}
	case tea.MouseMsg:
		maxOffset := max(0, len(m.lines)-m.visibleRows())
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.offset -= 3
			if m.offset < 0 {
				m.offset = 0
			}
		case tea.MouseButtonWheelDown:
			m.offset += 3
			if m.offset > maxOffset {
				m.offset = maxOffset
			}
		}
	}
	return m, nil
}

func (m detailModel) updateInput(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input = inputNone
		m.inputText = ""
		return m, nil
	case "enter":
		mode := m.input
		text := m.inputText
		id := m.ticket.ID
		m.input = inputNone
		m.inputText = ""

		switch mode {
		case inputNote:
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return addNoteMsg{id: id, text: trimmed}
			}
		case inputMove:
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return moveTicketMsg{id: id, targetRepo: trimmed}
			}
		}
		return m, nil
	case "backspace":
		if len(m.inputText) > 0 {
			m.inputText = m.inputText[:len(m.inputText)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.inputText += msg.String()
		}
	}
	return m, nil
}

func (m detailModel) updatePicker(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input = inputNone
		m.pickerItems, m.pickerIDs = nil, nil
		return m, nil
	case "up", "k":
		if m.pickerCursor > 0 {
			m.pickerCursor--
			m.clampPickerOffset()
		}
	case "down", "j":
		if m.pickerCursor < len(m.pickerItems)-1 {
			m.pickerCursor++
			m.clampPickerOffset()
		}
	case "enter":
		if m.input == inputChildPicker {
			qid := m.pickerIDs[m.pickerCursor]
			m.input = inputNone
			m.pickerItems, m.pickerIDs = nil, nil
			m.pickerCursor = 0
			return m, func() tea.Msg { return openTicketMsg{qid: qid} }
		}
		selected := m.pickerItems[m.pickerCursor]
		m.pickerItems = nil
		m.pickerCursor = 0

		if selected == enterPathOption {
			m.input = inputMove
			m.inputText = ""
			return m, nil
		}

		id := m.ticket.ID
		m.input = inputNone
		return m, func() tea.Msg {
			return moveTicketMsg{id: id, targetRepo: selected}
		}
	}
	return m, nil
}

func (m detailModel) view() string {
	var b strings.Builder

	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.lines) {
		end = len(m.lines)
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.lines[i])
		b.WriteString("\n")
	}

	// Pad remaining.
	for i := end - m.offset; i < visible; i++ {
		b.WriteString("\n")
	}

	// Input bar (if active).
	if m.input == inputMovePicker || m.input == inputChildPicker {
		label := "move to repo:"
		if m.input == inputChildPicker {
			label = "open child:"
		}
		b.WriteString(inputLabelStyle.Render(label) + "\n")
		end := m.pickerOffset + m.pickerRows()
		if end > len(m.pickerItems) {
			end = len(m.pickerItems)
		}
		for i := m.pickerOffset; i < end; i++ {
			line := "  " + m.pickerItems[i]
			if i == m.pickerCursor {
				line = "> " + m.pickerItems[i]
			}
			// Width 0 means the size isn't known yet — leave the line alone.
			// A long title otherwise wraps onto a second row the frame did
			// not budget for.
			if m.width > 0 {
				line = ansi.Truncate(line, m.width, "…")
			}
			b.WriteString(line + "\n")
		}
	} else if m.input != inputNone {
		var label string
		switch m.input {
		case inputNote:
			label = "note"
		case inputMove:
			label = "move to repo"
		}
		b.WriteString(inputLabelStyle.Render(label+": ") + m.inputText + "█\n")
	}

	// Help bar.
	for i, l := range m.helpLines() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(detailHelpStyle.Render(l))
	}

	return b.String()
}

func (m detailModel) render() []string {
	t := m.ticket
	var lines []string

	// Frontmatter fields.
	lines = append(lines, m.field("Title", ticket.SanitizeControl(t.Title)))
	lines = append(lines, m.field("ID", ticket.SanitizeControl(t.ID)))
	if t.Status != "" {
		lines = append(lines, m.field("Status", StatusBadge(t.Status)))
	}
	lines = append(lines, m.field("Type", TypeBadge(t.Type)))
	lines = append(lines, m.field("Priority", PriorityBadge(t.Priority)))

	if t.Parent != "" {
		parent := ticket.SanitizeControl(t.Parent)
		if m.snap != nil {
			if p, ok := m.snap.Get(ticket.QualifyRef(m.ns, t.Parent)); ok {
				parent += "  # " + ticket.SanitizeControl(p.Title)
			}
		}
		lines = append(lines, m.field("Parent", parent))
	}
	if len(t.Links) > 0 {
		lines = append(lines, m.field("Links", ticket.SanitizeControl(strings.Join(t.Links, ", "))))
	}
	if len(t.Tags) > 0 {
		lines = append(lines, m.field("Tags", ticket.SanitizeControl(strings.Join(t.Tags, ", "))))
	}
	if t.ExternalRef != "" {
		lines = append(lines, m.field("External Ref", ticket.SanitizeControl(t.ExternalRef)))
	}
	lines = append(lines, m.field("Created", t.Created.Local().Format("2006-01-02 15:04")))

	if len(t.Extra) > 0 {
		keys := make([]string, 0, len(t.Extra))
		for k := range t.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, m.field(ticket.SanitizeControl(k), ticket.SanitizeControl(t.Extra[k])))
		}
	}

	lines = append(lines, "")

	// Body sections.
	pad := "  "
	avail := m.width - len(pad)
	if avail < 20 {
		avail = 20
	}
	if len(t.Deps) > 0 {
		lines = append(lines, pad+sectionStyle.Render("## Dependencies"))
		lines = append(lines, "")
		deps := m.dependencyTree()
		for _, dep := range deps {
			status := string(dep.Status)
			if dep.Unknown {
				status = "unknown"
			}
			status = ticket.SanitizeControl(status)
			marker := "✗"
			if dep.Status == ticket.StatusDone || dep.Status == ticket.StatusClosed {
				marker = "✓"
			}
			line := fmt.Sprintf("%s %s [%s] %s", marker, ticket.SanitizeControl(dep.ID), status, ticket.SanitizeControl(dep.Title))
			if dep.RootBlocker {
				line += " ← root blocker"
			}
			if dep.Repeated {
				line += " (dependencies shown above)"
			}
			width := m.width
			if width <= 0 {
				width = avail + len(pad)
			}
			indentWidth := min(len(pad)+2*dep.Depth, max(0, width-20))
			indent := strings.Repeat(" ", indentWidth)
			for _, wl := range strings.Split(ansi.Wrap(line, width-indentWidth, ""), "\n") {
				lines = append(lines, indent+wl)
			}
		}
		lines = append(lines, "")
	}
	body := t.Body
	if body != "" {
		for _, line := range strings.Split(body, "\n") {
			line = ticket.SanitizeControl(line)
			if strings.HasPrefix(line, "## ") {
				lines = append(lines, pad+sectionStyle.Render(line))
			} else {
				for _, wl := range wrapText(line, avail) {
					lines = append(lines, pad+wl.text)
				}
			}
		}
	}

	// Notes.
	if len(t.Notes) > 0 {
		lines = append(lines, pad+sectionStyle.Render("## Notes"))
		lines = append(lines, "")
		for _, n := range t.Notes {
			lines = append(lines, pad+timestampStyle.Render(n.Timestamp.Local().Format("2006-01-02 15:04:05")))
			for _, wl := range wrapText(n.Text, avail) {
				lines = append(lines, pad+ticket.SanitizeControl(wl.text))
			}
			lines = append(lines, "")
		}
	}

	// Relationships, as `tk show` prints them: an epic's complete membership
	// and the counts its status was derived from, off the same graph; a leaf's
	// reason for being no epic's child. Wrapped, so a diagnostic naming a path
	// reaches the user whole.
	if t.Type == ticket.TypeEpic && m.snap != nil {
		if children := m.children(); len(children) > 0 {
			lines = append(lines, pad+sectionStyle.Render("## Children"))
			lines = append(lines, "")
			for _, c := range children {
				for _, wl := range wrapText(childLine(c), avail) {
					lines = append(lines, pad+wl.text)
				}
			}
			lines = append(lines, "")
		}
		p := m.snap.Progress(m.qid)
		lines = append(lines, pad+sectionStyle.Render("## Progress"))
		lines = append(lines, "")
		counts := fmt.Sprintf("children: %d — done %d, closed %d, open %d, ready %d, backlog %d", p.Total, p.Done, p.Closed, p.Open, p.Ready, p.Backlog)
		for _, wl := range wrapText(counts, avail) {
			lines = append(lines, pad+wl.text)
		}
		if p.Complete {
			lines = append(lines, pad+"complete: yes")
		} else {
			lines = append(lines, pad+StyleWarning.Render("complete: no"))
			for _, d := range p.Diagnostics {
				for _, wl := range wrapText(ticket.SanitizeControl(d), avail) {
					lines = append(lines, pad+StyleWarning.Render(wl.text))
				}
			}
		}
	} else if issue := ticket.RelationshipIssue(t); issue != "" {
		lines = append(lines, pad+sectionStyle.Render("## Relationship"))
		lines = append(lines, "")
		for _, wl := range wrapText(ticket.SanitizeControl(issue), avail) {
			lines = append(lines, pad+StyleWarning.Render(wl.text))
		}
	}

	// Findings: what `tk audit` holds against the ticket, so an invalid
	// parent is visible here before an edit is refused for it. The section
	// joins the lines like the others: the view clips them to the frame and
	// the scroll bound is computed over all of them.
	if findings := findingLines(m.findings, m.auditErr); len(findings) > 0 {
		if lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, pad+sectionStyle.Render("## Findings"))
		lines = append(lines, "")
		for _, line := range findings {
			for _, wl := range wrapText(line, avail) {
				lines = append(lines, pad+StyleWarning.Render(wl.text))
			}
		}
	}

	return lines
}

// findingLines is one line per finding in the vocabulary `tk audit` and `tk
// show` print them, without the ID — the reader is looking at that ticket.
// The kinds are ours and print bare; everything read off the store is
// sanitized before it reaches the terminal, and a stored status that is not
// one of ours is quoted the way the audit quotes it. An audit that failed is
// one line saying so, since an unchecked ticket must not read as clean.
func findingLines(f ticket.Findings, auditErr error) []string {
	var out []string
	if v := f.Parent; v != nil {
		out = append(out, fmt.Sprintf("%s  parent: %s  (%s)", v.Kind, ticket.SanitizeControl(v.Parent), ticket.SanitizeControl(v.Detail)))
	}
	if d := f.EpicStatus; d != nil {
		stored := ticket.SanitizeControl(string(d.Stored))
		if ticket.ValidateStatus(d.Stored) != nil {
			stored = fmt.Sprintf("%q", stored)
		}
		out = append(out, fmt.Sprintf("%s  stored: %s  reads: %s", d.Kind, stored, d.Derived))
	}
	for _, c := range f.Content {
		switch c.Kind {
		case ticket.ContentEnvelopeFragment:
			out = append(out, fmt.Sprintf("%s  %s: %q", c.Kind, ticket.SanitizeControl(c.Field), ticket.SanitizeControl(c.Detail)))
		case ticket.ContentBareAcceptance:
			out = append(out, fmt.Sprintf("%s  %d bare criterion(s)", c.Kind, c.Bare))
		case ticket.ContentLegacyReviewLog:
			out = append(out, fmt.Sprintf("%s  %d bytes", c.Kind, c.Bytes))
		default:
			out = append(out, string(c.Kind))
		}
	}
	if auditErr != nil {
		out = append(out, "not checked: "+ticket.SanitizeControl(auditErr.Error()))
	}
	return out
}

func (m detailModel) dependencyTree() []ticket.DepNode {
	if m.snap != nil {
		return m.snap.DependencyTree(m.qid)
	}
	nodes := make([]ticket.DepNode, 0, len(m.ticket.Deps))
	for _, id := range m.ticket.Deps {
		nodes = append(nodes, ticket.DepNode{ID: id, Title: "(not found)", Unknown: true, RootBlocker: true})
	}
	return nodes
}

func (m detailModel) field(key, val string) string {
	return "  " + fieldKeyStyle.Render(key+":") + " " + fieldValStyle.Render(val)
}
