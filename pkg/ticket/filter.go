package ticket

import (
	"sort"
	"strings"
)

// ListOptions carries filter parameters for listing tickets.
type ListOptions struct {
	Stage    Stage
	Type     TicketType
	Priority int // -1 means no filter
	Assignee string
	Tag      string
	Parent   string
}

// DefaultListOptions returns options with no filters applied.
func DefaultListOptions() ListOptions {
	return ListOptions{Priority: -1}
}

// Filter returns tickets matching all non-zero fields in opts.
func Filter(tickets []*Ticket, opts ListOptions) []*Ticket {
	var result []*Ticket
	for _, t := range tickets {
		if opts.Stage != "" && t.Stage != opts.Stage {
			continue
		}
		if opts.Type != "" && t.Type != opts.Type {
			continue
		}
		if opts.Priority >= 0 && t.Priority != opts.Priority {
			continue
		}
		if opts.Assignee != "" && !strings.EqualFold(t.Assignee, opts.Assignee) {
			continue
		}
		if opts.Tag != "" && !hasTag(t.Tags, opts.Tag) {
			continue
		}
		if opts.Parent != "" && t.Parent != opts.Parent {
			continue
		}
		result = append(result, t)
	}
	return result
}

// SortByStagePriorityID sorts tickets by stage order, then priority (asc),
// then ID. This is the default sort for ls/ready/blocked.
func SortByStagePriorityID(tickets []*Ticket) {
	sort.SliceStable(tickets, func(i, j int) bool {
		si := StageIndex(tickets[i].Type, tickets[i].Stage)
		sj := StageIndex(tickets[j].Type, tickets[j].Stage)
		if si != sj {
			return si < sj
		}
		if tickets[i].Priority != tickets[j].Priority {
			return tickets[i].Priority < tickets[j].Priority
		}
		return tickets[i].ID < tickets[j].ID
	})
}

// SortByPriorityID sorts by priority then ID. Used when grouping by status.
func SortByPriorityID(tickets []*Ticket) {
	sort.SliceStable(tickets, func(i, j int) bool {
		if tickets[i].Priority != tickets[j].Priority {
			return tickets[i].Priority < tickets[j].Priority
		}
		return tickets[i].ID < tickets[j].ID
	})
}

// TypeOrder returns the sort rank for a ticket type, matching the bash impl.
func TypeOrder(t TicketType) int {
	switch t {
	case TypeEpic:
		return 1
	case TypeFeature:
		return 2
	case TypeTask:
		return 3
	case TypeBug:
		return 4
	case TypeChore:
		return 5
	default:
		return 6
	}
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}
