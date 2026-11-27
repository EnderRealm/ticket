package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new ticket",
	Long: `Create a ticket. Outside a directory the config registers as a project, the
destination must be named: --project <namespace> (--project _root for an idea
with no repository yet) or --repo. The guard applies only without --project or
--repo: a --repo path still resolves by git remote or directory name, so a path
no project registers writes into whichever project those name.

An acceptance criterion carries at most one of a verify: and a test: line, and
one with neither is judged by the run (see tk verify --help). One carrying both
is reported on stderr when the ticket is written; the ticket is still created.

--workflow-kind generic marks work no run can do: a step a person takes, or an
event to observe. tk frontier and ready work leave a generic ticket out; as a
dependency it still holds its dependents until it is testing, done or closed.
Absent, or coding, is a run's work. A generic ticket carries its steps in --how
and its shell commands in --command, each repeatable in order, one line per
value and kept exactly as written (commas included). tk never runs them.
--machine names the machine those commands are for, so they are not run on
the wrong one; absent, they are for whichever machine they are read on. It is
a label: tk runs nothing on it.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCreate,
}

func init() {
	f := createCmd.Flags()
	f.StringP("description", "d", "", "ticket description")
	f.StringP("type", "t", "feature", "ticket type (feature, bug, epic)")
	f.StringP("priority", "p", "2", "priority (0-4, 0 highest)")
	f.String("external-ref", "", "external reference")
	f.String("parent", "", "parent epic ID (an epic in the same project; a qualified project/id names an epic in another namespace, accepted once the catalog requires cross-project-parents)")
	f.String("tags", "", "comma-separated tags")
	f.StringArray("set", nil, "set extra field (key=value)")
	f.String("workflow-kind", "", "how the ticket is worked: coding (the default) or generic (work a person does or an event to observe; left out of ready work)")
	f.StringArray("how", nil, "a step for a generic ticket, one line; repeat in order")
	f.StringArray("command", nil, "a shell command for a generic ticket, one line, kept exactly as written; repeat in order")
	f.String("machine", "", "the machine a generic ticket's commands are for, one line; a label, tk runs nothing on it")

	rootCmd.AddCommand(createCmd)
}

// createTicket is the store write runCreate goes through. Create can settle
// t.ID on a different ID than the one it was handed — a hash collision with a
// concurrent create regenerates it — and that path is only reachable through a
// race, so tests replace this to take it deterministically.
var createTicket = (*ticket.FileStore).Create

func runCreate(cmd *cobra.Command, args []string) error {
	// A create names its destination. Reads fall back from the working
	// directory to a git remote or a directory name, and warn; a write landing
	// somewhere inferred is a ticket in a namespace nobody chose, so only a
	// directory the config registers, or a selector, decides where it goes.
	// The guard covers only a bare create: a --repo path is resolved by
	// ticket.ResolveStoreForRepo, which keeps the remote and basename fallbacks
	// on purpose, so it can still land in a project inferred from that path.
	if projectFlag == "" && repoFlag == "" {
		cfg, err := project.Load()
		if err != nil {
			return fmt.Errorf("load ticket config: %w", err)
		}
		if _, source := project.ResolveName(cfg, mustGetwd(), ""); source != "config" {
			return fmt.Errorf("not inside a registered project: pass --project <namespace> to choose the destination (--project %s for an idea with no repository yet), or run tk init here", project.RootNamespace)
		}
	}
	store := TicketStore()

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

	description, _ := cmd.Flags().GetString("description")
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

	var body strings.Builder
	if description != "" {
		body.WriteString("\n" + description + "\n")
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

	workflowKind, _ := cmd.Flags().GetString("workflow-kind")
	machine, _ := cmd.Flags().GetString("machine")

	t := &ticket.Ticket{
		ID:           ticket.GenerateID(title),
		Status:       ticket.StatusBacklog,
		Type:         ticket.TicketType(typeStr),
		Priority:     priority,
		Parent:       parent,
		Deps:         []string{},
		Links:        []string{},
		Tags:         tags,
		ExternalRef:  externalRef,
		WorkflowKind: ticket.WorkflowKind(workflowKind),
		How:          lineFlag(cmd, "how"),
		Commands:     lineFlag(cmd, "command"),
		Machine:      machine,
		Created:      time.Now().UTC(),
		Title:        title,
		Body:         body.String(),
		Extra:        extra,
	}

	// Looked up before the write so the new ticket is not its own match.
	existing, _ := store.List()
	similar := ticket.SimilarTickets(existing, t)

	if err := createTicket(store, t); err != nil {
		return err
	}

	// The description is written raw into the body, so a `## Acceptance
	// Criteria` section in it is how this path produces criteria. Warned about
	// here, in the same sentence ticket_create returns, because the caller
	// still holds the context they came from.
	if ticket.UnparsedAcceptance(t.Body) {
		fmt.Fprintln(os.Stderr, ticket.UnparsedAcceptanceWarning(t.ID))
	}
	if shell := ticket.ShellSyntaxCriteria(t.Body); len(shell) > 0 {
		fmt.Fprintln(os.Stderr, ticket.ShellSyntaxWarning(t.ID, shell))
	}
	if both := ticket.TwoMarkerCriteria(t.Body); len(both) > 0 {
		fmt.Fprintln(os.Stderr, ticket.TwoMarkersWarning(t.ID, both))
	}

	if w := ticket.SimilarTicketsWarning(t.ID, similar); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}

	return showTicket(store, t.ID, false)
}
