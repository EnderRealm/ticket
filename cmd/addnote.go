package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var addNoteCmd = &cobra.Command{
	Use:   "add-note <id> [text]",
	Short: "Append timestamped note to a ticket",
	Long:  "Append a timestamped note. If no text is given, reads from stdin.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runAddNote,
}

func init() {
	rootCmd.AddCommand(addNoteCmd)
}

func runAddNote(cmd *cobra.Command, args []string) error {
	store := TicketStore()
	id := args[0]

	// A fast-fail before stdin is read, not the read the write is computed
	// from: Mutate re-reads under the lock, so this result is deliberately
	// discarded. Without it a bad ID is only discovered after the note text has
	// been consumed — which drains a pipe the caller cannot re-send, and
	// reports "no note provided" for what is actually a wrong ID.
	if _, err := store.Get(id); err != nil {
		return err
	}

	var noteText string
	if len(args) > 1 {
		noteText = strings.Join(args[1:], " ")
	} else {
		// Read from stdin if not a terminal.
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			scanner := bufio.NewScanner(os.Stdin)
			var lines []string
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
			}
			noteText = strings.Join(lines, "\n")
		} else {
			return fmt.Errorf("no note provided")
		}
	}

	if noteText == "" {
		return fmt.Errorf("no note provided")
	}

	// Through Mutate, not Get plus Update: the note is appended to whatever the
	// ticket already holds, so a writer that read before another one wrote would
	// drop that writer's note.
	t, err := ticket.Mutate(store, id, func(t *ticket.Ticket) error {
		t.Notes = append(t.Notes, ticket.Note{
			Timestamp: time.Now().UTC(),
			Text:      noteText,
		})

		// Rebuild body with notes section to ensure it's written.
		// The serializer handles notes from the Notes field.
		// Strip existing notes section from body to avoid duplication.
		if idx := strings.Index(t.Body, "\n## Notes\n"); idx >= 0 {
			t.Body = t.Body[:idx+1]
		} else if strings.HasPrefix(t.Body, "## Notes\n") {
			t.Body = "\n"
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("Note added to %s\n", t.ID)
	return nil
}
