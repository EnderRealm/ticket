package tui

import (
	"fmt"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/charmbracelet/lipgloss"
)

// epicChildren returns the board's own tickets among an epic's children: the
// graph's membership — every child in every namespace, by exact qualified ID —
// narrowed to this namespace and re-listed as the board's objects, so a child
// row is the row the cursor and selection logic already know. A foreign child
// is counted by epicProgress and listed by the epic's detail, never nested
// here: the board is a slice of the epic, and the label says so.
func (m dashboardModel) epicChildren(t *ticket.Ticket) []*ticket.Ticket {
	if m.snap == nil {
		return nil
	}
	var children []*ticket.Ticket
	for _, child := range m.snap.Children(m.qid(t)) {
		if local, ok := m.byQID[child.ID]; ok {
			children = append(children, local)
		}
	}
	return children
}

// toggleExpand flips the expansion of the epic group at the cursor and reports
// whether it did. A child row is part of a group rather than a group itself, so
// the caller can fall through to opening it.
func (m *dashboardModel) toggleExpand() bool {
	if m.activeTab != tabEpics || m.cursor < 0 || m.cursor >= len(m.rows) {
		return false
	}
	r := m.rows[m.cursor]
	if r.child {
		return false
	}
	if m.expanded == nil {
		m.expanded = make(map[string]bool)
	}
	id := r.item.Ticket.ID
	m.expanded[id] = !m.expanded[id]
	m.rebuildRows()
	m.clampOffset()
	return true
}

// focusEpic moves the cursor to the epic's group row, if the tab shows it.
func (m *dashboardModel) focusEpic(id string) {
	for i, r := range m.rows {
		if !r.child && r.item.Ticket.ID == id {
			m.cursor = i
			m.clampOffset()
			return
		}
	}
}

// expandIndicator is the one-character expand/collapse marker for an epic group.
func expandIndicator(expanded bool) string {
	if expanded {
		return "▾"
	}
	return "▸"
}

// progressMarkers is what qualifies an epic's global count on a board: a
// slice marker when the board lists fewer children than the epic has, and an
// incomplete marker when the graph could not be read in full — a total over a
// partial read is a lower bound, and rendering it as a settled figure would
// report an epic complete that may not be.
func (m dashboardModel) progressMarkers(t *ticket.Ticket, p ticket.EpicProgress, selBg lipgloss.Style) string {
	var out string
	if local := len(m.epicChildren(t)); local != p.Total {
		out += selBg.Foreground(colorSubtle).Render(fmt.Sprintf(" · slice %d of %d local", local, p.Total))
	}
	if !p.Complete {
		out += selBg.Render(" · ") + selBg.Foreground(colorWarning).Render("incomplete")
	}
	return out
}

// epicRollup renders the backlog tab's "(N children)" beside an epic: the
// global count, qualified by progressMarkers.
func (m dashboardModel) epicRollup(t *ticket.Ticket, selBg lipgloss.Style) string {
	if m.snap == nil {
		return selBg.Foreground(colorSubtle).Render("  (0 children)")
	}
	p := m.snap.Progress(m.qid(t))
	return selBg.Foreground(colorSubtle).Render(fmt.Sprintf("  (%d children)", p.Total)) + m.progressMarkers(t, p, selBg)
}

// epicProgress renders an epic's finished-children ratio and bar, or only
// the markers when it has no children — an epic whose children all sit in
// unreadable files still owes the incomplete marker. The figures are the
// graph's, over every namespace, with done and closed told apart: a closed
// child finishes the epic without the work having been done.
func (m dashboardModel) epicProgress(t *ticket.Ticket, selBg lipgloss.Style) string {
	if m.snap == nil {
		return ""
	}
	p := m.snap.Progress(m.qid(t))
	if p.Total == 0 {
		return m.progressMarkers(t, p, selBg)
	}
	finished := p.Done + p.Closed
	ratio := StyleDim.Render(fmt.Sprintf("%d/%d (%d done, %d closed)", finished, p.Total, p.Done, p.Closed))
	return selBg.Render(fmt.Sprintf("  %s  %s", ratio, ProgressBar(finished, p.Total, 15))) + m.progressMarkers(t, p, selBg)
}
