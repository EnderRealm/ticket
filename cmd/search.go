package cmd

import (
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search tickets by relevance (best matches first)",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	results := ticket.Search(tickets, strings.Join(args, " "))
	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no matches")
		return nil
	}

	// Preserve relevance order: results are already sorted best-first.
	ranked := make([]*ticket.Ticket, len(results))
	for i, r := range results {
		ranked[i] = r.Ticket
	}

	// Render rows manually so a context snippet can be interleaved under each.
	terms := ticket.Tokenize(strings.Join(args, " "))
	tw := newTableWriter()
	tw.computeWidths(ranked)
	tw.printHeader()
	for _, r := range results {
		tw.printRow(r.Ticket)
		if r.Snippet != "" {
			label := colorize(r.Field+":", ansiGray)
			fmt.Printf("    %s %s\n", label, highlightTerms(r.Snippet, terms))
		}
	}
	return nil
}
