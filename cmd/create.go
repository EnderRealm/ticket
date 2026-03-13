package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new ticket",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCreate,
}

func init() {
	f := createCmd.Flags()
	f.StringP("description", "d", "", "ticket description")
	f.String("design", "", "design notes")
	f.String("acceptance", "", "acceptance criteria")
	f.StringP("type", "t", "task", "ticket type (task, feature, bug, epic, chore)")
	f.StringP("priority", "p", "2", "priority (0-4)")
	f.StringP("assignee", "a", "", "assignee name")
	f.String("external-ref", "", "external reference")
	f.String("parent", "", "parent ticket ID")
	f.String("tags", "", "comma-separated tags")
	f.StringArray("set", nil, "set extra field (key=value)")

	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())

	var title string
	if len(args) > 0 {
		title = args[0]
	}
	if title == "" {
		return fmt.Errorf("title is required")
	}

	typeStr, _ := cmd.Flags().GetString("type")
	if err := ticket.ValidateType(ticket.TicketType(typeStr)); err != nil {
		return err
	}

	priorityStr, _ := cmd.Flags().GetString("priority")
	priority := 2
	if _, err := fmt.Sscanf(priorityStr, "%d", &priority); err != nil {
		return fmt.Errorf("invalid priority %q", priorityStr)
	}
	if err := ticket.ValidatePriority(priority); err != nil {
		return err
	}

	assignee, _ := cmd.Flags().GetString("assignee")
	if assignee == "" {
		assignee = gitUserName()
	}

	description, _ := cmd.Flags().GetString("description")
	design, _ := cmd.Flags().GetString("design")
	acceptance, _ := cmd.Flags().GetString("acceptance")
	externalRef, _ := cmd.Flags().GetString("external-ref")
	parent, _ := cmd.Flags().GetString("parent")
	tagsStr, _ := cmd.Flags().GetString("tags")

	var tags []string
	if tagsStr != "" {
		for _, t := range strings.Split(tagsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	id := ticket.GenerateID(title)

	var body strings.Builder
	if description != "" {
		body.WriteString("\n" + description + "\n")
	}
	if design != "" {
		body.WriteString("\n## Design\n\n" + design + "\n")
	}
	if acceptance != "" {
		body.WriteString("\n## Acceptance Criteria\n\n" + acceptance + "\n")
	}
	if body.Len() == 0 {
		body.WriteString("\n")
	}

	extra := map[string]string{}
	if sets, _ := cmd.Flags().GetStringArray("set"); cmd.Flags().Changed("set") {
		for _, s := range sets {
			key, value, err := parseSetFlag(s)
			if err != nil {
				return err
			}
			if value == "" {
				delete(extra, key)
			} else {
				extra[key] = value
			}
		}
	}

	t := &ticket.Ticket{
		ID:          id,
		Stage:       ticket.StageTriage,
		Type:        ticket.TicketType(typeStr),
		Priority:    priority,
		Assignee:    assignee,
		Parent:      parent,
		Deps:        []string{},
		Links:       []string{},
		Tags:        tags,
		ExternalRef: externalRef,
		Created:     time.Now().UTC(),
		Title:       title,
		Body:        body.String(),
		Extra:       extra,
	}

	if err := store.Create(t); err != nil {
		return err
	}

	return showTicket(store, id)
}

func gitUserName() string {
	// Quick check for git user.name, ignore errors.
	out, err := execCommand("git", "config", "user.name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
