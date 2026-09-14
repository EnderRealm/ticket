package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
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
	lsCmd.Flags().Bool("all", false, "include done and closed tickets (hidden by default; ignored with --status)")
	lsCmd.Flags().String("parent", "", "children of this epic (qualified project/id, or bare in the selected project); membership is global, the listing is the selected project's")
	lsCmd.Flags().Bool("all-projects", false, "list every namespace in the central store, IDs qualified")
	lsCmd.Flags().String("field", "", "filter by extra field (key=value, substring match)")
	lsCmd.Flags().String("group-by", "", "group by: workflow | type | priority")
	lsCmd.Flags().Bool("group", false, "shorthand for --group-by=workflow")
	lsCmd.Flags().Bool("flat", false, "flat list (no grouping)")

	rootCmd.AddCommand(lsCmd)
}

func runLs(cmd *cobra.Command, args []string) error {
	// The listing is one namespace's or every namespace's; the parent filter
	// below is answered off the whole graph either way.
	var store ticket.Store
	var snapshot func() (*ticket.Snapshot, error)
	var listFrom func(*ticket.Snapshot) []*ticket.Ticket
	ns := ""
	allProjects, _ := cmd.Flags().GetBool("all-projects")
	if allProjects {
		ms, err := allProjectsStore()
		if err != nil {
			return err
		}
		store, snapshot, listFrom = ms, ms.Snapshot, ms.ListFromSnapshot
	} else {
		fs := TicketStore()
		store, snapshot, listFrom, ns = fs, fs.Snapshot, fs.ListFromSnapshot, fs.Project
	}
	parentArg, _ := cmd.Flags().GetString("parent")
	if p, _ := ticket.ParseNamespacedID(parentArg); parentArg != "" && allProjects && p == "" {
		return fmt.Errorf("--parent %q must be qualified (project/id) with --all-projects", parentArg)
	}
	// With a parent, the rows come off the same snapshot that decides the
	// membership below, so the two cannot disagree about a child written in
	// between.
	var tickets []*ticket.Ticket
	var snap *ticket.Snapshot
	var err error
	if parentArg != "" {
		if snap, err = snapshot(); err != nil {
			return err
		}
		tickets = listFrom(snap)
	} else if tickets, err = store.List(); err != nil {
		return err
	}
	// Built from the whole listing, before the filters narrow it: the workflow
	// grouping asks whether each row is blocked, and a dep naming an epic
	// resolves through the store, which reads every ticket to derive it.
	blocked := ticket.BlockedFunc(store, tickets)

	opts, err := parseFilterFlags(cmd)
	if err != nil {
		return err
	}

	// Membership is decided by the snapshot, which places a child under its
	// epic across namespaces by exact qualified ID — never by a suffix the
	// listing happens to share. The listing only narrows what is shown, and
	// says so when the narrowing hides a child.
	if parentArg != "" {
		parentID, err := snap.Resolve(ns, parentArg)
		if err != nil {
			return err
		}
		if p, ok := snap.Get(parentID); ok && p.Type != ticket.TypeEpic {
			return fmt.Errorf("--parent %s is type %s, not an epic", parentID, p.Type)
		}
		children := snap.Children(parentID)
		member := make(map[string]bool, len(children))
		for _, c := range children {
			member[c.ID] = true
		}
		var kept []*ticket.Ticket
		for _, t := range tickets {
			if member[ticket.FormatNamespacedID(ns, t.ID)] {
				kept = append(kept, t)
			}
		}
		if len(kept) < len(children) {
			fmt.Fprintf(os.Stderr, "children of %s: %d across namespaces, %d in %s (--all-projects lists them all)\n", parentID, len(children), len(kept), ns)
		}
		tickets = kept
	}

	flat, _ := cmd.Flags().GetBool("flat")
	groupBy, _ := cmd.Flags().GetString("group-by")
	if shorthand, _ := cmd.Flags().GetBool("group"); shorthand && groupBy == "" {
		groupBy = "workflow"
	}

	statusFilter, _ := cmd.Flags().GetString("status")
	all, _ := cmd.Flags().GetBool("all")
	if statusFilter != "" {
		opts.Status = ticket.Status(statusFilter)
	} else if !all {
		// No explicit status: show live work only, so finished tickets do not
		// bury the rows a reader scans for.
		var filtered []*ticket.Ticket
		for _, t := range tickets {
			if t.Status != ticket.StatusDone && t.Status != ticket.StatusClosed {
				filtered = append(filtered, t)
			}
		}
		tickets = filtered
	}

	// Default to workflow grouping unless flat or explicit group-by.
	if groupBy == "" && !flat && statusFilter == "" {
		groupBy = "workflow"
	}

	tickets = ticket.Filter(tickets, opts)

	if len(tickets) == 0 {
		printEmptyMessage()
		return nil
	}

	if groupBy != "" {
		return printGrouped(blocked, tickets, groupBy)
	}

	ticket.SortByStatusPriorityID(tickets)
	newTableWriter().Print(tickets)
	return nil
}

func printGrouped(blocked func(*ticket.Ticket) bool, tickets []*ticket.Ticket, groupBy string) error {
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
			name, order = workflowGroup(blocked, t)
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

func workflowGroup(blocked func(*ticket.Ticket) bool, t *ticket.Ticket) (string, int) {
	if blocked(t) {
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
