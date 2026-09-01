package ticket

import (
	"sort"
	"strings"
	"time"
)

// ActionKind describes what type of action is needed on a ticket.
type ActionKind string

const (
	ActionWork    ActionKind = "work"
	ActionBlocked ActionKind = "blocked"
	ActionReady   ActionKind = "ready"
)

// QuestionField is the extra field a runner writes when it parks a run on a
// question for a human; a ticket carrying one needs an answer before work
// resumes, whatever its status says.
const QuestionField = "question"

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

	if q := strings.TrimSpace(t.Extra[QuestionField]); q != "" {
		item.Action = ActionBlocked
		item.Detail = q
		return item
	}

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

// Inbox returns actionable tickets (ready or open), sorted by priority then
// age. A parked ticket is blocked rather than work, and is kept: it is the one
// the human most needs to see.
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
		if item.Action == ActionWork || item.Action == ActionBlocked {
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

	// Group children by bare parent ID. A map key can't tolerate the namespace
	// mismatch the way SameTicketID does, and the central store records
	// children with a namespaced parent while tickets written before the
	// namespacing rollout record it bare.
	children := make(map[string][]*Ticket)
	for _, t := range tickets {
		if t.Parent != "" {
			_, parent := ParseNamespacedID(t.Parent)
			children[parent] = append(children[parent], t)
		}
	}

	// One summary per active epic, keyed off the epic's full ID — MultiStore
	// namespaces IDs, and two projects can hold the same bare ID.
	var summaries []ProjectSummary
	for _, epic := range tickets {
		if epic.Type != TypeEpic || epic.Status == StatusDone || epic.Status == StatusClosed {
			continue
		}
		_, bareID := ParseNamespacedID(epic.ID)
		kids := children[bareID]
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
				// A finished child is no action whatever it carries: a stale
				// question on it would otherwise leave the epic reading
				// complete and still holding an outstanding action.
				continue
			}

			action := NextAction(kid)
			if action.Action == ActionWork || action.Action == ActionBlocked {
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
