// Package mcp provides an MCP server for AI agent access to tickets.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server with all ticket management tools registered.
func NewServer(ticketsDir string) *mcp.Server {
	store := ticket.NewFileStore(ticketsDir)

	server := mcp.NewServer(
		&mcp.Implementation{Name: "tk", Version: "0.1.0"},
		nil,
	)

	registerList(server, store)
	registerShow(server, store)
	registerCreate(server, store)
	registerEdit(server, store)
	registerAddNote(server, store)
	registerDep(server, store)
	registerLink(server, store)
	registerReady(server, store)
	registerBlocked(server, store)
	registerWorkflow(server)
	registerAdvance(server, store)
	registerReview(server, store)
	registerSkip(server, store)
	registerMigrate(server, store)
	registerInbox(server, store)

	return server
}

// JSON representation of a ticket for MCP responses.
type ticketJSON struct {
	ID            string       `json:"id"`
	Status        string       `json:"status,omitempty"`
	Stage         string       `json:"stage,omitempty"`
	Review        string       `json:"review,omitempty"`
	Risk          string       `json:"risk,omitempty"`
	Deps          []string     `json:"deps"`
	Links         []string     `json:"links"`
	Created       string       `json:"created"`
	Type          string       `json:"type"`
	Priority      int          `json:"priority"`
	Assignee      string       `json:"assignee,omitempty"`
	ExternalRef   string       `json:"external_ref,omitempty"`
	Branch        string       `json:"branch,omitempty"`
	Parent        string       `json:"parent,omitempty"`
	Tags          []string     `json:"tags,omitempty"`
	Skipped       []string     `json:"skipped,omitempty"`
	Conversations []string     `json:"conversations,omitempty"`
	Title         string       `json:"title"`
	Description   string       `json:"description,omitempty"`
	Design        string       `json:"design,omitempty"`
	Acceptance    string       `json:"acceptance_criteria,omitempty"`
	TestResults   string       `json:"test_results,omitempty"`
	Notes         []noteJSON   `json:"notes,omitempty"`
	Reviews       []reviewJSON `json:"reviews,omitempty"`
}

type reviewJSON struct {
	Timestamp string `json:"timestamp"`
	Reviewer  string `json:"reviewer"`
	Verdict   string `json:"verdict"`
	Comment   string `json:"comment,omitempty"`
	Stage     string `json:"stage,omitempty"`
}

type noteJSON struct {
	Timestamp string `json:"timestamp"`
	Text      string `json:"text"`
}

func toJSON(t *ticket.Ticket) ticketJSON {
	j := ticketJSON{
		ID:            t.ID,
		Status:        string(t.Status),
		Stage:         string(t.Stage),
		Review:        string(t.Review),
		Risk:          string(t.Risk),
		Deps:          nonNil(t.Deps),
		Links:         nonNil(t.Links),
		Created:       t.Created.UTC().Format("2006-01-02T15:04:05Z"),
		Type:          string(t.Type),
		Priority:      t.Priority,
		Assignee:      t.Assignee,
		ExternalRef:   t.ExternalRef,
		Branch:        t.Branch,
		Parent:        t.Parent,
		Tags:          t.Tags,
		Conversations: t.Conversations,
		Title:         t.Title,
	}

	for _, s := range t.Skipped {
		j.Skipped = append(j.Skipped, string(s))
	}

	// Extract body sections.
	body := t.Body
	if body != "" {
		j.Description, j.Design, j.Acceptance, j.TestResults = parseSections(body)
	}

	for _, n := range t.Notes {
		j.Notes = append(j.Notes, noteJSON{
			Timestamp: n.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Text:      n.Text,
		})
	}

	for _, r := range t.Reviews {
		j.Reviews = append(j.Reviews, reviewJSON{
			Timestamp: r.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Reviewer:  r.Reviewer,
			Verdict:   r.Verdict,
			Comment:   r.Comment,
			Stage:     string(r.Stage),
		})
	}

	return j
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func parseSections(body string) (desc, design, acceptance, testResults string) {
	lines := strings.Split(body, "\n")
	var current *string
	var buf []string

	flush := func() {
		if current != nil {
			*current = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}

	desc = ""
	current = &desc

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "## Design"):
			flush()
			current = &design
		case strings.HasPrefix(line, "## Acceptance"):
			flush()
			current = &acceptance
		case strings.HasPrefix(line, "## Test Results"):
			flush()
			current = &testResults
		case strings.HasPrefix(line, "## "):
			// Other section - stop capturing known sections.
			flush()
			current = nil
		default:
			buf = append(buf, line)
		}
	}
	flush()
	return
}

func textResult(text string) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}, nil
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return textResult(string(data))
}

func errResult(format string, a ...any) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, a...)},
		},
		IsError: true,
	}, nil
}

// --- Tool registrations ---

type listArgs struct {
	Status   string `json:"status,omitempty" jsonschema:"filter by status: open, in_progress, needs_testing, closed"`
	Type     string `json:"type,omitempty" jsonschema:"filter by type: bug, feature, task, epic, chore"`
	Priority *int   `json:"priority,omitempty" jsonschema:"filter by priority (0-4)"`
	Assignee string `json:"assignee,omitempty" jsonschema:"filter by assignee name"`
	Tag      string `json:"tag,omitempty" jsonschema:"filter by tag"`
	Parent   string `json:"parent,omitempty" jsonschema:"filter by parent ticket ID"`
}

func registerList(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_list",
		Description: "List tickets with optional filters. Returns non-closed tickets by default.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
		tickets, err := store.List()
		if err != nil {
			r, _ := errResult("failed to list tickets: %v", err)
			return r, nil, nil
		}

		opts := ticket.DefaultListOptions()
		if args.Status != "" {
			opts.Status = ticket.Status(args.Status)
		} else {
			var filtered []*ticket.Ticket
			for _, t := range tickets {
				if t.Status != ticket.StatusClosed {
					filtered = append(filtered, t)
				}
			}
			tickets = filtered
		}
		if args.Type != "" {
			opts.Type = ticket.TicketType(args.Type)
		}
		if args.Priority != nil {
			opts.Priority = *args.Priority
		}
		if args.Assignee != "" {
			opts.Assignee = args.Assignee
		}
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		if args.Parent != "" {
			opts.Parent = args.Parent
		}

		tickets = ticket.Filter(tickets, opts)
		ticket.SortByStatusPriorityID(tickets)

		var result []ticketJSON
		for _, t := range tickets {
			result = append(result, toJSON(t))
		}

		r, err := jsonResult(result)
		return r, nil, err
	})
}

type showArgs struct {
	ID string `json:"id" jsonschema:"ticket ID (supports partial matching)"`
}

func registerShow(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_show",
		Description: "Show full details of a ticket by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args showArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type createArgs struct {
	Title       string `json:"title" jsonschema:"ticket title"`
	Description string `json:"description,omitempty" jsonschema:"description text"`
	Design      string `json:"design,omitempty" jsonschema:"design notes"`
	Acceptance  string `json:"acceptance,omitempty" jsonschema:"acceptance criteria"`
	Type        string `json:"type,omitempty" jsonschema:"ticket type: bug, feature, task, epic, chore (default: task)"`
	Priority    *int   `json:"priority,omitempty" jsonschema:"priority 0-4, 0=highest (default: 2)"`
	Assignee    string `json:"assignee,omitempty" jsonschema:"assignee name"`
	Parent      string `json:"parent,omitempty" jsonschema:"parent ticket ID"`
	Tags        string `json:"tags,omitempty" jsonschema:"comma-separated tags"`
	ExternalRef string `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string `json:"branch,omitempty" jsonschema:"git branch name"`
}

func registerCreate(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_create",
		Description: "Create a new ticket.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		if args.Title == "" {
			r, _ := errResult("title is required")
			return r, nil, nil
		}

		t := &ticket.Ticket{
			ID:       ticket.GenerateID(args.Title),
			Title:    args.Title,
			Status:   ticket.StatusOpen,
			Stage:    ticket.StageTriage,
			Priority: 2,
			Created:  time.Now().UTC(),
		}

		if args.Type != "" {
			t.Type = ticket.TicketType(args.Type)
		} else {
			t.Type = ticket.TypeTask
		}
		if args.Priority != nil {
			t.Priority = *args.Priority
		}
		if args.Assignee != "" {
			t.Assignee = args.Assignee
		}
		if args.Parent != "" {
			t.Parent = args.Parent
		}
		if args.ExternalRef != "" {
			t.ExternalRef = args.ExternalRef
		}
		if args.Branch != "" {
			t.Branch = args.Branch
		}
		if args.Tags != "" {
			t.Tags = strings.Split(args.Tags, ",")
			for i := range t.Tags {
				t.Tags[i] = strings.TrimSpace(t.Tags[i])
			}
		}

		// Build body.
		var body strings.Builder
		if args.Description != "" {
			body.WriteString(args.Description + "\n")
		}
		if args.Design != "" {
			body.WriteString("\n## Design\n\n" + args.Design + "\n")
		}
		if args.Acceptance != "" {
			body.WriteString("\n## Acceptance Criteria\n\n" + args.Acceptance + "\n")
		}
		t.Body = body.String()

		if err := store.Create(t); err != nil {
			r, _ := errResult("failed to create ticket: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type editArgs struct {
	ID          string `json:"id" jsonschema:"ticket ID"`
	Title       string `json:"title,omitempty" jsonschema:"new title"`
	Status      string `json:"status,omitempty" jsonschema:"new status: open, in_progress, needs_testing, closed"`
	Type        string `json:"type,omitempty" jsonschema:"new type"`
	Priority    *int   `json:"priority,omitempty" jsonschema:"new priority (0-4)"`
	Assignee    string `json:"assignee,omitempty" jsonschema:"new assignee"`
	Parent      string `json:"parent,omitempty" jsonschema:"new parent ticket ID"`
	Tags        string `json:"tags,omitempty" jsonschema:"comma-separated tags (replaces existing)"`
	ExternalRef string `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string `json:"branch,omitempty" jsonschema:"git branch name"`
	Description string `json:"description,omitempty" jsonschema:"new description text"`
	Design      string `json:"design,omitempty" jsonschema:"new design text"`
	Acceptance  string `json:"acceptance,omitempty" jsonschema:"new acceptance criteria"`
	TestResults string `json:"test_results,omitempty" jsonschema:"test results to record"`
}

func registerEdit(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_edit",
		Description: "Edit an existing ticket's fields.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args editArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}

		if args.Title != "" {
			t.Title = args.Title
		}
		if args.Status != "" {
			t.Status = ticket.Status(args.Status)
		}
		if args.Type != "" {
			t.Type = ticket.TicketType(args.Type)
		}
		if args.Priority != nil {
			t.Priority = *args.Priority
		}
		if args.Assignee != "" {
			t.Assignee = args.Assignee
		}
		if args.Parent != "" {
			t.Parent = args.Parent
		}
		if args.ExternalRef != "" {
			t.ExternalRef = args.ExternalRef
		}
		if args.Branch != "" {
			t.Branch = args.Branch
		}
		if args.Tags != "" {
			t.Tags = strings.Split(args.Tags, ",")
			for i := range t.Tags {
				t.Tags[i] = strings.TrimSpace(t.Tags[i])
			}
		}

		if args.Description != "" {
			t.Body = ticket.UpdateSection(t.Body, "", args.Description)
		}
		if args.Design != "" {
			t.Body = ticket.UpdateSection(t.Body, "Design", args.Design)
		}
		if args.Acceptance != "" {
			t.Body = ticket.UpdateSection(t.Body, "Acceptance Criteria", args.Acceptance)
		}
		if args.TestResults != "" {
			t.Body = ticket.UpdateSection(t.Body, "Test Results", args.TestResults)
		}

		if err := store.Update(t); err != nil {
			r, _ := errResult("failed to update ticket: %v", err)
			return r, nil, nil
		}

		// Propagate status changes.
		if args.Status != "" {
			ticket.PropagateStatus(store, t.ID)
		}

		// Re-read to get propagated state.
		t, _ = store.Get(t.ID)
		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type addNoteArgs struct {
	ID   string `json:"id" jsonschema:"ticket ID"`
	Text string `json:"text" jsonschema:"note text to append"`
}

func registerAddNote(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_add_note",
		Description: "Append a timestamped note to a ticket.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args addNoteArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}

		t.Notes = append(t.Notes, ticket.Note{
			Timestamp: time.Now().UTC(),
			Text:      args.Text,
		})

		if err := store.Update(t); err != nil {
			r, _ := errResult("failed to update ticket: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type depArgs struct {
	ID    string `json:"id" jsonschema:"ticket ID"`
	DepID string `json:"dep_id" jsonschema:"dependency ticket ID"`
	Action string `json:"action" jsonschema:"add or remove"`
}

func registerDep(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_dep",
		Description: "Add or remove a dependency. The ticket (id) depends on dep_id.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args depArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}
		dep, err := store.Get(args.DepID)
		if err != nil {
			r, _ := errResult("dep ticket not found: %v", err)
			return r, nil, nil
		}

		switch args.Action {
		case "add":
			ticket.AddDep(t, dep.ID)
		case "remove":
			ticket.RemoveDep(t, dep.ID)
		default:
			r, _ := errResult("action must be 'add' or 'remove'")
			return r, nil, nil
		}

		if err := store.Update(t); err != nil {
			r, _ := errResult("failed to update ticket: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type linkArgs struct {
	ID       string `json:"id" jsonschema:"ticket ID"`
	TargetID string `json:"target_id" jsonschema:"ticket to link/unlink"`
	Action   string `json:"action" jsonschema:"add or remove"`
}

func registerLink(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_link",
		Description: "Add or remove a symmetric link between two tickets.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args linkArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}
		target, err := store.Get(args.TargetID)
		if err != nil {
			r, _ := errResult("target ticket not found: %v", err)
			return r, nil, nil
		}

		switch args.Action {
		case "add":
			ticket.AddLink(t, target)
		case "remove":
			ticket.RemoveLink(t, target)
		default:
			r, _ := errResult("action must be 'add' or 'remove'")
			return r, nil, nil
		}

		if err := store.Update(t); err != nil {
			r, _ := errResult("failed to update ticket: %v", err)
			return r, nil, nil
		}
		if err := store.Update(target); err != nil {
			r, _ := errResult("failed to update target ticket: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type readyArgs struct {
	Assignee string `json:"assignee,omitempty" jsonschema:"filter by assignee"`
	Tag      string `json:"tag,omitempty" jsonschema:"filter by tag"`
}

func registerReady(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_ready",
		Description: "List tickets that are ready to work on (all deps resolved, parent in_progress).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readyArgs) (*mcp.CallToolResult, any, error) {
		ready, err := ticket.ReadyTickets(store)
		if err != nil {
			r, _ := errResult("failed to get ready tickets: %v", err)
			return r, nil, nil
		}

		opts := ticket.DefaultListOptions()
		if args.Assignee != "" {
			opts.Assignee = args.Assignee
		}
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		ready = ticket.Filter(ready, opts)
		ticket.SortByPriorityID(ready)

		var result []ticketJSON
		for _, t := range ready {
			result = append(result, toJSON(t))
		}

		r, err := jsonResult(result)
		return r, nil, err
	})
}

func registerBlocked(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_blocked",
		Description: "List tickets that are blocked by unresolved dependencies.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readyArgs) (*mcp.CallToolResult, any, error) {
		blocked, err := ticket.BlockedTickets(store)
		if err != nil {
			r, _ := errResult("failed to get blocked tickets: %v", err)
			return r, nil, nil
		}

		opts := ticket.DefaultListOptions()
		if args.Assignee != "" {
			opts.Assignee = args.Assignee
		}
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		blocked = ticket.Filter(blocked, opts)
		ticket.SortByPriorityID(blocked)

		var result []ticketJSON
		for _, t := range blocked {
			result = append(result, toJSON(t))
		}

		r, err := jsonResult(result)
		return r, nil, err
	})
}

type emptyArgs struct{}

func registerWorkflow(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_workflow",
		Description: "Show the ticket workflow guide (types, stages, pipelines, conventions).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
		guide := "Ticket Workflow Guide\n\n"
		guide += "Types: feature, bug, task, epic, chore\n\n"
		guide += "Stage Pipelines (default, type-dependent):\n"
		guide += ticket.PipelineDescription()
		guide += "\nRisk levels: low, normal, high, critical\n"
		guide += "  Risk determines pipeline variant (e.g., normal adds review stages).\n"
		guide += "  Use ticket_pipelines for full variant details.\n\n"
		guide += "Review states: pending, approved, rejected\n\n"
		guide += "Commands:\n"
		guide += "- ticket_advance: move ticket to next stage (enforces gates)\n"
		guide += "- ticket_review: record approve/reject verdict\n"
		guide += "- ticket_skip: jump to a stage with reason\n"
		guide += "- ticket_inbox: show items needing human attention\n"
		guide += "- ticket_pipelines: full pipeline config (stages, variants, gates)\n\n"
		guide += "Conventions:\n"
		guide += "- Epics group related work; set parent to nest tasks under epics\n"
		guide += "- Dependencies gate readiness: a ticket is \"ready\" when all deps are done\n"
		guide += "- Gate checks enforce prerequisites at each stage transition\n"
		guide += "- Priority: 0=critical, 1=high, 2=normal, 3=low, 4=backlog"

		r, err := textResult(guide)
		return r, nil, err
	})
}

type advanceArgs struct {
	ID     string `json:"id" jsonschema:"ticket ID"`
	To     string `json:"to,omitempty" jsonschema:"target stage (default: next in pipeline)"`
	Reason string `json:"reason,omitempty" jsonschema:"reason for skip (required when skipping)"`
	Force  bool   `json:"force,omitempty" jsonschema:"bypass gate checks"`
}

func registerAdvance(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_advance",
		Description: "Advance a ticket to its next pipeline stage. Enforces gate checks unless force=true.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args advanceArgs) (*mcp.CallToolResult, any, error) {
		opts := ticket.AdvanceOptions{Force: args.Force}
		if args.To != "" {
			opts.SkipTo = ticket.Stage(args.To)
			opts.Reason = args.Reason
		}

		result, err := ticket.Advance(store, args.ID, opts)
		if err != nil {
			msg := err.Error()
			if result != nil && len(result.GateErrors) > 0 {
				msg += "\nGate failures:"
				for _, e := range result.GateErrors {
					msg += "\n  - " + e.Error()
				}
			}
			r, _ := errResult("%s", msg)
			return r, nil, nil
		}

		t, _ := store.Get(args.ID)
		r, jsonErr := jsonResult(toJSON(t))
		return r, nil, jsonErr
	})
}

type reviewArgs struct {
	ID       string `json:"id" jsonschema:"ticket ID"`
	Approve  bool   `json:"approve,omitempty" jsonschema:"approve the current stage"`
	Reject   bool   `json:"reject,omitempty" jsonschema:"reject the current stage"`
	Comment  string `json:"comment,omitempty" jsonschema:"review comment"`
	Reviewer string `json:"reviewer,omitempty" jsonschema:"reviewer identity (e.g. human:steve, agent:code-review)"`
}

func registerReview(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_review",
		Description: "Record a review verdict (approve or reject) on a ticket's current stage.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reviewArgs) (*mcp.CallToolResult, any, error) {
		if args.Approve == args.Reject {
			r, _ := errResult("specify exactly one of approve or reject")
			return r, nil, nil
		}

		reviewer := args.Reviewer
		if reviewer == "" {
			reviewer = "agent:mcp"
		}

		var verdict ticket.ReviewState
		if args.Approve {
			verdict = ticket.ReviewApproved
		} else {
			verdict = ticket.ReviewRejected
		}

		if err := ticket.SetReview(store, args.ID, reviewer, verdict, args.Comment); err != nil {
			r, _ := errResult("review failed: %v", err)
			return r, nil, nil
		}

		t, _ := store.Get(args.ID)
		r, jsonErr := jsonResult(toJSON(t))
		return r, nil, jsonErr
	})
}

type skipArgs struct {
	ID     string `json:"id" jsonschema:"ticket ID"`
	To     string `json:"to" jsonschema:"target stage to skip to"`
	Reason string `json:"reason" jsonschema:"reason for skipping stages"`
}

func registerSkip(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_skip",
		Description: "Skip a ticket to a named stage with an audit trail.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args skipArgs) (*mcp.CallToolResult, any, error) {
		_, err := ticket.Skip(store, args.ID, ticket.Stage(args.To), args.Reason)
		if err != nil {
			r, _ := errResult("skip failed: %v", err)
			return r, nil, nil
		}

		t, _ := store.Get(args.ID)
		r, jsonErr := jsonResult(toJSON(t))
		return r, nil, jsonErr
	})
}

func registerMigrate(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_migrate",
		Description: "Migrate all status-based tickets to stage pipeline. Idempotent.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
		count, err := ticket.MigrateAll(store)
		if err != nil {
			r, _ := errResult("migration failed: %v", err)
			return r, nil, nil
		}

		r, err := textResult(fmt.Sprintf("Migrated %d ticket(s)", count))
		return r, nil, err
	})
}

type inboxArgs struct{}

func registerInbox(server *mcp.Server, store *ticket.FileStore) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_inbox",
		Description: "Show tickets needing human attention, sorted by priority then age.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args inboxArgs) (*mcp.CallToolResult, any, error) {
		items, err := ticket.Inbox(store)
		if err != nil {
			r, _ := errResult("inbox failed: %v", err)
			return r, nil, nil
		}

		type inboxItemJSON struct {
			Ticket ticketJSON `json:"ticket"`
			Action string     `json:"action"`
			Detail string     `json:"detail"`
		}

		var result []inboxItemJSON
		for _, item := range items {
			result = append(result, inboxItemJSON{
				Ticket: toJSON(item.Ticket),
				Action: string(item.Action),
				Detail: item.Detail,
			})
		}

		r, jsonErr := jsonResult(result)
		return r, nil, jsonErr
	})
}
