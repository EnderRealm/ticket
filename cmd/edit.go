package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:   "edit <id> [options]",
	Short: "Update ticket fields",
	Long: `Update ticket fields. Only the fields named by a flag change.

--description, --design, --acceptance and --test-results each replace one body
section: the description, ## Design, ## Acceptance Criteria and ## Test Results.
An edit writing the description or acceptance reports on stderr an acceptance
section no criterion parses from, or a verify: line carrying shell syntax (see
tk verify --help); the edit is still written.

--parent names an epic in the same project, and an epic itself cannot have a
parent. A qualified project/id names an epic in another namespace, accepted
once the catalog requires cross-project-parents.

An epic's status is derived from its children, and setting one by hand is
refused. The exception is --status closed, which abandons the epic: it records
abandoned: true and closes its unfinished children, naming them in the line
that reports the edit. Setting any other status on an abandoned epic takes the
abandon back. The same applies to a status set alongside --type epic. The
README's Epic Status section has the full rules.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runEdit,
}

func init() {
	registerEditFlags(editCmd)
	rootCmd.AddCommand(editCmd)
}

// registerEditFlags declares the flags runEdit reads. Shared with tests so a
// test flag set can't drift from the real command.
func registerEditFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("title", "", "new title")
	f.StringP("description", "d", "", "description text")
	f.String("design", "", "design section text")
	f.String("acceptance", "", "acceptance criteria section text")
	f.String("test-results", "", "test results section text")
	f.String("status", "", "ticket status (backlog, ready, open, done, closed)")
	f.StringP("type", "t", "", "ticket type")
	f.StringP("priority", "p", "", "priority (0-4)")
	f.String("external-ref", "", "external reference")
	f.String("branch", "", "git branch name")
	f.String("parent", "", "parent epic ID (an epic in the same project)")
	f.String("tags", "", "comma-separated tags")
	f.String("note", "", "append a timestamped note")
	f.StringArray("set", nil, "set extra field (key=value, blank value removes)")
	f.StringArray("output", nil, "set output value, e.g. branch, commit, artifact (key=value, blank value removes)")
}

func runEdit(cmd *cobra.Command, args []string) error {
	store := TicketStore()
	id := args[0]

	t, err := store.Get(id)
	if err != nil {
		return err
	}

	changed := false

	if v, _ := cmd.Flags().GetString("title"); cmd.Flags().Changed("title") {
		t.Title = v
		changed = true
	}
	if v, _ := cmd.Flags().GetString("status"); cmd.Flags().Changed("status") {
		if err := ticket.ValidateStatus(ticket.Status(v)); err != nil {
			return err
		}
		t.Status = ticket.Status(v)
		changed = true
	}
	if v, _ := cmd.Flags().GetString("type"); cmd.Flags().Changed("type") {
		if err := ticket.ValidateType(ticket.TicketType(v)); err != nil {
			return err
		}
		t.Type = ticket.TicketType(v)
		changed = true
	}
	if v, _ := cmd.Flags().GetString("priority"); cmd.Flags().Changed("priority") {
		var p int
		if _, err := fmt.Sscanf(v, "%d", &p); err != nil {
			return fmt.Errorf("invalid priority %q", v)
		}
		if err := ticket.ValidatePriority(p); err != nil {
			return err
		}
		t.Priority = p
		changed = true
	}
	if v, _ := cmd.Flags().GetString("external-ref"); cmd.Flags().Changed("external-ref") {
		t.ExternalRef = v
		changed = true
	}
	if v, _ := cmd.Flags().GetString("branch"); cmd.Flags().Changed("branch") {
		t.Branch = v
		changed = true
	}
	if v, _ := cmd.Flags().GetString("parent"); cmd.Flags().Changed("parent") {
		t.Parent = v
		changed = true
	}
	if v, _ := cmd.Flags().GetString("tags"); cmd.Flags().Changed("tags") {
		var tags []string
		for _, tag := range strings.Split(v, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		t.Tags = tags
		changed = true
	}

	if v, _ := cmd.Flags().GetString("description"); cmd.Flags().Changed("description") {
		t.Body = ticket.UpdateSection(t.Body, "", v)
		changed = true
	}
	if v, _ := cmd.Flags().GetString("design"); cmd.Flags().Changed("design") {
		t.Body = ticket.UpdateSection(t.Body, "Design", v)
		changed = true
	}
	if v, _ := cmd.Flags().GetString("acceptance"); cmd.Flags().Changed("acceptance") {
		t.Body = ticket.UpdateSection(t.Body, "Acceptance Criteria", v)
		changed = true
	}
	if v, _ := cmd.Flags().GetString("test-results"); cmd.Flags().Changed("test-results") {
		t.Body = ticket.UpdateSection(t.Body, "Test Results", v)
		changed = true
	}
	if v, _ := cmd.Flags().GetString("note"); cmd.Flags().Changed("note") {
		t.Notes = append(t.Notes, ticket.Note{
			Timestamp: time.Now().UTC(),
			Text:      v,
		})
		changed = true
	}

	if sets, _ := cmd.Flags().GetStringArray("set"); cmd.Flags().Changed("set") {
		if t.Extra == nil {
			t.Extra = map[string]string{}
		}
		for _, s := range sets {
			key, value, err := parseSetFlag(s)
			if err != nil {
				return err
			}
			if value == "" {
				delete(t.Extra, key)
			} else {
				t.Extra[key] = value
			}
		}
		changed = true
	}

	if outputs, _ := cmd.Flags().GetStringArray("output"); cmd.Flags().Changed("output") {
		if t.Outputs == nil {
			t.Outputs = map[string]string{}
		}
		for _, o := range outputs {
			key, value, err := parseOutputFlag(o)
			if err != nil {
				return err
			}
			if value == "" {
				delete(t.Outputs, key)
			} else {
				t.Outputs[key] = value
			}
		}
		changed = true
	}

	if !changed {
		return fmt.Errorf("no options provided")
	}

	closed, err := ticket.SaveEdit(store, t, cmd.Flags().Changed("status"))
	if err != nil {
		return err
	}

	fmt.Printf("Updated %s%s\n", t.ID, ticket.ClosedChildrenNote(closed))

	// The same warnings ticket_edit returns, under the same condition: only an
	// edit that wrote a section able to carry the criteria repeats them.
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("acceptance") {
		if ticket.UnparsedAcceptance(t.Body) {
			fmt.Fprintln(os.Stderr, ticket.UnparsedAcceptanceWarning(t.ID))
		}
		if shell := ticket.ShellSyntaxCriteria(t.Body); len(shell) > 0 {
			fmt.Fprintln(os.Stderr, ticket.ShellSyntaxWarning(t.ID, shell))
		}
	}
	return nil
}

func parseSetFlag(s string) (key, value string, err error) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "", "", fmt.Errorf("--set requires key=value format: %q", s)
	}
	key = s[:idx]
	value = s[idx+1:]
	if err := ticket.ValidateExtraKey(key); err != nil {
		return "", "", err
	}
	if value != "" {
		if err := ticket.ValidateExtraValue(value); err != nil {
			return "", "", err
		}
	}
	return key, value, nil
}

func parseOutputFlag(s string) (key, value string, err error) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "", "", fmt.Errorf("--output requires key=value format: %q", s)
	}
	key = s[:idx]
	value = s[idx+1:]
	if err := ticket.ValidateOutputKey(key); err != nil {
		return "", "", err
	}
	if value != "" {
		if err := ticket.ValidateOutputValue(value); err != nil {
			return "", "", err
		}
	}
	return key, value, nil
}
