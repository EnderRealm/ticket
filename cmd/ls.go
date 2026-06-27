package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List tickets",
	RunE:    runLs,
}

func init() {
	addFilterFlags(lsCmd)
	lsCmd.Flags().String("status", "", "filter by status")
	lsCmd.Flags().String("parent", "", "filter by parent ticket ID")
	lsCmd.Flags().String("field", "", "filter by extra field (key=value, substring match)")
	lsCmd.Flags().String("group-by", "", "group by: workflow | type | priority")
	lsCmd.Flags().Bool("group", false, "shorthand for --group-by=workflow")
	lsCmd.Flags().Bool("flat", false, "flat list (no grouping)")

	rootCmd.AddCommand(lsCmd)
}

func runLs(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	opts, err := parseFilterFlags(cmd)
	if err != nil {
		return err
	}

	flat, _ := cmd.Flags().GetBool("flat")
	groupBy, _ := cmd.Flags().GetString("group-by")
	if shorthand, _ := cmd.Flags().GetBool("group"); shorthand && groupBy == "" {
		groupBy = "workflow"
	}

	statusFilter, _ := cmd.Flags().GetString("status")
	if statusFilter != "" {
		opts.Status = ticket.Status(statusFilter)
	} else {
		// No explicit status: exclude closed (non-closed default).
		var filtered []*ticket.Ticket
		for _, t := range tickets {
			if t.Status != ticket.StatusClosed {
				filtered = append(filtered, t)
			}
		}
		tickets = filtered
	}

	// Default to workflow grouping unless flat or explicit group-by.
	if groupBy == "" && !flat && statusFilter == "" {
		groupBy = "workflow"
	}

	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		opts.Parent = v
	}

	tickets = ticket.Filter(tickets, opts)

	if len(tickets) == 0 {
		printEmptyMessage()
		return nil
	}

	if groupBy != "" {
		return printGrouped(store, tickets, groupBy)
	}

	ticket.SortByStatusPriorityID(tickets)
	newTableWriter().Print(tickets)
	return nil
}

func printGrouped(store *ticket.FileStore, tickets []*ticket.Ticket, groupBy string) error {
	type group struct {
		name    string
		order   int
		tickets []*ticket.Ticket
	}

	groups := map[string]*group{}
	var groupOrder []string

	for _, t := range tickets {
		var name string
		var order int

		switch groupBy {
		case "workflow":
			name, order = workflowGroup(store, t)
		case "type":
			name = string(t.Type)
			order = ticket.TypeOrder(t.Type)
		case "priority":
			name = fmt.Sprintf("P%d", t.Priority)
			order = t.Priority
		default:
			return fmt.Errorf("unknown group-by value: %s (use: workflow, type, priority)", groupBy)
		}

		g, ok := groups[name]
		if !ok {
			g = &group{name: name, order: order}
			groups[name] = g
			groupOrder = append(groupOrder, name)
		}
		g.tickets = append(g.tickets, t)
	}

	// Sort groups by order.
	sort.SliceStable(groupOrder, func(i, j int) bool {
		return groups[groupOrder[i]].order < groups[groupOrder[j]].order
	})

	// Determine which column to suppress based on grouping mode.
	var defaultSkip []string
	switch groupBy {
	case "workflow":
		defaultSkip = append(defaultSkip, "STATUS")
	case "type":
		defaultSkip = append(defaultSkip, "TYPE")
	case "priority":
		defaultSkip = append(defaultSkip, "P")
	}

	// Build table writers and compute widths globally across all tickets.
	tw := newTableWriter(defaultSkip...)
	tw.computeWidths(tickets)

	// Blocked group needs STATUS, so it gets its own writer with global widths.
	var blockedTW *tableWriter
	if groupBy == "workflow" {
		blockedTW = newTableWriter()
		blockedTW.computeWidths(tickets)
	}

	first := true
	for _, name := range groupOrder {
		g := groups[name]
		if len(g.tickets) == 0 {
			continue
		}

		ticket.SortByPriorityID(g.tickets)

		if !first {
			fmt.Println()
		}
		first = false

		fmt.Println(colorGroupHeader(fmt.Sprintf("=== %s ===", g.name)))

		if groupBy == "workflow" && g.name == "Blocked" {
			blockedTW.PrintGroup(g.tickets)
		} else {
			tw.PrintGroup(g.tickets)
		}
	}

	return nil
}

func workflowGroup(store *ticket.FileStore, t *ticket.Ticket) (string, int) {
	if ticket.IsBlocked(store, t) {
		return "Blocked", 99
	}
	status := t.Status
	if status == "" {
		return "Unknown", 100
	}
	return string(status), ticket.StatusOrder(status)
}

// addFilterFlags registers shared filter flags on a command.
func addFilterFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("tag", "T", "", "filter by tag")
	cmd.Flags().StringP("priority", "P", "", "filter by priority")
	cmd.Flags().StringP("type", "t", "", "filter by type")
}

// parseFilterFlags reads shared filter flags into ListOptions.
func parseFilterFlags(cmd *cobra.Command) (ticket.ListOptions, error) {
	opts := ticket.DefaultListOptions()

	if v, _ := cmd.Flags().GetString("tag"); v != "" {
		opts.Tag = v
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		// Strip leading P if present.
		v = strings.TrimPrefix(v, "P")
		var p int
		if _, err := fmt.Sscanf(v, "%d", &p); err == nil {
			opts.Priority = p
		}
	}
	if v, _ := cmd.Flags().GetString("type"); v != "" {
		opts.Type = ticket.TicketType(v)
	}
	if v, _ := cmd.Flags().GetString("field"); v != "" {
		key, value, err := ticket.ParseFieldFilter(v)
		if err != nil {
			return opts, fmt.Errorf("invalid --field: %w", err)
		}
		opts.FieldKey = key
		opts.FieldValue = value
	}
	return opts, nil
}
