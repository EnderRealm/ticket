package ticket

import (
	"sort"
	"strings"
)

// ListOptions carries filter parameters for listing tickets.
type ListOptions struct {
	Status   Status
	Type     TicketType
	Priority int // -1 means no filter
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
		if opts.Status != "" && t.Status != opts.Status {
			continue
		}
		if opts.Type != "" && t.Type != opts.Type {
			continue
		}
		if opts.Priority >= 0 && t.Priority != opts.Priority {
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

// SortByStatusPriorityID sorts tickets by status order, then priority (asc),
// then ID. This is the default sort for ls/ready/blocked.
func SortByStatusPriorityID(tickets []*Ticket) {
	sort.SliceStable(tickets, func(i, j int) bool {
		si := StatusOrder(tickets[i].Status)
		sj := StatusOrder(tickets[j].Status)
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

// TypeOrder returns the sort rank for a ticket type.
func TypeOrder(t TicketType) int {
	switch t {
	case TypeEpic:
		return 1
	case TypeFeature:
		return 2
	case TypeBug:
		return 3
	default:
		return 4
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
