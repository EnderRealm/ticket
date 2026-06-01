package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:   "edit <id> [options]",
	Short: "Update ticket fields",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runEdit,
}

func init() {
	f := editCmd.Flags()
	f.String("title", "", "new title")
	f.StringP("description", "d", "", "description text")
	f.String("status", "", "ticket status (backlog, ready, open, done, closed)")
	f.StringP("type", "t", "", "ticket type")
	f.StringP("priority", "p", "", "priority (0-4)")
	f.String("external-ref", "", "external reference")
	f.String("branch", "", "git branch name")
	f.String("parent", "", "parent ticket ID")
	f.String("tags", "", "comma-separated tags")
	f.String("note", "", "append a timestamped note")
	f.StringArray("set", nil, "set extra field (key=value, blank value removes)")

	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
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

	if !changed {
		return fmt.Errorf("no options provided")
	}

	if err := store.Update(t); err != nil {
		return err
	}

	fmt.Printf("Updated %s\n", t.ID)
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
