package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var claimCmd = &cobra.Command{
	Use:   "claim <id> --owner <label> [--ttl 2h] [--takeover]",
	Short: "Claim a ticket for a bounded time",
	Long: `Claim a ticket: a lease saying --owner is working it now, on this machine,
until --ttl from now (default 2h). A claim is live while now is before its
expiry, so one whose owner died stops counting by itself.

The owner's own live claim is renewed. Another owner's live claim refuses the
claim with "claim conflict:", naming that owner, its machine and its expiry, and
leaves the ticket unchanged; --takeover replaces it. An expired claim is
replaced, and the claim it replaced is reported.

Advisory: nothing else is refused on a claimed ticket. tk frontier,
ticket_ready and ticket_frontier leave out a ticket under a live claim, and
moving a ticket to done or closed clears it. tk release gives a claim up.`,
	Example: `  tk claim 5c4 --owner work-session-41
  tk claim 5c4 --owner work-session-41 --ttl 30m --takeover`,
	Args: cobra.ExactArgs(1),
	RunE: runClaim,
}

var releaseCmd = &cobra.Command{
	Use:   "release <id> --owner <label>",
	Short: "Release an owner's claim on a ticket",
	Long: `Release --owner's claim on a ticket, live or expired. Another owner's live
claim refuses the release with "claim conflict:" and stays. No claim, or another
owner's expired one, is nothing to release: the ticket is left unwritten.`,
	Args: cobra.ExactArgs(1),
	RunE: runRelease,
}

func init() {
	registerClaimFlags(claimCmd)
	registerReleaseFlags(releaseCmd)
	rootCmd.AddCommand(claimCmd, releaseCmd)
}

// registerClaimFlags declares the flags runClaim reads. Shared with tests so a
// test flag set can't drift from the real command.
func registerClaimFlags(cmd *cobra.Command) {
	registerReleaseFlags(cmd)
	cmd.Flags().Duration("ttl", ticket.DefaultClaimTTL, "how long the claim lasts from now (at least 1s)")
	cmd.Flags().Bool("takeover", false, "replace a live claim another owner holds")
}

func registerReleaseFlags(cmd *cobra.Command) {
	cmd.Flags().String("owner", "", "label of whoever holds the claim (required)")
}

// claimRefusal returns a claim conflict as itself, so the message opens with
// its stable prefix whatever wrapped it.
func claimRefusal(err error) error {
	var conflict *ticket.ClaimConflictError
	if errors.As(err, &conflict) {
		return conflict
	}
	return err
}

func runClaim(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	owner, _ := cmd.Flags().GetString("owner")
	ttl, _ := cmd.Flags().GetDuration("ttl")
	takeover, _ := cmd.Flags().GetBool("takeover")

	res, err := ticket.ClaimTicket(TicketStore(), args[0], owner, ttl, takeover)
	if err != nil {
		return claimRefusal(err)
	}

	if jsonOutput {
		data, err := json.MarshalIndent(struct {
			ID       string        `json:"id"`
			Claim    ticket.Claim  `json:"claim"`
			Replaced *ticket.Claim `json:"replaced,omitempty"`
		}{res.Ticket.ID, res.Claim, res.Replaced}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("Claimed %s for %s on %s until %s\n", res.Ticket.ID, res.Claim.Owner, res.Claim.Machine, res.Claim.Expires.Format(time.RFC3339))
	if r := res.Replaced; r != nil {
		fmt.Printf("Replaced the claim of %s on %s, which expires %s\n", r.Owner, r.Machine, r.Expires.Format(time.RFC3339))
	}
	return nil
}

func runRelease(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	owner, _ := cmd.Flags().GetString("owner")

	res, err := ticket.ReleaseTicket(TicketStore(), args[0], owner)
	if err != nil {
		return claimRefusal(err)
	}

	if jsonOutput {
		data, err := json.MarshalIndent(struct {
			ID       string        `json:"id"`
			Released *ticket.Claim `json:"released,omitempty"`
		}{res.Ticket.ID, res.Released}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	if res.Released == nil {
		fmt.Printf("%s holds no claim of %s's to release\n", res.Ticket.ID, owner)
		return nil
	}
	fmt.Printf("Released %s's claim on %s\n", owner, res.Ticket.ID)
	return nil
}
