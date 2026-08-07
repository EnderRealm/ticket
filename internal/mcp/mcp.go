// Package mcp provides an MCP server for AI agent access to tickets.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
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
	registerDelete(server, store)
	registerAddNote(server, store)
	registerDep(server, store)
	registerLink(server, store)
	registerReady(server, store, defaultProject)
	registerFrontier(server, store, defaultProject)
	registerBlocked(server, store, defaultProject)
	registerInbox(server, store, defaultProject)
	registerSearch(server, store, defaultProject)
	registerVerify(server, store, defaultProject)
	registerStoreInfo(server, centralRoot)

	return server
}

// Summary representation for list responses — metadata only, no body content.
type ticketSummaryJSON struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Type     string            `json:"type"`
	Priority int               `json:"priority"`
	Parent   string            `json:"parent,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Deps     []string          `json:"deps"`
	Links    []string          `json:"links"`
	Created  string            `json:"created"`
	Extra    map[string]string `json:"-"`
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
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	Abandoned   bool              `json:"abandoned,omitempty"`
	Deps        []string          `json:"deps"`
	Links       []string          `json:"links"`
	Created     string            `json:"created"`
	Type        string            `json:"type"`
	Priority    int               `json:"priority"`
	ExternalRef string            `json:"external_ref,omitempty"`
	Branch      string            `json:"branch,omitempty"`
	Parent      string            `json:"parent,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Design      string            `json:"design,omitempty"`
	Acceptance  string            `json:"acceptance_criteria,omitempty"`
	TestResults string            `json:"test_results,omitempty"`
	Notes       []noteJSON        `json:"notes,omitempty"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	DepCargo    map[string]string `json:"dep_cargo,omitempty"`
	Extra       map[string]string `json:"-"`
	// ClosedChildren names the children an edit that abandoned an epic closed
	// along with it. Set by ticket_edit alone — every other tool leaves it
	// empty, and it is omitted from the response then.
	ClosedChildren []string `json:"closed_children,omitempty"`
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
		ID:          t.ID,
		Status:      string(t.Status),
		Abandoned:   t.Abandoned,
		Deps:        nonNil(t.Deps),
		Links:       nonNil(t.Links),
		Created:     t.Created.UTC().Format("2006-01-02T15:04:05Z"),
		Type:        string(t.Type),
		Priority:    t.Priority,
		ExternalRef: t.ExternalRef,
		Branch:      t.Branch,
		Parent:      t.Parent,
		Tags:        t.Tags,
		Title:       t.Title,
		Outputs:     t.Outputs,
		DepCargo:    t.DepCargo,
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
	Status   string   `json:"status,omitempty" jsonschema:"filter by status: backlog, ready, open, done, closed"`
	Type     string   `json:"type,omitempty" jsonschema:"filter by type: bug, feature, epic"`
	Priority *FlexInt `json:"priority,omitempty" jsonschema:"filter by priority (0-4)"`
	Tag      string   `json:"tag,omitempty" jsonschema:"filter by tag"`
	Field    string   `json:"field,omitempty" jsonschema:"filter by extra field (key=value, substring match)"`
	Parent   string   `json:"parent,omitempty" jsonschema:"filter by parent ticket ID"`
	Project  string   `json:"project,omitempty" jsonschema:"filter by project name (multi-project mode)"`
	Offset   *FlexInt `json:"offset,omitempty" jsonschema:"number of results to skip (default 0)"`
	Limit    *FlexInt `json:"limit,omitempty" jsonschema:"max results to return (default 50, 0 for unlimited)"`
}

const defaultListLimit = 50

type listResultJSON struct {
	Tickets              []ticketSummaryJSON `json:"tickets"`
	Total                int                 `json:"total"`
	Offset               int                 `json:"offset"`
	Limit                int                 `json:"limit"`
	UnregisteredProjects []string            `json:"unregistered_projects,omitempty"`
}

// unregisteredProjects names the projects among these tickets that are not
// registered with the central store. Unregistered is a property of a project,
// not of a ticket, so it rides on the response rather than repeating on every
// row — but an agent reading a project's tickets should not have to call
// ticket_store_info to learn the project is unregistered. Bare IDs carry no
// project (single-project mode), so nothing is reported and no config is read.
// A config that will not load leaves the answer unknown, which is reported as
// nothing rather than as "registered".
func unregisteredProjects(tickets []*ticket.Ticket) []string {
	names := map[string]bool{}
	for _, t := range tickets {
		if proj, _ := ticket.ParseNamespacedID(t.ID); proj != "" {
			names[proj] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	cfg, err := project.Load()
	if err != nil {
		return nil
	}
	var out []string
	for name := range names {
		if !project.CentralRegistered(cfg, name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// resolveTicketsDirFromConfig resolves the tickets directory and project name
// for a repo path using the central store config. Returns ("", "", false) if
// the path doesn't match a known central-mode project.
func resolveTicketsDirFromConfig(repoPath string) (string, string, bool) {
	cfg, err := project.Load()
	if err != nil {
		return "", "", false
	}
	name, _ := project.ResolveName(cfg, repoPath, "")
	if name == "" {
		return "", "", false
	}
	if !project.CentralRegistered(cfg, name) {
		return "", "", false
	}
	dir, err := project.CentralProjectDir(name)
	if err != nil {
		return "", "", false
	}
	return dir, name, true
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
		Description: "List tickets with optional filters and pagination. Returns non-closed tickets by default. Default limit is 50; use offset/limit to paginate. `unregistered_projects` names any project in the result set with a directory in the store but no `store: central` entry in config, so no repo is registered to it — run `tk init` in that project's repo to register it.",
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
			opts.Priority = int(*args.Priority)
		}
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		if args.Parent != "" {
			opts.Parent = args.Parent
		}
		if args.Field != "" {
			key, value, err := ticket.ParseFieldFilter(args.Field)
			if err != nil {
				r, _ := errResult("invalid field filter: %v", err)
				return r, nil, nil
			}
			opts.FieldKey = key
			opts.FieldValue = value
		}

		tickets = ticket.Filter(tickets, opts)
		if proj := resolveProject(args.Project, defaultProject); proj != "" {
			tickets = filterByProject(tickets, proj)
		}
		ticket.SortByStatusPriorityID(tickets)

		total := len(tickets)
		// Computed over the whole filtered set, like total: derived from the page
		// instead, an unregistered project whose tickets sort past the first page
		// would go unnamed, making the signal depend on which page was asked for.
		unregistered := unregisteredProjects(tickets)

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
			Tickets:              items,
			Total:                total,
			Offset:               offset,
			Limit:                limit,
			UnregisteredProjects: unregistered,
		})
		return r, nil, err
	})
}

type showArgs struct {
	ID           string    `json:"id" jsonschema:"ticket ID (supports partial matching)"`
	NotesLimit   *FlexInt  `json:"notes_limit,omitempty" jsonschema:"max number of notes to return, newest first (default 20, 0 for all)"`
	NotesOffset  *FlexInt  `json:"notes_offset,omitempty" jsonschema:"number of notes to skip, counted from newest (default 0)"`
	MetadataOnly *FlexBool `json:"metadata_only,omitempty" jsonschema:"when true, omit notes entirely"`
}

const defaultShowNotesLimit = 20

// showResultJSON wraps ticketJSON with note-paging metadata so a token-
// conscious caller can tell whether there are more notes to fetch.
type showResultJSON struct {
	ticketJSON
	NotesTotal int `json:"notes_total"`
	NotesShown int `json:"notes_shown"`
}

func (s showResultJSON) MarshalJSON() ([]byte, error) {
	inner, err := json.Marshal(s.ticketJSON)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(inner, &m); err != nil {
		return nil, err
	}
	total, _ := json.Marshal(s.NotesTotal)
	shown, _ := json.Marshal(s.NotesShown)
	m["notes_total"] = total
	m["notes_shown"] = shown
	return json.Marshal(m)
}

func registerShow(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_show",
		Description: "Show full details of a ticket by ID. Notes are trimmed to the newest 20 by default; use notes_limit=0 for all, metadata_only=true for none, or notes_offset to page further back.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args showArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}

		total := len(t.Notes)

		metadataOnly := false
		if args.MetadataOnly != nil {
			metadataOnly = bool(*args.MetadataOnly)
		}

		limit := defaultShowNotesLimit
		if args.NotesLimit != nil {
			limit = int(*args.NotesLimit)
		}
		offset := 0
		if args.NotesOffset != nil && int(*args.NotesOffset) > 0 {
			offset = int(*args.NotesOffset)
		}

		// Slice a newest-first window of size `limit`, paged back by `offset`.
		// limit<=0 means "all"; metadata_only wins over both.
		if metadataOnly {
			t.Notes = nil
		} else if limit > 0 {
			end := total - offset
			if end < 0 {
				end = 0
			}
			start := end - limit
			if start < 0 {
				start = 0
			}
			t.Notes = t.Notes[start:end]
		}

		resp := showResultJSON{
			ticketJSON: toJSON(t),
			NotesTotal: total,
			NotesShown: len(t.Notes),
		}
		r, err := jsonResult(resp)
		return r, nil, err
	})
}

type createArgs struct {
	Title       string            `json:"title" jsonschema:"ticket title"`
	Description string            `json:"description,omitempty" jsonschema:"description text"`
	Design      string            `json:"design,omitempty" jsonschema:"design notes"`
	Acceptance  string            `json:"acceptance,omitempty" jsonschema:"acceptance criteria"`
	Type        string            `json:"type,omitempty" jsonschema:"ticket type: bug, feature, epic (default: feature)"`
	Priority    *FlexInt          `json:"priority,omitempty" jsonschema:"priority 0-4, 0=highest (default: 2)"`
	Parent      string            `json:"parent,omitempty" jsonschema:"parent epic ID; must name an epic in the same project"`
	Tags        string            `json:"tags,omitempty" jsonschema:"comma-separated tags"`
	ExternalRef string            `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string            `json:"branch,omitempty" jsonschema:"git branch name"`
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
			// A local .tickets/ store never sees namespaced IDs; only a central
			// project dir carries a project namespace.
			name := ""
			dir, ok := ticket.FindTicketsDir(abs)
			if !ok {
				// No .tickets/ dir — try central store config for this repo path.
				dir, name, ok = resolveTicketsDirFromConfig(abs)
			}
			if !ok {
				r, _ := errResult("no ticket store found for %s", abs)
				return r, nil, nil
			}
			targetStore = ticket.NewProjectFileStore(dir, name)
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
	ID          string            `json:"id" jsonschema:"ticket ID"`
	Title       string            `json:"title,omitempty" jsonschema:"new title"`
	Status      string            `json:"status,omitempty" jsonschema:"status: backlog, ready, open, done, closed. An epic's status is derived from its children; the only one that can be set on an epic is closed, which closes its children too"`
	Type        string            `json:"type,omitempty" jsonschema:"new type"`
	Priority    *FlexInt          `json:"priority,omitempty" jsonschema:"new priority (0-4)"`
	Parent      *string           `json:"parent,omitempty" jsonschema:"new parent epic ID; must name an epic in the same project. Pass an empty string to clear it"`
	Tags        string            `json:"tags,omitempty" jsonschema:"comma-separated tags (replaces existing)"`
	ExternalRef string            `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string            `json:"branch,omitempty" jsonschema:"git branch name"`
	Description string            `json:"description,omitempty" jsonschema:"new description text"`
	Design      string            `json:"design,omitempty" jsonschema:"new design text"`
	Acceptance  string            `json:"acceptance,omitempty" jsonschema:"new acceptance criteria"`
	TestResults string            `json:"test_results,omitempty" jsonschema:"test results to record"`
	Set         map[string]string `json:"set,omitempty" jsonschema:"set extra fields (key: value to set, key: empty string to remove)"`
	Outputs     map[string]string `json:"outputs,omitempty" jsonschema:"set outputs the ticket produced, e.g. branch/commit/artifacts (key: value to set, key: empty string to remove)"`
}

func registerEdit(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_edit",
		Description: "Edit an existing ticket's fields. Closing an epic closes its children too; the response names them in `closed_children`.",
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
		// A pointer, unlike the other string fields: the remedy for a rejected
		// parent is "repoint or clear it", so an explicit "" has to mean clear
		// rather than "no change" — otherwise the only fix an agent can apply
		// over MCP is repointing.
		if args.Parent != nil {
			t.Parent = *args.Parent
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

		if len(args.Outputs) > 0 {
			if t.Outputs == nil {
				t.Outputs = map[string]string{}
			}
			for k, v := range args.Outputs {
				if err := ticket.ValidateOutputKey(k); err != nil {
					r, _ := errResult("invalid output key: %v", err)
					return r, nil, nil
				}
				if v == "" {
					delete(t.Outputs, k)
				} else {
					if err := ticket.ValidateOutputValue(v); err != nil {
						r, _ := errResult("invalid output value for %q: %v", k, err)
						return r, nil, nil
					}
					t.Outputs[k] = v
				}
			}
		}

		// An omitted status means no change, so only a status the caller passed
		// says anything about an epic's abandon intent.
		closed, err := ticket.SaveEdit(store, t, args.Status != "")
		if err != nil {
			r, _ := errResult("failed to update ticket: %v", err)
			return r, nil, nil
		}

		// Re-read to get current state.
		t, err = store.Get(t.ID)
		if err != nil {
			r, _ := errResult("failed to re-read ticket: %v", err)
			return r, nil, nil
		}
		// The edit closed other tickets, so it reports them rather than leaving
		// the caller to discover the cascade by listing the epic's children.
		j := toJSON(t)
		j.ClosedChildren = closed
		r, err := jsonResult(j)
		return r, nil, err
	})
}

type deleteArgs struct {
	ID string `json:"id" jsonschema:"ticket ID (supports partial matching)"`
}

func registerDelete(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_delete",
		Description: "Delete a ticket permanently by ID. This is a hard delete that removes the ticket file; it is distinct from setting status to closed.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}

		if err := store.Delete(t.ID); err != nil {
			r, _ := errResult("failed to delete ticket: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(map[string]string{"deleted": t.ID})
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
	ID     string `json:"id" jsonschema:"ticket ID"`
	DepID  string `json:"dep_id" jsonschema:"dependency ticket ID"`
	Action string `json:"action" jsonschema:"add or remove"`
	Cargo  string `json:"cargo,omitempty" jsonschema:"optional (add only): the concrete artifact that flows across this edge, e.g. a branch, schema or doc. Edges with no cargo are flagged as unannotated by tk dep tree"`
}

func registerDep(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_dep",
		Description: "Add or remove a dependency. The ticket (id) depends on dep_id. On add, cargo optionally names what concretely flows across the edge.",
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
			if strings.TrimSpace(args.Cargo) != "" {
				if err := ticket.SetDepCargo(t, dep.ID, args.Cargo); err != nil {
					r, _ := errResult("invalid cargo: %v", err)
					return r, nil, nil
				}
			}
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

		result := []ticketSummaryJSON{}
		for _, t := range ready {
			result = append(result, toSummaryJSON(t))
		}

		r, err := jsonResult(result)
		return r, nil, err
	})
}

func registerFrontier(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_frontier",
		Description: "List the schedulable frontier: tickets with status ready whose dependencies are all done or closed. The parallel-safe set to start next.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readyArgs) (*mcp.CallToolResult, any, error) {
		frontier, err := ticket.FrontierTickets(store)
		if err != nil {
			r, _ := errResult("failed to get frontier tickets: %v", err)
			return r, nil, nil
		}

		opts := ticket.DefaultListOptions()
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		frontier = ticket.Filter(frontier, opts)
		if proj := resolveProject(args.Project, defaultProject); proj != "" {
			frontier = filterByProject(frontier, proj)
		}
		ticket.SortByPriorityID(frontier)

		result := []ticketSummaryJSON{}
		for _, t := range frontier {
			result = append(result, toSummaryJSON(t))
		}

		r, err := jsonResult(result)
		return r, nil, err
	})
}

func registerBlocked(server *mcp.Server, store ticket.Store, defaultProject string) {
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
		if proj := resolveProject(args.Project, defaultProject); proj != "" {
			blocked = filterByProject(blocked, proj)
		}
		ticket.SortByPriorityID(blocked)

		result := []ticketSummaryJSON{}
		for _, t := range blocked {
			result = append(result, toSummaryJSON(t))
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
			Ticket ticketSummaryJSON `json:"ticket"`
			Action string            `json:"action"`
			Detail string            `json:"detail"`
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
				Ticket: toSummaryJSON(item.Ticket),
				Action: string(item.Action),
				Detail: item.Detail,
			})
		}

		r, jsonErr := jsonResult(result)
		return r, nil, jsonErr
	})
}

type searchArgs struct {
	Query   string   `json:"query" jsonschema:"search query; ranked by relevance across title, body, and notes"`
	Project string   `json:"project,omitempty" jsonschema:"filter by project name (multi-project mode)"`
	Limit   *FlexInt `json:"limit,omitempty" jsonschema:"max results to return (default 50, 0 for unlimited)"`
}

// searchMatchJSON wraps a ticket summary with the field the query matched and a
// one-line context snippet around the earliest matched term.
type searchMatchJSON struct {
	ticketSummaryJSON
	MatchField string `json:"match_field,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
}

func (s searchMatchJSON) MarshalJSON() ([]byte, error) {
	inner, err := json.Marshal(s.ticketSummaryJSON)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(inner, &m); err != nil {
		return nil, err
	}
	if s.MatchField != "" {
		field, _ := json.Marshal(s.MatchField)
		m["match_field"] = field
	}
	if s.Snippet != "" {
		snippet, _ := json.Marshal(s.Snippet)
		m["snippet"] = snippet
	}
	return json.Marshal(m)
}

type searchResultJSON struct {
	Matches []searchMatchJSON `json:"matches"`
	Total   int               `json:"total"`
}

func registerSearch(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_search",
		Description: "Search tickets by relevance across title, body, and notes. Use before creating a ticket to find similar or duplicate tickets. Returns matches ranked best-first, each with the match_field and a context snippet around the matched term.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		tickets, err := store.List()
		if err != nil {
			r, _ := errResult("failed to list tickets: %v", err)
			return r, nil, nil
		}

		if proj := resolveProject(args.Project, defaultProject); proj != "" {
			tickets = filterByProject(tickets, proj)
		}

		results := ticket.Search(tickets, args.Query)
		total := len(results)

		limit := defaultListLimit
		if args.Limit != nil {
			if int(*args.Limit) == 0 {
				limit = total // 0 = unlimited
			} else if int(*args.Limit) > 0 {
				limit = int(*args.Limit)
			}
		}
		if limit < total {
			results = results[:limit]
		}

		items := []searchMatchJSON{}
		for _, res := range results {
			items = append(items, searchMatchJSON{
				ticketSummaryJSON: toSummaryJSON(res.Ticket),
				MatchField:        res.Field,
				Snippet:           res.Snippet,
			})
		}

		r, err := jsonResult(searchResultJSON{
			Matches: items,
			Total:   total,
		})
		return r, nil, err
	})
}

type verifyArgs struct {
	ID string `json:"id" jsonschema:"ticket ID (supports partial matching)"`
}

func registerVerify(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_verify",
		Description: "Run the verify commands declared in a ticket's acceptance criteria (\"verify: <command>\" lines) and record the results on the ticket. Commands execute on the server host in the ticket's project repo directory, as argv and never through a shell: quotes group arguments, but ;, |, &&, $(), backticks and ~ are literal text passed to the command. A command whose program is not in the host user's machine-local verify_allow list is reported as refused without running — you cannot widen that list, from this tool or from ticket content, so report a refusal to the user rather than working around it. Criteria with no command are reported as unverified.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args verifyArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}

		criteria := ticket.ParseCriteria(ticket.AcceptanceCriteria(t.Body))
		if len(criteria) == 0 {
			r, _ := errResult("%s has no acceptance criteria", t.ID)
			return r, nil, nil
		}

		dir, err := verifyWorkDir(t.ID, defaultProject)
		if err != nil {
			r, _ := errResult("cannot resolve project directory: %v", err)
			return r, nil, nil
		}

		// The allow-list comes from machine-local config only — no tool argument
		// carries it, so a caller cannot widen what runs.
		allow, allowErr := project.VerifyAllow()
		results, err := ticket.RunVerify(ctx, criteria, dir, allow, allowErr)
		if err != nil {
			r, _ := errResult("cannot run verify commands: %v", err)
			return r, nil, nil
		}
		report := ticket.NewVerifyReport(t.ID, dir, results)

		// Record after the run so a store failure degrades to a reported
		// warning instead of discarding the results.
		t.Body = ticket.UpdateSection(t.Body, "Test Results", ticket.FormatVerifyRecord(results, time.Now().UTC()))
		if err := store.Update(t); err != nil {
			report.RecordError = fmt.Sprintf("failed to record verify results: %v", err)
		}

		r, err := jsonResult(report)
		return r, nil, err
	})
}

// verifyWorkDir resolves the repo directory a ticket's verify commands run in
// from the project config. Verify must never run in an arbitrary directory, so
// an unresolvable project path is an error.
func verifyWorkDir(id, defaultProject string) (string, error) {
	proj, _ := ticket.ParseNamespacedID(id)
	if proj == "" {
		proj = defaultProject
	}
	if proj == "" {
		return "", fmt.Errorf("ticket ID %q has no project namespace", id)
	}
	cfg, err := project.Load()
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	p, ok := cfg.Projects[proj]
	if !ok || p.Path == "" {
		return "", fmt.Errorf("project %q has no configured path", proj)
	}
	return p.Path, nil
}

func registerStoreInfo(server *mcp.Server, centralRoot string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_store_info",
		Description: "Return central store root path, per-project ticket directory paths, and which of those projects are unregistered: a directory in the store with no `store: central` entry in config, so no repo is registered to it — run `tk init` in that project's repo to register it. Only available in central mode.",
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
		cfg, cfgErr := project.Load()

		projects := map[string]string{}
		unregistered := []string{}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			projects[e.Name()] = filepath.Join(ticketsDir, e.Name())
			if cfgErr == nil && !project.CentralRegistered(cfg, e.Name()) {
				unregistered = append(unregistered, e.Name())
			}
		}

		info := map[string]any{
			"central_root": centralRoot,
			"projects":     projects,
		}
		if cfgErr != nil {
			// Config only fails to load when it is corrupt — exactly when the
			// store paths are what an agent needs to diagnose it. Report them
			// without the registration answer rather than failing the call. The
			// underlying error is not echoed: it wraps the yaml error, which
			// quotes the offending scalar, so a malformed config would leak a
			// config value (a git email, a project path, a spawn command) into
			// the response.
			info["note"] = "registration could not be determined: the ticket config (~/.ticket/config.yaml or <central_root>/config.yaml) could not be read"
		} else {
			info["unregistered"] = unregistered
		}

		r, jsonErr := jsonResult(info)
		return r, nil, jsonErr
	})
}
