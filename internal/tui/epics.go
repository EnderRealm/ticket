package tui

import (
	"fmt"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/charmbracelet/lipgloss"
)

// childrenByParent groups tickets under their parent's bare ID. A map key can't
// tolerate the namespace mismatch the way SameTicketID does, and the central
// store records children with a namespaced parent while tickets written before
// the namespacing rollout record it bare. The backlog rollup count and the
// epics tab's expansion read the same map, so both report the same set.
func childrenByParent(tickets []*ticket.Ticket) map[string][]*ticket.Ticket {
	children := make(map[string][]*ticket.Ticket)
	for _, t := range tickets {
		if t.Parent == "" {
			continue
		}
		_, parent := ticket.ParseNamespacedID(t.Parent)
		children[parent] = append(children[parent], t)
	}
	return children
}

// epicChildren returns the tickets that name t as their parent.
func (m dashboardModel) epicChildren(t *ticket.Ticket) []*ticket.Ticket {
	_, bareID := ticket.ParseNamespacedID(t.ID)
	return m.children[bareID]
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

// epicProgress renders an epic's completed-children ratio and bar, or "" when
// it has no children.
func (m dashboardModel) epicProgress(t *ticket.Ticket, selBg lipgloss.Style) string {
	children := m.epicChildren(t)
	if len(children) == 0 {
		return ""
	}
	done := 0
	for _, child := range children {
		if child.Status == ticket.StatusDone || child.Status == ticket.StatusClosed {
			done++
		}
	}
	ratio := StyleDim.Render(fmt.Sprintf("%d/%d", done, len(children)))
	return selBg.Render(fmt.Sprintf("  %s  %s", ratio, ProgressBar(done, len(children), 15)))
}
