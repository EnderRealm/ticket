package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach <id> --role before|after --attachment-id <id> --link <url>",
	Short: "Attach a before or after picture by its link",
	Long: `Attach a picture hosted elsewhere to a ticket: its id and its https link, under
the role before (the picture the ticket started from) or after (the picture the
run captured). A ticket holds one attachment per role, and attaching another
replaces it. The link must be https, and the id is letters, digits, '.', '_'
and '-', starting with a letter or digit. tk detach removes one.`,
	Example: `  tk attach 5c4 --role after --attachment-id 9f2c --link https://folio.example/api/images/9f2c`,
	Args:    cobra.ExactArgs(1),
	RunE:    runAttach,
}

var detachCmd = &cobra.Command{
	Use:   "detach <id> --role before|after",
	Short: "Remove a ticket's before or after attachment",
	Long:  "Remove the attachment with that role. Removing one the ticket does not hold is not an error.",
	Args:  cobra.ExactArgs(1),
	RunE:  runDetach,
}

func init() {
	registerAttachFlags(attachCmd)
	registerDetachFlags(detachCmd)
	rootCmd.AddCommand(attachCmd, detachCmd)
}

// registerAttachFlags declares the flags runAttach reads. Shared with tests so
// a test flag set can't drift from the real command.
func registerAttachFlags(cmd *cobra.Command) {
	registerDetachFlags(cmd)
	cmd.Flags().String("attachment-id", "", "id the attachment's host gave it (required)")
	cmd.Flags().String("link", "", "https link to it (required)")
}

func registerDetachFlags(cmd *cobra.Command) {
	cmd.Flags().String("role", "", "before or after (required)")
}

func runAttach(cmd *cobra.Command, args []string) error {
	// Everything below fails on the state of the store or on a value the
	// validation names; the usage dump would bury it.
	cmd.SilenceUsage = true

	role, _ := cmd.Flags().GetString("role")
	attachmentID, _ := cmd.Flags().GetString("attachment-id")
	link, _ := cmd.Flags().GetString("link")
	a := ticket.Attachment{ID: attachmentID, Role: ticket.AttachmentRole(role), Link: link}

	t, err := ticket.Mutate(TicketStore(), args[0], func(t *ticket.Ticket) error {
		return ticket.SetAttachment(t, a)
	})
	if err != nil {
		return err
	}

	fmt.Printf("Attached %s as %s of %s\n", a.ID, a.Role, t.ID)
	return nil
}

func runDetach(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	role, _ := cmd.Flags().GetString("role")

	t, err := ticket.Mutate(TicketStore(), args[0], func(t *ticket.Ticket) error {
		return ticket.RemoveAttachment(t, ticket.AttachmentRole(role))
	})
	if err != nil {
		return err
	}

	fmt.Printf("Removed %s attachment of %s\n", role, t.ID)
	return nil
}
