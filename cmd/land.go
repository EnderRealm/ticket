package cmd

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var landCmd = &cobra.Command{
	Use:   "land <id> --commit <sha>",
	Short: "Record the commit a ticket landed as",
	Long: `Record the commit a ticket landed as. The ticket becomes testing when it has at
least one criterion with a test: line (see tk criteria), otherwise done; the
commit watcher's auto-close decides the same way. outputs.commit is set to
--commit, the full hash, in place of any earlier one, so a ticket that failed
its test and lands again names the new commit; outputs.branch keeps the value
already recorded. Refused on an epic.

Landing a commit that was already tested runs the decision again, so it returns
a passed or failed ticket to testing. Retry one landing under the --action-id it
was first sent with, which returns the recorded outcome instead.`,
	Example: `  tk land 5c4 --commit "$(git rev-parse HEAD)"`,
	Args:    cobra.ExactArgs(1),
	RunE:    runLand,
}

var passCmd = &cobra.Command{
	Use:   "pass <id>",
	Short: "Pass a testing ticket",
	Long: `Record that a testing ticket passed its test: it becomes done, closed now.
Refused on a ticket that is not in testing. tk retest undoes it.`,
	Args: cobra.ExactArgs(1),
	RunE: runPass,
}

var failCmd = &cobra.Command{
	Use:   "fail <id> --check <n>... --note <text>",
	Short: "Fail a testing ticket",
	Long: `Record that a testing ticket failed its test: it becomes ready, for a new run,
which reads what the person saw from --note. --check names a failed criterion by
its 1-based index among the ticket's test: criteria (the [test] rows of
tk criteria, counted from the top); repeat it for each. Both are required.
ticket_show returns the failed criteria, the note and the time as last_test.
Refused on a ticket that is not in testing. tk retest undoes it.`,
	Example: `  tk fail 5c4 --check 2 --note "A long answer runs past the card's edge."`,
	Args:    cobra.ExactArgs(1),
	RunE:    runFail,
}

var retestCmd = &cobra.Command{
	Use:   "retest <id>",
	Short: "Undo a ticket's last pass or fail",
	Long: `Undo the last tk pass or tk fail: its record is removed and the ticket returns
to testing. Refused when no test stands, or the ticket no longer holds the
status and the outputs.commit its last test left it with.`,
	Args: cobra.ExactArgs(1),
	RunE: runRetest,
}

func init() {
	registerLandFlags(landCmd)
	registerTestFlags(passCmd)
	registerFailFlags(failCmd)
	registerTestFlags(retestCmd)
	rootCmd.AddCommand(landCmd, passCmd, failCmd, retestCmd)
}

// registerTestFlags declares the flags the four commands share. The flag sets
// are shared with tests so a test's cannot drift from the real command's.
func registerTestFlags(cmd *cobra.Command) {
	cmd.Flags().String("note", "", "note to append")
	cmd.Flags().String("action-id", "", "stable identity for this action, so a retry returns the recorded outcome instead of applying it twice (default: generated)")
}

func registerLandFlags(cmd *cobra.Command) {
	registerTestFlags(cmd)
	cmd.Flags().String("commit", "", "full hash of the commit the ticket landed as (required)")
}

func registerFailFlags(cmd *cobra.Command) {
	registerTestFlags(cmd)
	cmd.Flags().IntSlice("check", nil, "a failed criterion, by its index among the ticket's test: criteria; repeat it for each (required)")
}

func runLand(cmd *cobra.Command, args []string) error {
	commit, _ := cmd.Flags().GetString("commit")
	return applyLanding(cmd, "Landed as commit "+commit+".", ticket.ActionRequest{ID: args[0], Transition: ticket.TransitionLand, Commit: commit})
}

func runPass(cmd *cobra.Command, args []string) error {
	return applyLanding(cmd, "Passed its test.", ticket.ActionRequest{ID: args[0], Transition: ticket.TransitionPass})
}

func runFail(cmd *cobra.Command, args []string) error {
	checks, _ := cmd.Flags().GetIntSlice("check")
	return applyLanding(cmd, "", ticket.ActionRequest{ID: args[0], Transition: ticket.TransitionFail, Checks: checks})
}

func runRetest(cmd *cobra.Command, args []string) error {
	return applyLanding(cmd, "Last test undone; back to testing.", ticket.ActionRequest{ID: args[0], Transition: ticket.TransitionRetest})
}

// landingJSON is the --json report of the four commands. Status is what the
// ticket holds now, which on a replay may have moved on from the receipt's
// outcome.
type landingJSON struct {
	ID       string               `json:"id"`
	Status   string               `json:"status"`
	LastTest *ticket.TestRow      `json:"last_test,omitempty"`
	Receipt  ticket.ActionReceipt `json:"receipt"`
	Replayed bool                 `json:"replayed"`
}

// applyLanding applies req through the action protocol under the note and
// action ID the flags give, and reports the status the ticket holds after it.
func applyLanding(cmd *cobra.Command, defaultNote string, req ticket.ActionRequest) error {
	// Everything below fails on the state of the store or on a value ApplyAction
	// judges and names; the usage dump would bury it.
	cmd.SilenceUsage = true

	req.Note = defaultNote
	if cmd.Flags().Changed("note") {
		req.Note, _ = cmd.Flags().GetString("note")
	}
	req.ActionID = "cli-" + rand.Text()
	if cmd.Flags().Changed("action-id") {
		req.ActionID, _ = cmd.Flags().GetString("action-id")
	}

	res, err := ticket.ApplyAction(TicketStore(), req)
	if err != nil {
		return err
	}

	if jsonOutput {
		data, err := json.MarshalIndent(landingJSON{
			ID:       res.Ticket.ID,
			Status:   string(res.Ticket.Status),
			LastTest: ticket.LastTest(res.Ticket),
			Receipt:  res.Receipt,
			Replayed: res.Replayed,
		}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	if res.Replayed {
		fmt.Printf("%s is %s (already recorded as %s)\n", res.Ticket.ID, res.Ticket.Status, res.Receipt.Outcome)
		return nil
	}
	fmt.Printf("%s is %s\n", res.Ticket.ID, res.Ticket.Status)
	return nil
}
