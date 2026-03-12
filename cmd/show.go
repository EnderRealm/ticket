package cmd

import (
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
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
	rootCmd.AddCommand(showCmd)
}

func runShow(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	for i, id := range args {
		if i > 0 {
			fmt.Println()
		}
		if err := showTicket(store, id); err != nil {
			return err
		}
	}
	return nil
}

func showTicket(store *ticket.FileStore, id string) error {
	t, err := store.Get(id)
	if err != nil {
		return err
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

	// Annotate parent line with title.
	output := string(data)
	if t.Parent != "" {
		if parent, ok := byID[t.Parent]; ok {
			output = strings.Replace(output,
				"parent: "+t.Parent,
				"parent: "+t.Parent+"  # "+parent.Title,
				1)
		}
	}

	fmt.Print(output)

	// Blockers: deps not at done stage.
	var blockers []string
	for _, depID := range t.Deps {
		dep, ok := byID[depID]
		if !ok || dep.Stage != ticket.StageDone {
			blockers = append(blockers, depID)
		}
	}
	if len(blockers) > 0 {
		fmt.Print("\n## Blockers\n\n")
		for _, id := range blockers {
			if dep, ok := byID[id]; ok {
				fmt.Printf("- %s [%s] %s\n", id, dep.Stage, dep.Title)
			} else {
				fmt.Printf("- %s [unknown]\n", id)
			}
		}
	}

	// Blocking: tickets that depend on this one and aren't done.
	var blocking []string
	for _, tk := range allTickets {
		if tk.Stage == ticket.StageDone {
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
				fmt.Printf("- %s [%s] %s\n", id, tk.Stage, tk.Title)
			}
		}
	}

	// Children: tickets with this as parent.
	var children []string
	for _, tk := range allTickets {
		if tk.Parent == t.ID {
			children = append(children, tk.ID)
		}
	}
	if len(children) > 0 {
		fmt.Print("\n## Children\n\n")
		for _, id := range children {
			if tk, ok := byID[id]; ok {
				fmt.Printf("- %s [%s] %s\n", id, tk.Stage, tk.Title)
			}
		}
	}

	// Links.
	if len(t.Links) > 0 {
		fmt.Print("\n## Linked\n\n")
		for _, id := range t.Links {
			if tk, ok := byID[id]; ok {
				fmt.Printf("- %s [%s] %s\n", id, tk.Stage, tk.Title)
			} else {
				fmt.Printf("- %s [unknown]\n", id)
			}
		}
	}

	return nil
}
