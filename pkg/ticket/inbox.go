package ticket

import (
	"sort"
	"time"
)

// ActionKind describes what type of action is needed on a ticket.
type ActionKind string

const (
	ActionWork    ActionKind = "work"
	ActionBlocked ActionKind = "blocked"
	ActionReady   ActionKind = "ready"
)

// InboxItem represents a ticket needing attention, with context about what's needed.
type InboxItem struct {
	Ticket *Ticket
	Action ActionKind
	Detail string    // Human-readable description of what's needed.
	Since  time.Time // When this action became pending.
}

// NextAction computes the next action needed for a single ticket.
func NextAction(t *Ticket) InboxItem {
	item := InboxItem{Ticket: t, Since: t.Created}

	switch t.Status {
	case StatusBacklog, StatusDone, StatusClosed, "":
		item.Action = ActionReady
		item.Detail = "no action needed"
	case StatusReady:
		item.Action = ActionWork
		item.Detail = "ready for work"
	case StatusOpen:
		item.Action = ActionWork
		item.Detail = "in progress"
	default:
		item.Action = ActionReady
		item.Detail = "unknown status"
	}

	return item
}

// Inbox returns actionable tickets (ready or open), sorted by priority then age.
func Inbox(store Store) ([]InboxItem, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	var items []InboxItem
	for _, t := range tickets {
		if t.Status == StatusDone || t.Status == StatusClosed || t.Status == StatusBacklog {
			continue
		}
		item := NextAction(t)
		if item.Action == ActionWork {
			items = append(items, item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Ticket.Priority != items[j].Ticket.Priority {
			return items[i].Ticket.Priority < items[j].Ticket.Priority
		}
		return items[i].Since.Before(items[j].Since)
	})

	return items, nil
}

// ProjectSummary aggregates progress for an epic/parent ticket.
type ProjectSummary struct {
	Epic            *Ticket
	Total           int
	StatusBreakdown map[Status]int
	NextActions     []InboxItem
	CompletionPct   float64
}

// Projects returns active epics with their child progress, sorted by
// priority then completeness (least complete first).
func Projects(store Store) ([]ProjectSummary, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	// Find epics.
	epics := make(map[string]*Ticket)
	for _, t := range tickets {
		if t.Type == TypeEpic && t.Status != StatusDone && t.Status != StatusClosed {
			epics[t.ID] = t
		}
	}

	// Group children by parent.
	children := make(map[string][]*Ticket)
	for _, t := range tickets {
		if t.Parent != "" {
			if _, ok := epics[t.Parent]; ok {
				children[t.Parent] = append(children[t.Parent], t)
			}
		}
	}

	var summaries []ProjectSummary
	for id, epic := range epics {
		kids := children[id]
		summary := ProjectSummary{
			Epic:            epic,
			Total:           len(kids),
			StatusBreakdown: make(map[Status]int),
		}

		doneCount := 0
		for _, kid := range kids {
			summary.StatusBreakdown[kid.Status]++
			if kid.Status == StatusDone || kid.Status == StatusClosed {
				doneCount++
			}

			action := NextAction(kid)
			if action.Action == ActionWork {
				summary.NextActions = append(summary.NextActions, action)
			}
		}

		if summary.Total > 0 {
			summary.CompletionPct = float64(doneCount) / float64(summary.Total) * 100
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Epic.Priority != summaries[j].Epic.Priority {
			return summaries[i].Epic.Priority < summaries[j].Epic.Priority
		}
		return summaries[i].CompletionPct < summaries[j].CompletionPct
	})

	return summaries, nil
}
