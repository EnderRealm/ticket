package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:     "show <id> [id...]",
	Aliases: []string{"get"},
	Short:   "Display ticket details",
	Args:    cobra.MinimumNArgs(1),
	RunE:    runShow,
}

func init() {
	showCmd.Flags().Bool("metadata", false, "show only frontmatter fields and description")
	rootCmd.AddCommand(showCmd)
}

func runShow(cmd *cobra.Command, args []string) error {
	metadataOnly, _ := cmd.Flags().GetBool("metadata")
	store := TicketStore()
	for i, id := range args {
		if i > 0 {
			fmt.Println()
		}
		if err := showTicket(store, id, metadataOnly); err != nil {
			return err
		}
	}
	return nil
}

func showTicket(store *ticket.FileStore, id string, metadataOnly bool) error {
	t, err := store.Get(id)
	if err != nil {
		return err
	}

	if metadataOnly {
		// Serialize only frontmatter + title + description (no notes, relationships).
		meta := *t
		meta.Notes = nil
		data, err := ticket.Serialize(&meta)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	}

	// Get all tickets for relationship display.
	allTickets, _ := store.List()
	byID := map[string]*ticket.Ticket{}
	for _, tk := range allTickets {
		byID[tk.ID] = tk
	}

	// Serialize the base ticket content.
	data, err := ticket.Serialize(t)
	if err != nil {
		return err
	}

	// Render timestamps in local wall-clock time for human-facing output.
	output := localizeTimestamps(string(data), t)

	// Annotate parent line with title. The stored parent may be namespaced
	// while the parent's own ID is bare, so match it the way Children does.
	if t.Parent != "" {
		for _, tk := range allTickets {
			if ticket.SameTicketID(tk.ID, t.Parent) {
				output = strings.Replace(output,
					"parent: "+t.Parent,
					"parent: "+t.Parent+"  # "+tk.Title,
					1)
				break
			}
		}
	}

	fmt.Print(output)

	// Outputs: what the ticket produced, for downstream handoff.
	if len(t.Outputs) > 0 {
		keys := make([]string, 0, len(t.Outputs))
		for k := range t.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Print("\n## Outputs\n\n")
		for _, k := range keys {
			fmt.Printf("- %s: %s\n", k, t.Outputs[k])
		}
	}

	// Dep cargo: what flows across each annotated dependency edge.
	if len(t.DepCargo) > 0 {
		keys := make([]string, 0, len(t.DepCargo))
		for k := range t.DepCargo {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Print("\n## Dep Cargo\n\n")
		for _, k := range keys {
			fmt.Printf("- %s: %s\n", k, t.DepCargo[k])
		}
	}

	// Blockers: deps not at done status.
	var blockers []string
	for _, depID := range t.Deps {
		dep, ok := byID[depID]
		if !ok || dep.Status != ticket.StatusDone {
			blockers = append(blockers, depID)
		}
	}
	if len(blockers) > 0 {
		fmt.Print("\n## Blockers\n\n")
		for _, id := range blockers {
			if dep, ok := byID[id]; ok {
				fmt.Printf("- %s [%s] %s\n", id, dep.Status, dep.Title)
			} else {
				fmt.Printf("- %s [unknown]\n", id)
			}
		}
	}

	// Blocking: tickets that depend on this one and aren't done.
	var blocking []string
	for _, tk := range allTickets {
		if tk.Status == ticket.StatusDone {
			continue
		}
		for _, depID := range tk.Deps {
			if depID == t.ID {
				blocking = append(blocking, tk.ID)
				break
			}
		}
	}
	if len(blocking) > 0 {
		fmt.Print("\n## Blocking\n\n")
		for _, id := range blocking {
			if tk, ok := byID[id]; ok {
				fmt.Printf("- %s [%s] %s\n", id, tk.Status, tk.Title)
			}
		}
	}

	// Children: tickets with this as parent.
	var children []string
	for _, tk := range allTickets {
		if ticket.SameTicketID(tk.Parent, t.ID) {
			children = append(children, tk.ID)
		}
	}
	if len(children) > 0 {
		fmt.Print("\n## Children\n\n")
		for _, id := range children {
			if tk, ok := byID[id]; ok {
				fmt.Printf("- %s [%s] %s\n", id, tk.Status, tk.Title)
			}
		}
	}

	// Links.
	if len(t.Links) > 0 {
		fmt.Print("\n## Linked\n\n")
		for _, id := range t.Links {
			if tk, ok := byID[id]; ok {
				fmt.Printf("- %s [%s] %s\n", id, tk.Status, tk.Title)
			} else {
				fmt.Printf("- %s [unknown]\n", id)
			}
		}
	}

	return nil
}

// localizeTimestamps rewrites the created/updated/completed frontmatter values
// in serialized ticket output from UTC (as written by Serialize) to local
// wall-clock time with no zone offset, for human-facing CLI display. The
// on-disk file is unaffected.
func localizeTimestamps(serialized string, t *ticket.Ticket) string {
	const localLayout = "2006-01-02T15:04:05"
	fields := []struct {
		key string
		ts  time.Time
	}{
		{"created", t.Created},
		{"updated", t.Updated},
		{"completed", t.Completed},
	}
	for _, f := range fields {
		if f.ts.IsZero() {
			continue
		}
		serialized = strings.Replace(serialized,
			f.key+": "+f.ts.UTC().Format(time.RFC3339),
			f.key+": "+f.ts.Local().Format(localLayout),
			1)
	}
	return serialized
}
