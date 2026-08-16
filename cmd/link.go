package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
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
	store := TicketStore()

	// Resolve all tickets first.
	var tickets []*ticket.Ticket
	for _, id := range args {
		t, err := store.Get(id)
		if err != nil {
			return err
		}
		tickets = append(tickets, t)
	}

	// One Mutate per ticket, each holding its own lock only: the links a ticket
	// gains are appended to the ones it already holds, so a plain
	// read-modify-write would drop a link a concurrent writer added. Taking
	// every ticket's lock at once to make the set atomic would deadlock two
	// runs naming the same pair in opposite orders, and the write was not
	// atomic across the set before this either.
	for i := range tickets {
		if _, err := ticket.Mutate(store, tickets[i].ID, func(t *ticket.Ticket) error {
			for j := range tickets {
				if j != i {
					// AddLink is symmetric, but the far side here is the copy read
					// above and is never written — that side is added by its own
					// pass, against its own current file.
					ticket.AddLink(t, tickets[j])
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	fmt.Printf("Linked %d tickets\n", len(tickets))
	return nil
}

func runUnlink(cmd *cobra.Command, args []string) error {
	store := TicketStore()

	a, err := store.Get(args[0])
	if err != nil {
		return err
	}
	b, err := store.Get(args[1])
	if err != nil {
		return err
	}

	// One side at a time, each under its own lock — see runLink.
	if _, err := ticket.Mutate(store, a.ID, func(t *ticket.Ticket) error {
		ticket.RemoveLink(t, b)
		return nil
	}); err != nil {
		return err
	}
	if _, err := ticket.Mutate(store, b.ID, func(t *ticket.Ticket) error {
		ticket.RemoveLink(a, t)
		return nil
	}); err != nil {
		return err
	}

	fmt.Printf("Unlinked %s and %s\n", a.ID, b.ID)
	return nil
}
