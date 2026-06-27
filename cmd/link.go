package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link <id> <id> [id...]",
	Short: "Create symmetric links between tickets",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runLink,
}

var unlinkCmd = &cobra.Command{
	Use:   "unlink <id> <target-id>",
	Short: "Remove link between two tickets",
	Args:  cobra.ExactArgs(2),
	RunE:  runUnlink,
}

func init() {
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(unlinkCmd)
}

func runLink(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())

	// Resolve all tickets first.
	var tickets []*ticket.Ticket
	for _, id := range args {
		t, err := store.Get(id)
		if err != nil {
			return err
		}
		tickets = append(tickets, t)
	}

	// Link every pair.
	for i := 0; i < len(tickets); i++ {
		for j := i + 1; j < len(tickets); j++ {
			ticket.AddLink(tickets[i], tickets[j])
		}
	}

	// Write all back.
	for _, t := range tickets {
		if err := store.Update(t); err != nil {
			return err
		}
	}

	fmt.Printf("Linked %d tickets\n", len(tickets))
	return nil
}

func runUnlink(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())

	a, err := store.Get(args[0])
	if err != nil {
		return err
	}
	b, err := store.Get(args[1])
	if err != nil {
		return err
	}

	ticket.RemoveLink(a, b)

	if err := store.Update(a); err != nil {
		return err
	}
	if err := store.Update(b); err != nil {
		return err
	}

	fmt.Printf("Unlinked %s and %s\n", a.ID, b.ID)
	return nil
}
