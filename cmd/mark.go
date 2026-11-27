package cmd

import (
	"strings"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var markCmd = &cobra.Command{
	Use:   "mark <id> <done|failed|not-needed> --note <text>",
	Short: "Record a person's word on a generic ticket's step",
	Long: `Record that a person finished a generic ticket's step by hand, or could not.
done makes the ticket done; not-needed closes it; failed leaves the status as it
is, since the step stays with the person. --note is required and is kept on the
mark with who recorded it (the declared source), where (this host's name) and
when; ticket_show returns it as last_mark. Refused on a ticket that is not
generic, since a coding ticket finishes by landing. tk unmark undoes it.`,
	Example: `  tk mark 5c4 done --note "Installed build 412 on the test phone."`,
	Args:    cobra.ExactArgs(2),
	RunE:    runMark,
}

var unmarkCmd = &cobra.Command{
	Use:   "unmark <id>",
	Short: "Undo a generic ticket's last mark",
	Long: `Undo the last tk mark: its record is removed and the ticket returns to the
status it held before it. Refused when no mark stands, or the ticket no longer
holds the status its last mark left it with.`,
	Args: cobra.ExactArgs(1),
	RunE: runUnmark,
}

func init() {
	registerTestFlags(markCmd)
	registerTestFlags(unmarkCmd)
	rootCmd.AddCommand(markCmd, unmarkCmd)
}

func runMark(cmd *cobra.Command, args []string) error {
	result := ticket.MarkResult(strings.ReplaceAll(args[1], "-", "_"))
	return applyLanding(cmd, "", ticket.ActionRequest{ID: args[0], Transition: ticket.TransitionMark, Mark: result})
}

func runUnmark(cmd *cobra.Command, args []string) error {
	return applyLanding(cmd, "Last mark undone.", ticket.ActionRequest{ID: args[0], Transition: ticket.TransitionUnmark})
}
