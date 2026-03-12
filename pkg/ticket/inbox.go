package ticket

import (
	"sort"
	"time"
)

// ActionKind describes what type of action is needed on a ticket.
type ActionKind string

const (
	ActionHumanReview ActionKind = "human-review"
	ActionAgentReview ActionKind = "agent-review"
	ActionHumanInput  ActionKind = "human-input"
	ActionAgentWork   ActionKind = "agent-work"
	ActionBlocked     ActionKind = "blocked"
	ActionReady       ActionKind = "ready"
)

// InboxItem represents a ticket needing attention, with context about what's needed.
type InboxItem struct {
	Ticket *Ticket
	Action ActionKind
	Detail string    // Human-readable description of what's needed.
	Since  time.Time // When this action became pending.
}

// ConversationalStages marks stages where human back-and-forth is expected.
var ConversationalStages = map[Stage]bool{
	StageTriage: true,
	StageSpec:   true,
	StageVerify: true,
}

// NextAction computes the next action needed for a single ticket.
func NextAction(t *Ticket) InboxItem {
	item := InboxItem{Ticket: t, Since: t.Created}

	if t.Stage == "" || t.Stage == StageDone {
		item.Action = ActionReady
		item.Detail = "no action needed"
		return item
	}

	// Check review state first.
	switch t.Review {
	case ReviewPending:
		if ConversationalStages[t.Stage] {
			item.Action = ActionHumanReview
			item.Detail = "awaiting human review at " + string(t.Stage)
		} else {
			item.Action = ActionAgentReview
			item.Detail = "awaiting agent review at " + string(t.Stage)
		}
		return item
	case ReviewRejected:
		item.Action = ActionAgentWork
		item.Detail = "review rejected at " + string(t.Stage) + " — needs rework"
		return item
	}

	// No pending review — determine action by stage.
	switch t.Stage {
	case StageTriage:
		item.Action = ActionHumanInput
		item.Detail = "needs triage"
	case StageSpec:
		item.Action = ActionAgentWork
		item.Detail = "spec needs drafting"
	case StageDesign:
		item.Action = ActionAgentWork
		item.Detail = "design needs drafting"
	case StageImplement:
		item.Action = ActionAgentWork
		item.Detail = "ready for implementation"
	case StageTest:
		item.Action = ActionAgentWork
		item.Detail = "ready for testing"
	case StageVerify:
		item.Action = ActionHumanReview
		item.Detail = "ready for human verification"
	default:
		item.Action = ActionReady
		item.Detail = "at stage " + string(t.Stage)
	}

	return item
}

// Inbox returns tickets needing human attention, sorted by priority then age.
func Inbox(store *FileStore) ([]InboxItem, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	var items []InboxItem
	for _, t := range tickets {
		if t.Stage == "" || t.Stage == StageDone {
			continue
		}
		item := NextAction(t)
		if item.Action == ActionHumanReview || item.Action == ActionHumanInput {
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
	Epic           *Ticket
	Total          int
	StageBreakdown map[Stage]int
	NextActions    []InboxItem
	CompletionPct  float64
}

// Projects returns active epics with their child progress, sorted by
// priority then completeness (least complete first).
func Projects(store *FileStore) ([]ProjectSummary, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	// Find epics.
	epics := make(map[string]*Ticket)
	for _, t := range tickets {
		if t.Type == TypeEpic && t.Stage != StageDone {
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
			Epic:           epic,
			Total:          len(kids),
			StageBreakdown: make(map[Stage]int),
		}

		doneCount := 0
		for _, kid := range kids {
			stage := kid.Stage
			summary.StageBreakdown[stage]++
			if stage == StageDone {
				doneCount++
			}

			action := NextAction(kid)
			if action.Action == ActionHumanReview || action.Action == ActionHumanInput {
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
