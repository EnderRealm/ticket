package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
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
	lsCmd.Flags().String("stage", "", "filter by stage")
	lsCmd.Flags().String("parent", "", "filter by parent ticket ID")
	lsCmd.Flags().String("group-by", "", "group by: workflow | pipeline | type | priority")
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

	opts := parseFilterFlags(cmd)

	flat, _ := cmd.Flags().GetBool("flat")
	groupBy, _ := cmd.Flags().GetString("group-by")
	if shorthand, _ := cmd.Flags().GetBool("group"); shorthand && groupBy == "" {
		groupBy = "workflow"
	}

	stageFilter, _ := cmd.Flags().GetString("stage")
	if stageFilter != "" {
		opts.Stage = ticket.Stage(stageFilter)
	} else {
		// No explicit stage: exclude done.
		var filtered []*ticket.Ticket
		for _, t := range tickets {
			if t.Stage != ticket.StageDone {
				filtered = append(filtered, t)
			}
		}
		tickets = filtered
	}

	// Default to workflow grouping unless flat or explicit group-by.
	if groupBy == "" && !flat && stageFilter == "" {
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

	ticket.SortByStagePriorityID(tickets)
	printHeader()
	for _, t := range tickets {
		printRow(t)
	}
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
		case "pipeline":
			name, order = pipelineGroup(t)
		case "type":
			name = string(t.Type)
			order = ticket.TypeOrder(t.Type)
		case "priority":
			name = fmt.Sprintf("P%d", t.Priority)
			order = t.Priority
		default:
			return fmt.Errorf("unknown group-by value: %s (use: workflow, pipeline, type, priority)", groupBy)
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

	first := true
	for _, name := range groupOrder {
		g := groups[name]
		if len(g.tickets) == 0 {
			continue
		}

		ticket.SortByStagePriorityID(g.tickets)

		if !first {
			fmt.Println()
		}
		first = false

		fmt.Printf("=== %s ===\n", g.name)
		printHeader()
		for _, t := range g.tickets {
			printRow(t)
		}
	}

	return nil
}

func workflowGroup(store *ticket.FileStore, t *ticket.Ticket) (string, int) {
	if ticket.IsBlocked(store, t) {
		return "Blocked", 99
	}
	stage := t.Stage
	if stage == "" {
		return "Unknown", 100
	}
	return string(stage), ticket.StageIndex(t.Type, stage)
}

func printHeader() {
	fmt.Printf("%-9s %-3s %-11s %-14s %s\n", "ID", "P", "TYPE", "STATUS", "TITLE")
}

func printRow(t *ticket.Ticket) {
	depStr := ""
	if n := len(t.Deps); n == 1 {
		depStr = " (1 dep)"
	} else if n > 1 {
		depStr = fmt.Sprintf(" (%d deps)", n)
	}

	fmt.Printf("%-9s P%d  %-11s %-14s %s%s\n",
		t.ID, t.Priority, t.Type, t.Stage, t.Title, depStr)
}

func pipelineGroup(t *ticket.Ticket) (string, int) {
	stage := t.Stage
	if stage == "" {
		return "unknown", 99
	}
	return string(stage), ticket.StageIndex(t.Type, stage)
}

// addFilterFlags registers shared filter flags on a command.
func addFilterFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("assignee", "a", "", "filter by assignee")
	cmd.Flags().StringP("tag", "T", "", "filter by tag")
	cmd.Flags().StringP("priority", "P", "", "filter by priority")
	cmd.Flags().StringP("type", "t", "", "filter by type")
}

// parseFilterFlags reads shared filter flags into ListOptions.
func parseFilterFlags(cmd *cobra.Command) ticket.ListOptions {
	opts := ticket.DefaultListOptions()

	if v, _ := cmd.Flags().GetString("assignee"); v != "" {
		opts.Assignee = v
	}
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
	return opts
}
