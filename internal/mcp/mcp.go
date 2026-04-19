// Package mcp provides an MCP server for AI agent access to tickets.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/internal/project"
	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server with all ticket management tools registered.
// defaultProject scopes tools to a specific project when the caller doesn't
// provide an explicit project parameter. Empty string means no default (all projects).
func NewServer(store ticket.Store, defaultProject string, centralRoot string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "tk", Version: "0.1.0"},
		nil,
	)

	registerList(server, store, defaultProject)
	registerShow(server, store)
	registerCreate(server, store, defaultProject)
	registerEdit(server, store)
	registerAddNote(server, store)
	registerDep(server, store)
	registerLink(server, store)
	registerReady(server, store, defaultProject)
	registerBlocked(server, store)
	registerInbox(server, store, defaultProject)
	registerStoreInfo(server, centralRoot)

	return server
}

// Summary representation for list responses — metadata only, no body content.
type ticketSummaryJSON struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Type     string   `json:"type"`
	Priority int      `json:"priority"`
	Parent   string   `json:"parent,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Deps    []string          `json:"deps"`
	Links   []string          `json:"links"`
	Created string            `json:"created"`
	Extra   map[string]string `json:"-"`
}

func (j ticketSummaryJSON) MarshalJSON() ([]byte, error) {
	type alias ticketSummaryJSON
	data, err := json.Marshal(alias(j))
	if err != nil {
		return nil, err
	}
	if len(j.Extra) == 0 {
		return data, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for k, v := range j.Extra {
		b, _ := json.Marshal(v)
		m[k] = b
	}
	return json.Marshal(m)
}

func toSummaryJSON(t *ticket.Ticket) ticketSummaryJSON {
	return ticketSummaryJSON{
		ID:       t.ID,
		Title:    t.Title,
		Status:   string(t.Status),
		Type:     string(t.Type),
		Priority: t.Priority,
		Parent:   t.Parent,
		Tags:     t.Tags,
		Deps:     nonNil(t.Deps),
		Links:    nonNil(t.Links),
		Created:  t.Created.UTC().Format("2006-01-02T15:04:05Z"),
		Extra:    t.Extra,
	}
}

// Full JSON representation of a ticket for MCP responses.
type ticketJSON struct {
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	Deps          []string `json:"deps"`
	Links         []string `json:"links"`
	Created       string   `json:"created"`
	Type          string   `json:"type"`
	Priority      int      `json:"priority"`
	ExternalRef   string   `json:"external_ref,omitempty"`
	Branch        string   `json:"branch,omitempty"`
	Parent        string   `json:"parent,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	Design        string   `json:"design,omitempty"`
	Acceptance    string   `json:"acceptance_criteria,omitempty"`
	TestResults   string   `json:"test_results,omitempty"`
	Notes []noteJSON        `json:"notes,omitempty"`
	Extra map[string]string `json:"-"`
}

func (j ticketJSON) MarshalJSON() ([]byte, error) {
	type alias ticketJSON
	data, err := json.Marshal(alias(j))
	if err != nil {
		return nil, err
	}
	if len(j.Extra) == 0 {
		return data, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for k, v := range j.Extra {
		b, _ := json.Marshal(v)
		m[k] = b
	}
	return json.Marshal(m)
}

type noteJSON struct {
	Timestamp string `json:"timestamp"`
	Text      string `json:"text"`
}

func toJSON(t *ticket.Ticket) ticketJSON {
	j := ticketJSON{
		ID:            t.ID,
		Status:        string(t.Status),
		Deps:          nonNil(t.Deps),
		Links:         nonNil(t.Links),
		Created:       t.Created.UTC().Format("2006-01-02T15:04:05Z"),
		Type:          string(t.Type),
		Priority:      t.Priority,
		ExternalRef:   t.ExternalRef,
		Branch:        t.Branch,
		Parent:        t.Parent,
		Tags:          t.Tags,
		Title:         t.Title,
	}

	j.Extra = t.Extra

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

// filterByProject returns only tickets belonging to the given project.
// Uses ParseNamespacedID to extract the project from each ticket's ID.
func filterByProject(tickets []*ticket.Ticket, project string) []*ticket.Ticket {
	var filtered []*ticket.Ticket
	for _, t := range tickets {
		proj, _ := ticket.ParseNamespacedID(t.ID)
		if proj == project {
			filtered = append(filtered, t)
		}
	}
	return filtered
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
	Status   string    `json:"status,omitempty" jsonschema:"filter by status: backlog, ready, open, done, closed"`
	Type     string    `json:"type,omitempty" jsonschema:"filter by type: bug, feature, epic"`
	Priority *FlexInt  `json:"priority,omitempty" jsonschema:"filter by priority (0-4)"`
	Tag      string    `json:"tag,omitempty" jsonschema:"filter by tag"`
	Parent   string    `json:"parent,omitempty" jsonschema:"filter by parent ticket ID"`
	Project  string    `json:"project,omitempty" jsonschema:"filter by project name (multi-project mode)"`
	Offset   *FlexInt  `json:"offset,omitempty" jsonschema:"number of results to skip (default 0)"`
	Limit    *FlexInt  `json:"limit,omitempty" jsonschema:"max results to return (default 50, 0 for unlimited)"`
}

const defaultListLimit = 50

type listResultJSON struct {
	Tickets []ticketSummaryJSON `json:"tickets"`
	Total   int                 `json:"total"`
	Offset  int                 `json:"offset"`
	Limit   int                 `json:"limit"`
}

// resolveTicketsDirFromConfig resolves the tickets directory for a repo path
// using the central store config. Returns ("", false) if the path doesn't match
// a known central-mode project.
func resolveTicketsDirFromConfig(repoPath string) (string, bool) {
	cfg, err := project.Load()
	if err != nil {
		return "", false
	}
	name, _ := project.ResolveName(cfg, repoPath, "")
	if name == "" {
		return "", false
	}
	p, ok := cfg.Projects[name]
	if !ok || p.Store != "central" {
		return "", false
	}
	dir, err := project.CentralProjectDir(name)
	if err != nil {
		return "", false
	}
	return dir, true
}

// resolveProject returns the effective project: explicit arg > default > empty.
func resolveProject(explicit, defaultProject string) string {
	if explicit != "" {
		return explicit
	}
	return defaultProject
}

func registerList(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_list",
		Description: "List tickets with optional filters and pagination. Returns non-closed tickets by default. Default limit is 50; use offset/limit to paginate.",
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
				if t.Status != ticket.StatusDone && t.Status != ticket.StatusBacklog {
					filtered = append(filtered, t)
				}
			}
			tickets = filtered
		}
		if args.Type != "" {
			opts.Type = ticket.TicketType(args.Type)
		}
		if args.Priority != nil {
			opts.Priority = int(*args.Priority)
		}
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		if args.Parent != "" {
			opts.Parent = args.Parent
		}

		tickets = ticket.Filter(tickets, opts)
		if proj := resolveProject(args.Project, defaultProject); proj != "" {
			tickets = filterByProject(tickets, proj)
		}
		ticket.SortByStatusPriorityID(tickets)

		total := len(tickets)

		// Apply pagination.
		offset := 0
		if args.Offset != nil && int(*args.Offset) > 0 {
			offset = int(*args.Offset)
		}
		limit := defaultListLimit
		if args.Limit != nil {
			if int(*args.Limit) == 0 {
				limit = total // 0 = unlimited
			} else if int(*args.Limit) > 0 {
				limit = int(*args.Limit)
			}
		}

		offset = min(offset, total)
		end := min(offset+limit, total)
		tickets = tickets[offset:end]

		items := []ticketSummaryJSON{}
		for _, t := range tickets {
			items = append(items, toSummaryJSON(t))
		}

		r, err := jsonResult(listResultJSON{
			Tickets: items,
			Total:   total,
			Offset:  offset,
			Limit:   limit,
		})
		return r, nil, err
	})
}

type showArgs struct {
	ID string `json:"id" jsonschema:"ticket ID (supports partial matching)"`
}

func registerShow(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
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
	Type        string   `json:"type,omitempty" jsonschema:"ticket type: bug, feature, epic (default: feature)"`
	Priority    *FlexInt `json:"priority,omitempty" jsonschema:"priority 0-4, 0=highest (default: 2)"`
	Parent      string `json:"parent,omitempty" jsonschema:"parent ticket ID"`
	Tags        string `json:"tags,omitempty" jsonschema:"comma-separated tags"`
	ExternalRef string `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string `json:"branch,omitempty" jsonschema:"git branch name"`
	Project     string            `json:"project,omitempty" jsonschema:"project name for multi-project mode (namespaces the ticket ID)"`
	Repo        string            `json:"repo,omitempty" jsonschema:"path to repo root; resolves ticket store via .tickets/ directory or central store config"`
	Set         map[string]string `json:"set,omitempty" jsonschema:"set extra fields (key: value)"`
}

func registerCreate(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_create",
		Description: "Create a new ticket. Supports optional repo parameter for cross-repo creation.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		if args.Title == "" {
			r, _ := errResult("title is required")
			return r, nil, nil
		}

		targetStore := store
		if args.Repo != "" {
			abs, err := filepath.Abs(args.Repo)
			if err != nil {
				r, _ := errResult("invalid repo path: %v", err)
				return r, nil, nil
			}
			dir, ok := ticket.FindTicketsDir(abs)
			if !ok {
				// No .tickets/ dir — try central store config for this repo path.
				dir, ok = resolveTicketsDirFromConfig(abs)
			}
			if !ok {
				r, _ := errResult("no ticket store found for %s", abs)
				return r, nil, nil
			}
			targetStore = ticket.NewFileStore(dir)
		}

		t := &ticket.Ticket{
			ID:       ticket.GenerateID(args.Title),
			Title:    args.Title,
			Status:   ticket.StatusBacklog,
			Priority: 2,
			Created:  time.Now().UTC(),
		}

		if args.Type != "" {
			t.Type = ticket.TicketType(args.Type)
		} else {
			t.Type = ticket.TypeFeature
		}
		if args.Priority != nil {
			t.Priority = int(*args.Priority)
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

		if len(args.Set) > 0 {
			t.Extra = map[string]string{}
			for k, v := range args.Set {
				if err := ticket.ValidateExtraKey(k); err != nil {
					r, _ := errResult("invalid extra key: %v", err)
					return r, nil, nil
				}
				if v != "" {
					if err := ticket.ValidateExtraValue(v); err != nil {
						r, _ := errResult("invalid extra value for %q: %v", k, err)
						return r, nil, nil
					}
					t.Extra[k] = v
				}
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

		if proj := resolveProject(args.Project, defaultProject); proj != "" {
			t.ID = ticket.FormatNamespacedID(proj, t.ID)
		}

		if err := targetStore.Create(t); err != nil {
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
	Status      string `json:"status,omitempty" jsonschema:"status: backlog, ready, open, done, closed"`
	Type        string `json:"type,omitempty" jsonschema:"new type"`
	Priority    *FlexInt `json:"priority,omitempty" jsonschema:"new priority (0-4)"`
	Parent      string `json:"parent,omitempty" jsonschema:"new parent ticket ID"`
	Tags        string `json:"tags,omitempty" jsonschema:"comma-separated tags (replaces existing)"`
	ExternalRef string `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string `json:"branch,omitempty" jsonschema:"git branch name"`
	Description string `json:"description,omitempty" jsonschema:"new description text"`
	Design      string `json:"design,omitempty" jsonschema:"new design text"`
	Acceptance  string `json:"acceptance,omitempty" jsonschema:"new acceptance criteria"`
	TestResults string            `json:"test_results,omitempty" jsonschema:"test results to record"`
	Set         map[string]string `json:"set,omitempty" jsonschema:"set extra fields (key: value to set, key: empty string to remove)"`
}

func registerEdit(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
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
			if err := ticket.ValidateStatus(ticket.Status(args.Status)); err != nil {
				r, _ := errResult("invalid status: %v", err)
				return r, nil, nil
			}
			t.Status = ticket.Status(args.Status)
		}
		if args.Type != "" {
			t.Type = ticket.TicketType(args.Type)
		}
		if args.Priority != nil {
			t.Priority = int(*args.Priority)
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

		if len(args.Set) > 0 {
			if t.Extra == nil {
				t.Extra = map[string]string{}
			}
			for k, v := range args.Set {
				if err := ticket.ValidateExtraKey(k); err != nil {
					r, _ := errResult("invalid extra key: %v", err)
					return r, nil, nil
				}
				if v == "" {
					delete(t.Extra, k)
				} else {
					if err := ticket.ValidateExtraValue(v); err != nil {
						r, _ := errResult("invalid extra value for %q: %v", k, err)
						return r, nil, nil
					}
					t.Extra[k] = v
				}
			}
		}

		if err := store.Update(t); err != nil {
			r, _ := errResult("failed to update ticket: %v", err)
			return r, nil, nil
		}

		// Re-read to get current state.
		t, err = store.Get(t.ID)
		if err != nil {
			r, _ := errResult("failed to re-read ticket: %v", err)
			return r, nil, nil
		}
		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type addNoteArgs struct {
	ID   string `json:"id" jsonschema:"ticket ID"`
	Text string `json:"text" jsonschema:"note text to append"`
}

func registerAddNote(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
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

func registerDep(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
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

func registerLink(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
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
	Tag     string `json:"tag,omitempty" jsonschema:"filter by tag"`
	Project string `json:"project,omitempty" jsonschema:"filter by project name (multi-project mode)"`
}

func registerReady(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_ready",
		Description: "List tickets that are ready to work on (all deps resolved, parent in_progress).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readyArgs) (*mcp.CallToolResult, any, error) {
		ready, err := ticket.ReadyTickets(store)
		if err != nil {
			r, _ := errResult("failed to get ready tickets: %v", err)
			return r, nil, nil
		}

		opts := ticket.DefaultListOptions()
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		ready = ticket.Filter(ready, opts)
		if proj := resolveProject(args.Project, defaultProject); proj != "" {
			ready = filterByProject(ready, proj)
		}
		ticket.SortByPriorityID(ready)

		result := []ticketJSON{}
		for _, t := range ready {
			result = append(result, toJSON(t))
		}

		r, err := jsonResult(result)
		return r, nil, err
	})
}

func registerBlocked(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_blocked",
		Description: "List tickets that are blocked by unresolved dependencies.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readyArgs) (*mcp.CallToolResult, any, error) {
		blocked, err := ticket.BlockedTickets(store)
		if err != nil {
			r, _ := errResult("failed to get blocked tickets: %v", err)
			return r, nil, nil
		}

		opts := ticket.DefaultListOptions()
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		blocked = ticket.Filter(blocked, opts)
		ticket.SortByPriorityID(blocked)

		result := []ticketJSON{}
		for _, t := range blocked {
			result = append(result, toJSON(t))
		}

		r, err := jsonResult(result)
		return r, nil, err
	})
}

type emptyArgs struct{}

type inboxArgs struct {
	Project string `json:"project,omitempty" jsonschema:"filter by project name (multi-project mode)"`
}

func registerInbox(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
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
		effectiveProject := resolveProject(args.Project, defaultProject)
		for _, item := range items {
			if effectiveProject != "" {
				proj, _ := ticket.ParseNamespacedID(item.Ticket.ID)
				if proj != effectiveProject {
					continue
				}
			}
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

func registerStoreInfo(server *mcp.Server, centralRoot string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_store_info",
		Description: "Return central store root path and per-project ticket directory paths. Only available in central mode.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
		if centralRoot == "" {
			r, _ := errResult("ticket_store_info requires central mode (tk serve --central)")
			return r, nil, nil
		}

		ticketsDir := filepath.Join(centralRoot, "tickets")
		entries, err := os.ReadDir(ticketsDir)
		if err != nil {
			r, _ := errResult("failed to read tickets directory: %v", err)
			return r, nil, nil
		}

		projects := map[string]string{}
		for _, e := range entries {
			if e.IsDir() {
				projects[e.Name()] = filepath.Join(ticketsDir, e.Name())
			}
		}

		info := map[string]any{
			"central_root": centralRoot,
			"projects":     projects,
		}

		r, jsonErr := jsonResult(info)
		return r, nil, jsonErr
	})
}
