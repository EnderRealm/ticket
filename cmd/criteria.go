package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var criteriaCmd = &cobra.Command{
	Use:   "criteria <id>",
	Short: "List a ticket's acceptance criteria as tk parses them",
	Long: `List a ticket's acceptance criteria as tk verify parses them: each one's index
(the one tk verify --criterion takes), its kind, its text and the line that
decides it, under the identity of the whole contract. Runs nothing.

The kinds: verify (tk runs its command), test (a person checks it on the real
build) and judged (the run's review and its tests decide it). See
tk verify --help for the lines that declare them.

--json prints the shape the ticket_criteria MCP tool returns.`,
	Example: `  tk criteria 5c4
  tk criteria 5c4 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runCriteria,
}

func init() {
	rootCmd.AddCommand(criteriaCmd)
}

func runCriteria(cmd *cobra.Command, args []string) error {
	t, err := TicketStore().Get(args[0])
	if err != nil {
		return err
	}
	report := ticket.NewCriteriaReport(t.ID, ticket.ParseCriteria(ticket.AcceptanceCriteria(t.Body)))

	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(report.Criteria) == 0 {
		fmt.Printf("%s has no acceptance criteria\n", ticket.SanitizeControl(report.ID))
		return nil
	}
	fmt.Printf("%s acceptance %s\n", ticket.SanitizeControl(report.ID), report.AcceptanceID)
	for _, c := range report.Criteria {
		fmt.Printf("%d. [%s] %s\n", c.Index, c.Kind, c.Text)
		if c.Command != "" {
			fmt.Printf("   verify: %s\n", c.Command)
		}
		if c.Test != "" {
			fmt.Printf("   test: %s\n", c.Test)
		}
		if c.Unverifiable {
			fmt.Println(strings.TrimRight("   unverifiable: "+c.UnverifiableReason, " "))
		}
	}
	return nil
}
