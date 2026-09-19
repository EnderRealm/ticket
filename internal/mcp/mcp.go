// Package mcp provides an MCP server for AI agent access to tickets.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server owns both MCP sessions and their background verification workers.
// Callers using Connect directly must call Shutdown before leaving the host
// process. Run drains workers automatically when its transport closes.
type Server struct {
	*mcp.Server
	verify *verifyJobs
}

func (s *Server) Run(ctx context.Context, transport mcp.Transport) error {
	defer s.Shutdown()
	return s.Server.Run(ctx, transport)
}

// Shutdown refuses new verification jobs, cancels running commands, and waits
// for workers (including a complete result already being recorded) to exit.
func (s *Server) Shutdown() {
	s.verify.shutdown()
}

// NewServer creates an MCP server with all ticket management tools registered.
// defaultProject scopes tools to a specific project when the caller doesn't
// provide an explicit project parameter. Empty string means no default (all projects).
func NewServer(store ticket.Store, defaultProject string, centralRoot string) *Server {
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
	jobs := registerVerify(server, store, defaultProject)
	registerVerdictRecord(server, store)
	registerVerdictCurrent(server, store)
	registerApplyAction(server, store)
	registerDiscover(server, store, defaultProject)
	registerStoreInfo(server, centralRoot)

	return &Server{Server: server, verify: jobs}
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
	// The ticket's verdict ledger, in record order. Whether a row is current is
	// not answered here — that needs the head to judge against, which
	// ticket_verdict_current takes.
	Verdicts []ticket.VerdictRow `json:"verdicts,omitempty"`
	// The ticket's observer action receipts, in record order (see
	// ticket_apply_action and ticket_discover).
	Actions []ticket.ActionReceipt `json:"actions,omitempty"`
	Extra   map[string]string      `json:"-"`
	// ClosedChildren names the children an edit that abandoned an epic closed
	// along with it. Set by ticket_edit alone — every other tool leaves it
	// empty, and it is omitted from the response then.
	ClosedChildren []string `json:"closed_children,omitempty"`
	// UnregisteredWarning says the ticket landed in a project with a directory
	// in the store but no `store: central` entry in config. Set by ticket_create
	// with a `repo` argument alone, and the same sentence the CLI prints on
	// stderr — which is the server's log here, so a warning about where a write
	// went has to ride on the response the caller reads.
	UnregisteredWarning string `json:"unregistered_warning,omitempty"`
	// EmptyAcceptanceWarning says the ticket was created with a description but
	// no acceptance criteria. Set by ticket_create alone — every other tool
	// leaves it empty. /capture and /work both gate on a why-plus-success
	// contract, so a ticket in this state is one neither will accept, and
	// nothing else reports it; a warning rather than a refusal, so stub-first
	// flows still create.
	EmptyAcceptanceWarning string `json:"empty_acceptance_warning,omitempty"`
	// BareAcceptanceCriteria names the criteria that carry neither a `verify:`
	// nor an `unverifiable:` line, so a caller can machine-read which bullets
	// are the gap rather than parse the sentence below. Set by ticket_create
	// alone — every other tool leaves it empty.
	BareAcceptanceCriteria []string `json:"bare_acceptance_criteria,omitempty"`
	// BareAcceptanceWarning is the remedy for those criteria. Set by
	// ticket_create alone. The caller is the only party still holding the
	// context the criteria came from, so it is the one that can attach commands
	// cheaply; a warning rather than a refusal, so a batch filing path is not
	// left unable to record the ticket at all.
	BareAcceptanceWarning string `json:"bare_acceptance_warning,omitempty"`
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
		Verdicts:    t.Verdicts,
		Actions:     t.Actions,
	}

	j.Extra = t.Extra

	// Extract body sections.
	body := t.Body
	if body != "" {
		j.Description, j.Design, j.Acceptance, j.TestResults = ticket.BodySections(body)
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

// sourceDefault attributes a write by a client that declared no name in the
// handshake.
const sourceDefault = "mcp"

// sourceFor decides who a write tool's change is attributed to in the mutation
// log: the caller's own source argument, else the client name from the MCP
// handshake, else a bare "mcp". TK_SOURCE is not consulted here — the store
// applies it ahead of whatever this returns, so it wins on every surface.
func sourceFor(req *mcp.CallToolRequest, arg string) string {
	if s := strings.TrimSpace(arg); s != "" {
		return s
	}
	if req != nil && req.Session != nil {
		if params := req.Session.InitializeParams(); params != nil && params.ClientInfo != nil {
			if name := strings.TrimSpace(params.ClientInfo.Name); name != "" {
				return name
			}
		}
	}
	return sourceDefault
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

// envelopeFragmentResult refuses a free-text argument whose value ran past its
// own parameter and swallowed the rest of the tool call — the arguments after
// it were never populated, so storing this one would leave a ticket missing the
// fields the caller believed it sent. Returns nil when the value is clean.
func envelopeFragmentResult(field, value string) *mcp.CallToolResult {
	tail, ok := ticket.EnvelopeFragment(value)
	if !ok {
		return nil
	}
	r, _ := errResult("%s ends in a tool-call envelope fragment: %q. "+
		"The %s text ran past its own parameter and absorbed the remainder of the call, so every argument after it is missing. "+
		"Resend the call with %s closed by its own matching tag.", field, tail, field, field)
	return r
}

// --- Tool registrations ---

type listArgs struct {
	Status        string    `json:"status,omitempty" jsonschema:"filter by status: backlog, ready, open, done, closed"`
	Type          string    `json:"type,omitempty" jsonschema:"filter by type: bug, feature, epic"`
	Priority      *FlexInt  `json:"priority,omitempty" jsonschema:"filter by priority (0-4)"`
	Tag           string    `json:"tag,omitempty" jsonschema:"filter by tag"`
	Field         string    `json:"field,omitempty" jsonschema:"filter by extra field (key=value, substring match)"`
	Parent        string    `json:"parent,omitempty" jsonschema:"only the children of this epic, in every namespace: a qualified project/id, or a bare ID relative to project (or the default project)"`
	Project       string    `json:"project,omitempty" jsonschema:"narrow the rows to one project (multi-project mode); never changes what a parent or status resolves to"`
	AllProjects   *FlexBool `json:"all_projects,omitempty" jsonschema:"list every namespace, ignoring the server's default project; conflicts with project"`
	IncludeClosed *FlexBool `json:"include_closed,omitempty" jsonschema:"keep closed tickets in the rows (dropped by default unless status is set)"`
	Snapshot      string    `json:"snapshot,omitempty" jsonschema:"the snapshot token the first page returned; required on every page with offset > 0, and refused if the store has changed since, so pages of one listing never mix revisions"`
	Offset        *FlexInt  `json:"offset,omitempty" jsonschema:"number of results to skip (default 0); an offset above 0 requires snapshot"`
	Limit         *FlexInt  `json:"limit,omitempty" jsonschema:"max results to return (default 50, 0 for unlimited)"`
}

const defaultListLimit = 50

type listResultJSON struct {
	Tickets              []ticketSummaryJSON `json:"tickets"`
	Total                int                 `json:"total"`
	Offset               int                 `json:"offset"`
	Limit                int                 `json:"limit"`
	Snapshot             string              `json:"snapshot"`
	Complete             bool                `json:"complete"`
	Namespaces           []string            `json:"namespaces"`
	Parent               *parentJSON         `json:"parent,omitempty"`
	UnregisteredProjects []string            `json:"unregistered_projects,omitempty"`
	SkippedFiles         []fileSkipJSON      `json:"skipped_files,omitempty"`
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

// resolveProject returns the effective project: explicit arg > default > empty.
func resolveProject(explicit, defaultProject string) string {
	if explicit != "" {
		return explicit
	}
	return defaultProject
}

// fileSkipJSON is a skipped file as a tool reports it, with whether it left the
// epics in its project derived from a partial set of children. The flag is
// computed from the kind rather than stored, so it cannot disagree with what
// the derivation actually did.
type fileSkipJSON struct {
	ticket.FileSkip
	EpicStatusDegraded bool `json:"epic_status_degraded,omitempty"`
}

// snapshotter is a store that hands out the graph it answers with. Both
// stores the server is built over do; SnapshotOf covers any other by reading
// its listing as one namespace.
type snapshotter interface {
	Snapshot() (*ticket.Snapshot, error)
}

// storeSnapshot is the one reading of the store a listing tool works from.
// Everything the response carries — the rows, the skips, the revision token,
// whether the read was complete, an epic's counts — is derived from it, so a
// response never mixes two readings of the store. The store's own skip warning
// goes to stderr, which is discarded at both ends of the MCP transport, so a
// skip only reaches an agent by riding on the response.
func storeSnapshot(store ticket.Store) (*ticket.Snapshot, error) {
	if s, ok := store.(snapshotter); ok {
		return s.Snapshot()
	}
	return ticket.SnapshotOf(store)
}

// scopeProject is the namespace a listing tool narrows its rows to: the
// explicit project, else the server's default, else none. With all_projects
// the default is set aside — the directory `tk serve` started in is not a
// request — and an explicit project beside it is refused, since a call handed
// both cannot honour either. The scope narrows rows only: lookup, membership
// and status are answered off the whole store before it applies.
func scopeProject(explicit string, allProjects *FlexBool, defaultProject string) (string, *mcp.CallToolResult) {
	if allProjects != nil && bool(*allProjects) {
		if explicit != "" {
			r, _ := errResult("all_projects lists every namespace; drop project or all_projects")
			return "", r
		}
		return "", nil
	}
	return resolveProject(explicit, defaultProject), nil
}

// resolveParentArg turns a parent argument into the exact qualified ID the
// snapshot holds. A bare parent is relative to ns, the project the call is
// scoped to; on a namespaced store with no scope it names nothing — no other
// namespace is searched for a bare ID, since the central store holds identical
// bare IDs in different projects — so it is refused with the remedy rather
// than looked up. An unresolvable parent, or one that is not an epic, is a
// refusal and never an empty listing.
func resolveParentArg(store ticket.Store, snap *ticket.Snapshot, ns, parent string) (string, *mcp.CallToolResult) {
	if _, multi := store.(*ticket.MultiStore); multi && ns == "" {
		if p, _ := ticket.ParseNamespacedID(parent); p == "" {
			r, _ := errResult("parent %q is bare and no project scopes it: name the epic qualified as project/id", parent)
			return "", r
		}
	}
	id, err := snap.Resolve(ns, parent)
	if err != nil {
		r, _ := errResult("parent: %v", err)
		return "", r
	}
	if t, ok := snap.Get(id); ok && t.Type != ticket.TypeEpic {
		r, _ := errResult("parent %s is type %s, not an epic", id, t.Type)
		return "", r
	}
	return id, nil
}

// childrenOf keeps the tickets the snapshot places under the epic, by exact
// qualified ID across every namespace: a suffix two IDs happen to share is not
// membership.
func childrenOf(snap *ticket.Snapshot, parentID string, tickets []*ticket.Ticket) []*ticket.Ticket {
	member := map[string]bool{}
	for _, c := range snap.Children(parentID) {
		member[c.ID] = true
	}
	var kept []*ticket.Ticket
	for _, t := range tickets {
		if member[t.ID] {
			kept = append(kept, t)
		}
	}
	return kept
}

// snapshotChangedResult refuses a page asked for against a token the store has
// moved past. The caller's earlier pages were cut from a membership and a
// total this reading no longer has, so serving the page would mix two
// revisions into one listing; the new token is in the text so the caller can
// restart from offset 0 without another call. Nil when no token was passed or
// it still matches.
func snapshotChangedResult(token, revision string) *mcp.CallToolResult {
	if token == "" || token == revision {
		return nil
	}
	r, _ := errResult("snapshot changed: the store was modified since %s was issued; restart from offset 0 with snapshot %s", token, revision)
	return r
}

// statusCountsJSON is an epic's children counted by derived status.
type statusCountsJSON struct {
	Done    int `json:"done"`
	Closed  int `json:"closed"`
	Open    int `json:"open"`
	Ready   int `json:"ready"`
	Backlog int `json:"backlog"`
}

func countsJSON(p ticket.EpicProgress) statusCountsJSON {
	return statusCountsJSON{Done: p.Done, Closed: p.Closed, Open: p.Open, Ready: p.Ready, Backlog: p.Backlog}
}

// parentJSON is the epic a parent-scoped listing was cut from, as the whole
// graph holds it: children_total and counts cover every child in every
// namespace, whatever the project scope or the closed filter left out of the
// rows, so a consumer can tell a slice from the epic's whole. Status is the
// one derived off the same snapshot as the counts.
type parentJSON struct {
	ID            string           `json:"id"`
	Status        string           `json:"status"`
	Type          string           `json:"type"`
	Complete      bool             `json:"complete"`
	ChildrenTotal int              `json:"children_total"`
	Counts        statusCountsJSON `json:"counts"`
}

func parentOf(snap *ticket.Snapshot, parentID string) *parentJSON {
	p := snap.Progress(parentID)
	j := &parentJSON{ID: parentID, Complete: p.Complete, ChildrenTotal: p.Total, Counts: countsJSON(p)}
	if t, ok := snap.Get(parentID); ok {
		j.Status, j.Type = string(t.Status), string(t.Type)
	}
	return j
}

// snapshotDoc documents the fields every snapshot-backed listing carries, so
// the tools cannot describe them differently.
const snapshotDoc = " `snapshot` is the revision token of the store reading the response was cut from and `complete` says whether that reading saw the whole store." +
	" `all_projects=true` lists every namespace and ignores the server's default project; it conflicts with `project`, which only narrows the rows — lookup, membership and status are answered off the whole store first."

// parentDoc documents the parent argument shared by the tools that take it.
const parentDoc = " `parent` keeps only the children the graph places under that epic, in every namespace: a qualified project/id, or a bare ID relative to `project` (or the default project); with neither, a bare parent is refused. The response then carries `parent` — the epic's derived `status`, `complete`, `children_total` and `counts` by status over every child in every namespace, whatever `project` or the closed filter hid from the rows."

// skippedFilesJSON renders the skips a response carries. A skip that degrades
// the epics is kept whatever project the response is scoped to: the derivation
// demotes every epic in the store over it (FileSkipKind.DegradesEpicStatus),
// so a project view that dropped a foreign one would show a demoted epic with
// no cause in sight. Only a kind that degrades nothing — a file naming another
// project — says nothing about the tickets the caller can see, and is scoped
// to the project the response is. Returns nil when there is nothing to report,
// so a healthy store's response carries no field.
func skippedFilesJSON(skips []ticket.FileSkip, project string) []fileSkipJSON {
	var out []fileSkipJSON
	for _, s := range skips {
		degraded := s.Kind.DegradesEpicStatus()
		if project != "" && s.Project != project && !degraded {
			continue
		}
		out = append(out, fileSkipJSON{FileSkip: s, EpicStatusDegraded: degraded})
	}
	return out
}

// skippedFilesDoc documents the skip fields in the tool descriptions that carry
// them, so the three cannot describe the same field differently.
const skippedFilesDoc = " `skipped_files` names every file in the projects read that was not read as a ticket, with the reason. " +
	"An entry with `epic_status_degraded` is a ticket the store cannot place — a file that could not be read, a `duplicate-id` two files claim, or a `namespace` that could not be read — and it could be any epic's child in any project, so while it stands anywhere in the store, no epic anywhere reads done or closed, whatever its children say; a project view carries such an entry from another project for that reason."

func registerList(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_list",
		Description: "List tickets with optional filters and pagination. Returns non-closed tickets by default; `include_closed=true` keeps them, and `status` selects exactly one. Default limit is 50; use offset/limit to paginate, and pass the `snapshot` token from the first page back on every later page — a page at offset > 0 without it is refused, and a store that changed in between is refused with the new token rather than mixing two revisions into one total." + snapshotDoc + " `namespaces` names the namespaces read in full." + parentDoc + " `unregistered_projects` names any project in the result set with a directory in the store but no `store: central` entry in config, so no repo is registered to it — run `tk init` in that project's repo to register it." + skippedFilesDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
		effectiveProject, r := scopeProject(args.Project, args.AllProjects, defaultProject)
		if r != nil {
			return r, nil, nil
		}
		snap, err := storeSnapshot(store)
		if err != nil {
			r, _ := errResult("failed to list tickets: %v", err)
			return r, nil, nil
		}
		revision := snap.Revision()
		if r := snapshotChangedResult(args.Snapshot, revision); r != nil {
			return r, nil, nil
		}

		// Membership before every filter: the graph places a child under its
		// epic across namespaces, and the scope and status filters below only
		// narrow what the membership already decided.
		tickets := snap.Tickets
		var parent *parentJSON
		if args.Parent != "" {
			parentID, r := resolveParentArg(store, snap, effectiveProject, args.Parent)
			if r != nil {
				return r, nil, nil
			}
			tickets = childrenOf(snap, parentID, tickets)
			parent = parentOf(snap, parentID)
		}

		opts := ticket.DefaultListOptions()
		if args.Status != "" {
			opts.Status = ticket.Status(args.Status)
		} else if args.IncludeClosed == nil || !bool(*args.IncludeClosed) {
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
		if effectiveProject != "" {
			tickets = filterByProject(tickets, effectiveProject)
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
		// The token is what ties a later page to the reading the first page was
		// cut from; without one there is nothing to compare the store against,
		// and the page would be served off whatever revision is current.
		if offset > 0 && args.Snapshot == "" {
			r, _ := errResult("page at offset %d requires the snapshot token the first page returned; restart from offset 0", offset)
			return r, nil, nil
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

		r, err = jsonResult(listResultJSON{
			Tickets:              items,
			Total:                total,
			Offset:               offset,
			Limit:                limit,
			Snapshot:             revision,
			Complete:             snap.Complete,
			Namespaces:           nonNil(snap.Namespaces()),
			Parent:               parent,
			UnregisteredProjects: unregistered,
			// Reported over the whole store's skips, not the page: the file was
			// never a row here — it is why a row may be missing — so paging it
			// away would hide it exactly when the caller reads a short page.
			SkippedFiles: skippedFilesJSON(snap.Skips, effectiveProject),
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
// conscious caller can tell whether there are more notes to fetch, and with
// what the graph says about the ticket beyond its own file.
type showResultJSON struct {
	ticketJSON
	showExtras
}

// showExtras is everything ticket_show reports that is not the ticket's own
// serialization. Namespace is the ticket's namespace half, "" on a single
// store. An epic carries epicJSON; a leaf whose parent does not make it a
// child carries the reason.
type showExtras struct {
	NotesTotal int    `json:"notes_total"`
	NotesShown int    `json:"notes_shown"`
	Namespace  string `json:"namespace"`
	// Precondition is the opaque token ticket_apply_action holds a write
	// against: it identifies the state this response was read from, and an
	// action carrying it is refused once the ticket has changed.
	Precondition string `json:"precondition"`
	*epicJSON
	RelationshipIssue string `json:"relationship_issue,omitempty"`
}

// epicJSON is an epic as the whole graph holds it: its children in every
// namespace under their qualified IDs, and the counts its status was derived
// from, off the same snapshot, so the two agree. Complete says whether that
// snapshot saw the whole store; while it did not, Diagnostics names what it
// could not read and the epic reads neither done nor closed.
type epicJSON struct {
	Children      []childJSON      `json:"children"`
	ChildrenTotal int              `json:"children_total"`
	Counts        statusCountsJSON `json:"counts"`
	Complete      bool             `json:"complete"`
	Diagnostics   []string         `json:"diagnostics,omitempty"`
}

type childJSON struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Type      string `json:"type"`
	Namespace string `json:"namespace"`
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
	extras, err := json.Marshal(s.showExtras)
	if err != nil {
		return nil, err
	}
	var e map[string]json.RawMessage
	if err := json.Unmarshal(extras, &e); err != nil {
		return nil, err
	}
	for k, v := range e {
		m[k] = v
	}
	return json.Marshal(m)
}

func registerShow(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_show",
		Description: "Show full details of a ticket by ID. Notes are trimmed to the newest 20 by default; use notes_limit=0 for all, metadata_only=true for none, or notes_offset to page further back. `namespace` is the ticket's project (empty on a single store). An epic also carries `children` (id, title, status, type, namespace — every child in every namespace, IDs qualified), `children_total`, `counts` by status, and `complete`, all off one reading of the store so the derived status and the counts agree; while `complete` is false `diagnostics` names what could not be read and the epic reads neither done nor closed. A leaf whose parent does not make it a child carries `relationship_issue` saying why. `precondition` is the opaque token to pass to ticket_apply_action so the action lands only on the state shown here. An id whose file exists but cannot be read as a ticket is reported as `ticket unreadable`, naming the file — the ticket is there and the file needs repair, which is not the same as `ticket not found`.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args showArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			// A file that resolved and yielded no ticket is a repair, not a
			// missing ticket: reported as "not found" a caller would create the
			// ticket a second time and leave the corrupt file where it is.
			var unreadable *ticket.UnreadableTicketError
			if errors.As(err, &unreadable) {
				r, _ := errResult("ticket unreadable: %v", err)
				return r, nil, nil
			}
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}

		ns, _ := ticket.ParseNamespacedID(t.ID)
		extras := showExtras{Namespace: ns, Precondition: t.Precondition(), RelationshipIssue: ticket.RelationshipIssue(t)}
		// The graph is read only when the response needs it — an epic's
		// children and counts, or a leaf's issue as the snapshot stamps it —
		// so a plain leaf costs one file read.
		if t.Type == ticket.TypeEpic || extras.RelationshipIssue != "" {
			snap, err := storeSnapshot(store)
			if err != nil {
				r, _ := errResult("failed to read the store: %v", err)
				return r, nil, nil
			}
			// Get derived the ticket off its own reading; the status and the
			// issue reported here come off this one, so they cannot disagree
			// with the children and counts beside them.
			if cur, ok := snap.Get(t.ID); ok {
				t.Status = cur.Status
				extras.RelationshipIssue = ticket.RelationshipIssue(cur)
			}
			if t.Type == ticket.TypeEpic {
				p := snap.Progress(t.ID)
				epic := &epicJSON{Children: []childJSON{}, ChildrenTotal: p.Total, Counts: countsJSON(p), Complete: p.Complete, Diagnostics: p.Diagnostics}
				for _, c := range snap.Children(t.ID) {
					cns, _ := ticket.ParseNamespacedID(c.ID)
					epic.Children = append(epic.Children, childJSON{ID: c.ID, Title: c.Title, Status: string(c.Status), Type: string(c.Type), Namespace: cns})
				}
				extras.epicJSON = epic
			}
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

		extras.NotesTotal, extras.NotesShown = total, len(t.Notes)
		r, err := jsonResult(showResultJSON{ticketJSON: toJSON(t), showExtras: extras})
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
	Parent      string            `json:"parent,omitempty" jsonschema:"parent epic ID: an epic in the same project, or a qualified project/id naming an epic in another namespace once the catalog requires cross-project-parents"`
	Tags        string            `json:"tags,omitempty" jsonschema:"comma-separated tags"`
	ExternalRef string            `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string            `json:"branch,omitempty" jsonschema:"git branch name"`
	Project     string            `json:"project,omitempty" jsonschema:"destination namespace in multi-project mode (namespaces the ticket ID): a registered project, or _root for an idea with no repository yet"`
	Repo        string            `json:"repo,omitempty" jsonschema:"registered project name or path to repo root"`
	Set         map[string]string `json:"set,omitempty" jsonschema:"set extra fields (key: value)"`
	Source      string            `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
}

func registerCreate(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_create",
		Description: "Create a new ticket. In multi-project mode the destination is `project` (a registered project, or `_root` for an idea with no repository yet — refused until the catalog requires root-namespace), `repo` (a registered project name or repo path, for cross-repo creation), or the server's default project; with none of the three the create is refused rather than landing somewhere inferred. Passing `repo` together with a `project` naming a different project is refused rather than one silently winning; the CWD-derived default project never conflicts. `parent` may name an epic in another namespace, qualified as project/id, once the catalog requires cross-project-parents. `unregistered_warning` is set when that repo's project has a directory in the store but no `store: central` entry in config, so no repo is registered to it — run `tk init` in that repo to register it. `empty_acceptance_warning` is set when a description was given with no acceptance criteria. `bare_acceptance_criteria` and `bare_acceptance_warning` are set when an acceptance criterion carries neither a `verify: <command>` line nor an `unverifiable: <reason>` line — re-send those criteria with one of the two attached. A description, design or acceptance value that ends in a tool-call envelope fragment is refused rather than stored.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createArgs) (*mcp.CallToolResult, any, error) {
		content := ticketContent{
			Title:       args.Title,
			Description: args.Description,
			Design:      args.Design,
			Acceptance:  args.Acceptance,
			Type:        args.Type,
			Priority:    args.Priority,
			Parent:      args.Parent,
			Tags:        args.Tags,
		}
		if r := content.refusal(); r != nil {
			return r, nil, nil
		}

		dst, r := resolveDestination(store, sourceFor(req, args.Source), args.Repo, args.Project)
		if r != nil {
			return r, nil, nil
		}

		t := content.ticket()
		if args.ExternalRef != "" {
			t.ExternalRef = args.ExternalRef
		}
		if args.Branch != "" {
			t.Branch = args.Branch
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

		if r := dst.qualify(t, store, args.Project, defaultProject); r != nil {
			return r, nil, nil
		}

		if err := dst.store.Create(t); err != nil {
			r, _ := errResult("failed to create ticket: %v", dst.frame(err))
			return r, nil, nil
		}
		dst.settle(t)

		j := toJSON(t)
		j.UnregisteredWarning = dst.unregisteredWarning
		content.warn(&j, t)
		r, err := jsonResult(j)
		return r, nil, err
	})
}

// ticketContent is the content of a new ticket as ticket_create and
// ticket_discover both take it: the fields the ticket is built from, and — for
// a discovery — the fields its finding key is digested over.
type ticketContent struct {
	Title       string
	Description string
	Design      string
	Acceptance  string
	Type        string
	Priority    *FlexInt
	Parent      string
	Tags        string
}

// refusal is the check every new ticket's content passes before a store is
// touched: a title, and no body field ending in a tool-call envelope
// fragment. Nil when the content is clean.
func (c ticketContent) refusal() *mcp.CallToolResult {
	if c.Title == "" {
		r, _ := errResult("title is required")
		return r
	}
	for _, f := range []struct{ name, value string }{
		{"description", c.Description},
		{"design", c.Design},
		{"acceptance", c.Acceptance},
	} {
		if r := envelopeFragmentResult(f.name, f.value); r != nil {
			return r
		}
	}
	return nil
}

// ticket builds the new ticket, its ID generated from the title and bare
// until the destination qualifies it.
func (c ticketContent) ticket() *ticket.Ticket {
	t := &ticket.Ticket{
		ID:       ticket.GenerateID(c.Title),
		Title:    c.Title,
		Status:   ticket.StatusBacklog,
		Priority: 2,
		Created:  time.Now().UTC(),
	}

	if c.Type != "" {
		t.Type = ticket.TicketType(c.Type)
	} else {
		t.Type = ticket.TypeFeature
	}
	if c.Priority != nil {
		t.Priority = int(*c.Priority)
	}
	if c.Parent != "" {
		t.Parent = c.Parent
	}
	if c.Tags != "" {
		t.Tags = strings.Split(c.Tags, ",")
		for i := range t.Tags {
			t.Tags[i] = strings.TrimSpace(t.Tags[i])
		}
	}

	// Build body.
	var body strings.Builder
	if c.Description != "" {
		body.WriteString(c.Description + "\n")
	}
	if c.Design != "" {
		body.WriteString("\n## Design\n\n" + c.Design + "\n")
	}
	if c.Acceptance != "" {
		body.WriteString("\n## Acceptance Criteria\n\n" + c.Acceptance + "\n")
	}
	t.Body = body.String()
	return t
}

// warn stamps the acceptance warnings a created ticket's response carries.
func (c ticketContent) warn(j *ticketJSON, t *ticket.Ticket) {
	// Epics are exempt on the same grounds tk audit exempts them: a container
	// holds children that each carry their own contract.
	// Compared trimmed, because the audit classifies the same ticket off
	// BodySections' trimmed output: an acceptance of " " is stored as none,
	// so it has to warn here too or the two surfaces disagree.
	if t.Type != ticket.TypeEpic && strings.TrimSpace(c.Description) != "" && strings.TrimSpace(c.Acceptance) == "" {
		j.EmptyAcceptanceWarning = fmt.Sprintf("ticket %s has a description but no acceptance criteria: nothing states what done means, and the workflow gates on that contract. "+
			"Add it with ticket_edit on %s and an `acceptance` argument.", t.ID, t.ID)
	}
	// Read off the stored body, not c.Acceptance, so the CLI — which has no
	// acceptance argument, only a description carrying the section — reaches
	// the same helper. No type exemption: unlike an empty contract, which is
	// expected on a container, a criterion that was written but cannot be
	// checked is a gap whatever the type.
	j.BareAcceptanceCriteria = ticket.BareCriteria(t.Body)
	j.BareAcceptanceWarning = ticket.BareAcceptanceWarning(t.ID, j.BareAcceptanceCriteria)
}

// destination is where a new ticket lands, resolved from the `repo` and
// `project` arguments ticket_create and ticket_discover share.
type destination struct {
	// store is the store the write goes through, attributed to the caller.
	store ticket.Store
	// repoProject is the project the repo argument resolved to, empty when no
	// repo was given. It stands in for the project argument on that path: the
	// repo names the project itself, so nothing else gets to decide the
	// namespace.
	repoProject         string
	unregisteredWarning string
}

// resolveDestination resolves the store a new ticket is written to. With a
// repo argument, that repo's store; otherwise the server's own, and qualify
// decides the namespace. A refusal comes back as the tool result to return.
func resolveDestination(store ticket.Store, source, repoArg, projectArg string) (*destination, *mcp.CallToolResult) {
	dst := &destination{store: ticket.WithSource(store, source)}
	if repoArg == "" {
		return dst, nil
	}
	repo := repoArg
	cfg, err := project.Load()
	if err != nil {
		r, _ := errResult("load ticket config: %v", err)
		return nil, r
	}
	path, configured := project.ConfiguredRepoPath(cfg, repo)
	if configured {
		repo = path
	} else {
		abs, err := filepath.Abs(repo)
		if err != nil {
			r, _ := errResult("invalid repo path: %v", err)
			return nil, r
		}
		info, err := os.Stat(abs)
		if err != nil {
			r, _ := errResult("repo %q is neither a registered project name nor a directory: %v", repoArg, err)
			return nil, r
		}
		if !info.IsDir() {
			r, _ := errResult("repo %q is neither a registered project name nor a directory", repoArg)
			return nil, r
		}
		repo = abs
	}
	// The resolution the CLI's own store and `tk move`'s destination read
	// through, so a repo resolves to one store and one error whichever
	// surface names it — including the project-name bound that keeps a
	// config key crafted on another machine from steering this write
	// outside the central root.
	resolved, unregistered, err := ticket.ResolveStoreForRepo(repo)
	if err != nil {
		r, _ := errResult("%v", err)
		return nil, r
	}
	if unregistered {
		// Carried on the response rather than written to stderr, which is
		// the server's log and not something the calling agent reads: the
		// project is one MultiStore.Create refuses to write to, so a create
		// into it puts a ticket where nothing else will.
		dst.unregisteredWarning = ticket.UnregisteredWarning(resolved)
	}
	// Only an explicitly passed project conflicts: the CWD-derived
	// defaultProject is not a request for a destination, and a server
	// started in one repo is exactly the case repo exists to escape.
	if projectArg != "" && projectArg != resolved.Project {
		r, _ := errResult("repo %q resolves to project %q but project %q was also given — pass one or the other", repoArg, resolved.Project, projectArg)
		return nil, r
	}
	dst.repoProject = resolved.Project
	dst.store = ticket.WithSource(resolved, source)
	return dst, nil
}

// qualify namespaces the new ticket's ID for the server's own store. Not on
// the repo path: that one writes through a project FileStore, which takes
// bare IDs and refuses one carrying a separator, so settle namespaces on the
// way out instead. A central store with no destination named is refused here
// with the remedy, rather than by the store's own ID-format error: nothing
// infers where a write lands.
func (d *destination) qualify(t *ticket.Ticket, store ticket.Store, projectArg, defaultProject string) *mcp.CallToolResult {
	if d.repoProject != "" {
		return nil
	}
	proj := resolveProject(projectArg, defaultProject)
	if _, multi := store.(*ticket.MultiStore); multi && proj == "" {
		r, _ := errResult("no destination: pass project (a registered project, or %s for an idea with no repository yet) or repo", project.RootNamespace)
		return r
	}
	if proj != "" {
		t.ID = ticket.FormatNamespacedID(proj, t.ID)
	}
	return nil
}

// frame names the destination in a write's failure. A project FileStore
// names a bare ID and not the project that refused it, so the failure is
// framed the way MultiStore.Create frames its own — a cross-repo create names
// the same destination whichever argument chose it.
func (d *destination) frame(err error) error {
	if d.repoProject != "" {
		return fmt.Errorf("project %s: %w", d.repoProject, err)
	}
	return err
}

// settle prefixes the ID a repo-path write settled on. The ID went in bare,
// and only now is it final: a hash collision makes Create regenerate it, and
// the one it kept is the one to prefix. Everything downstream — the JSON, and
// the empty_acceptance_warning naming an ID for ticket_edit — reads t.ID.
func (d *destination) settle(t *ticket.Ticket) {
	if d.repoProject != "" {
		t.ID = ticket.FormatNamespacedID(d.repoProject, t.ID)
	}
}

type editArgs struct {
	ID          string            `json:"id" jsonschema:"ticket ID"`
	Title       string            `json:"title,omitempty" jsonschema:"new title"`
	Status      string            `json:"status,omitempty" jsonschema:"status: backlog, ready, open, done, closed. An epic's status is derived from its children; the only one that can be set on an epic is closed, which closes its children too"`
	Type        string            `json:"type,omitempty" jsonschema:"new type"`
	Priority    *FlexInt          `json:"priority,omitempty" jsonschema:"new priority (0-4)"`
	Parent      *string           `json:"parent,omitempty" jsonschema:"new parent epic ID: an epic in the same project, or a qualified project/id naming an epic in another namespace once the catalog requires cross-project-parents. Pass an empty string to clear it"`
	Tags        string            `json:"tags,omitempty" jsonschema:"comma-separated tags (replaces existing)"`
	ExternalRef string            `json:"external_ref,omitempty" jsonschema:"external reference"`
	Branch      string            `json:"branch,omitempty" jsonschema:"git branch name"`
	Description string            `json:"description,omitempty" jsonschema:"new description text"`
	Design      string            `json:"design,omitempty" jsonschema:"new design text"`
	Acceptance  string            `json:"acceptance,omitempty" jsonschema:"new acceptance criteria"`
	TestResults string            `json:"test_results,omitempty" jsonschema:"test results to record"`
	Set         map[string]string `json:"set,omitempty" jsonschema:"set extra fields (key: value to set, key: empty string to remove)"`
	Outputs     map[string]string `json:"outputs,omitempty" jsonschema:"set outputs the ticket produced, e.g. branch/commit/artifacts (key: value to set, key: empty string to remove)"`
	Source      string            `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
}

func registerEdit(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_edit",
		Description: "Edit an existing ticket's fields. Closing an epic closes its children too; the response names them in `closed_children`. A body field that ends in a tool-call envelope fragment is refused rather than stored.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args editArgs) (*mcp.CallToolResult, any, error) {
		for _, f := range []struct{ name, value string }{
			{"description", args.Description},
			{"design", args.Design},
			{"acceptance", args.Acceptance},
			{"test_results", args.TestResults},
		} {
			if r := envelopeFragmentResult(f.name, f.value); r != nil {
				return r, nil, nil
			}
		}

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
		closed, err := ticket.SaveEdit(ticket.WithSource(store, sourceFor(req, args.Source)), t, args.Status != "")
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
	ID     string `json:"id" jsonschema:"ticket ID (supports partial matching)"`
	Source string `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
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

		if err := ticket.WithSource(store, sourceFor(req, args.Source)).Delete(t.ID); err != nil {
			r, _ := errResult("failed to delete ticket: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(map[string]string{"deleted": t.ID})
		return r, nil, err
	})
}

type addNoteArgs struct {
	ID     string `json:"id" jsonschema:"ticket ID"`
	Text   string `json:"text" jsonschema:"note text to append"`
	Source string `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
}

func registerAddNote(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_add_note",
		Description: "Append a timestamped note to a ticket.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args addNoteArgs) (*mcp.CallToolResult, any, error) {
		// Through Mutate: the note is appended to whatever the ticket already
		// holds, so agents noting the same ticket at once do not overwrite one
		// another's notes.
		t, err := ticket.Mutate(ticket.WithSource(store, sourceFor(req, args.Source)), args.ID, func(t *ticket.Ticket) error {
			t.Notes = append(t.Notes, ticket.Note{
				Timestamp: time.Now().UTC(),
				Text:      args.Text,
			})
			return nil
		})
		if err != nil {
			r, _ := errResult("failed to add note: %v", err)
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
	Source string `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
}

func registerDep(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_dep",
		Description: "Add or remove a dependency. The ticket (id) depends on dep_id. On add, cargo optionally names what concretely flows across the edge.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args depArgs) (*mcp.CallToolResult, any, error) {
		dep, err := store.Get(args.DepID)
		if err != nil {
			r, _ := errResult("dep ticket not found: %v", err)
			return r, nil, nil
		}
		if args.Action != "add" && args.Action != "remove" {
			r, _ := errResult("action must be 'add' or 'remove'")
			return r, nil, nil
		}

		// Through Mutate: the edge is added to the deps the ticket already
		// holds, so a concurrent writer's edge is not dropped.
		t, err := ticket.Mutate(ticket.WithSource(store, sourceFor(req, args.Source)), args.ID, func(t *ticket.Ticket) error {
			if args.Action == "remove" {
				ticket.RemoveDep(t, dep.ID)
				return nil
			}
			if err := ticket.AddDep(t, dep.ID); err != nil {
				return err
			}
			if strings.TrimSpace(args.Cargo) != "" {
				if err := ticket.SetDepCargo(t, dep.ID, args.Cargo); err != nil {
					return fmt.Errorf("invalid cargo: %w", err)
				}
			}
			return nil
		})
		if err != nil {
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
	Source   string `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
}

func registerLink(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_link",
		Description: "Add or remove a symmetric link between two tickets.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args linkArgs) (*mcp.CallToolResult, any, error) {
		target, err := store.Get(args.TargetID)
		if err != nil {
			r, _ := errResult("target ticket not found: %v", err)
			return r, nil, nil
		}
		if args.Action != "add" && args.Action != "remove" {
			r, _ := errResult("action must be 'add' or 'remove'")
			return r, nil, nil
		}

		// One side at a time, each under its own lock: the links are appended to
		// what each ticket already holds, and holding both locks at once would
		// deadlock two calls naming the same pair in opposite orders. The pair
		// was not written atomically before this either.
		//
		// AddLink and RemoveLink are symmetric, but the far side each call
		// passes is a copy that is never written — that side is written by the
		// other call, against its own current file.
		st := ticket.WithSource(store, sourceFor(req, args.Source))
		t, err := ticket.Mutate(st, args.ID, func(t *ticket.Ticket) error {
			if args.Action == "add" {
				ticket.AddLink(t, target)
			} else {
				ticket.RemoveLink(t, target)
			}
			return nil
		})
		if err != nil {
			r, _ := errResult("failed to update ticket: %v", err)
			return r, nil, nil
		}
		if _, err := ticket.Mutate(st, target.ID, func(cur *ticket.Ticket) error {
			if args.Action == "add" {
				ticket.AddLink(t, cur)
			} else {
				ticket.RemoveLink(t, cur)
			}
			return nil
		}); err != nil {
			r, _ := errResult("failed to update target ticket: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(toJSON(t))
		return r, nil, err
	})
}

type readyArgs struct {
	Tag         string    `json:"tag,omitempty" jsonschema:"filter by tag"`
	Project     string    `json:"project,omitempty" jsonschema:"narrow the rows to one project (multi-project mode); never changes what is ready or blocked"`
	AllProjects *FlexBool `json:"all_projects,omitempty" jsonschema:"list every namespace, ignoring the server's default project; conflicts with project"`
}

// allProjectsDoc documents the scope arguments of the tools that narrow rows
// but carry no snapshot fields.
const allProjectsDoc = " `all_projects=true` lists every namespace and ignores the server's default project; it conflicts with `project`, which only narrows the rows — readiness, blocking and every relationship are answered off the whole store first."

func registerReady(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_ready",
		Description: "List tickets that can be picked up: status open, ready or backlog, with all deps resolved and no terminal parent epic. Epics are never listed — an epic is a container, not a work item. Ordered open first, then ready, then backlog; priority then ID within each group. A backlog ticket is reachable but ungroomed — check it carries a why and success criteria before starting it." + allProjectsDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readyArgs) (*mcp.CallToolResult, any, error) {
		proj, r := scopeProject(args.Project, args.AllProjects, defaultProject)
		if r != nil {
			return r, nil, nil
		}
		ready, err := ticket.ReadyTickets(store)
		if err != nil {
			r, _ := errResult("failed to get ready tickets: %v", err)
			return r, nil, nil
		}

		// An epic is a container, not a work item: it is never picked up
		// directly, whatever status it derives from its children.
		var work []*ticket.Ticket
		for _, t := range ready {
			if t.Type != ticket.TypeEpic {
				work = append(work, t)
			}
		}
		ready = work

		opts := ticket.DefaultListOptions()
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		ready = ticket.Filter(ready, opts)
		if proj != "" {
			ready = filterByProject(ready, proj)
		}
		ticket.SortByStatusPriorityID(ready)

		result := []ticketSummaryJSON{}
		for _, t := range ready {
			result = append(result, toSummaryJSON(t))
		}

		r, err = jsonResult(result)
		return r, nil, err
	})
}

type frontierArgs struct {
	readyArgs
	Parent string `json:"parent,omitempty" jsonschema:"only the children of this epic, in every namespace: a qualified project/id, or a bare ID relative to project (or the default project)"`
}

// frontierResultJSON wraps the frontier so the response can also say which
// files the listing it was computed from did not read. A bare array had nowhere
// to put that, and a frontier is exactly the answer a skipped file shortens.
type frontierResultJSON struct {
	Tickets      []ticketSummaryJSON `json:"tickets"`
	Snapshot     string              `json:"snapshot"`
	Complete     bool                `json:"complete"`
	Parent       *parentJSON         `json:"parent,omitempty"`
	SkippedFiles []fileSkipJSON      `json:"skipped_files,omitempty"`
}

func registerFrontier(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_frontier",
		Description: "List the schedulable frontier: tickets with status ready whose dependencies are all done or closed. The parallel-safe set to start next. Returns an object with `tickets`, computed over the whole store before `parent` and `project` narrow it." + snapshotDoc + parentDoc + skippedFilesDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args frontierArgs) (*mcp.CallToolResult, any, error) {
		effectiveProject, r := scopeProject(args.Project, args.AllProjects, defaultProject)
		if r != nil {
			return r, nil, nil
		}
		snap, err := storeSnapshot(store)
		if err != nil {
			r, _ := errResult("failed to get frontier tickets: %v", err)
			return r, nil, nil
		}
		frontier := ticket.FrontierOf(store, snap.Tickets)

		var parent *parentJSON
		if args.Parent != "" {
			parentID, r := resolveParentArg(store, snap, effectiveProject, args.Parent)
			if r != nil {
				return r, nil, nil
			}
			frontier = childrenOf(snap, parentID, frontier)
			parent = parentOf(snap, parentID)
		}

		opts := ticket.DefaultListOptions()
		if args.Tag != "" {
			opts.Tag = args.Tag
		}
		frontier = ticket.Filter(frontier, opts)
		if effectiveProject != "" {
			frontier = filterByProject(frontier, effectiveProject)
		}
		ticket.SortByPriorityID(frontier)

		result := []ticketSummaryJSON{}
		for _, t := range frontier {
			result = append(result, toSummaryJSON(t))
		}

		r, err = jsonResult(frontierResultJSON{
			Tickets:      result,
			Snapshot:     snap.Revision(),
			Complete:     snap.Complete,
			Parent:       parent,
			SkippedFiles: skippedFilesJSON(snap.Skips, effectiveProject),
		})
		return r, nil, err
	})
}

func registerBlocked(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_blocked",
		Description: "List tickets that are blocked by unresolved dependencies." + allProjectsDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readyArgs) (*mcp.CallToolResult, any, error) {
		proj, r := scopeProject(args.Project, args.AllProjects, defaultProject)
		if r != nil {
			return r, nil, nil
		}
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
		if proj != "" {
			blocked = filterByProject(blocked, proj)
		}
		ticket.SortByPriorityID(blocked)

		result := []ticketSummaryJSON{}
		for _, t := range blocked {
			result = append(result, toSummaryJSON(t))
		}

		r, err = jsonResult(result)
		return r, nil, err
	})
}

type emptyArgs struct{}

type inboxArgs struct {
	Project     string    `json:"project,omitempty" jsonschema:"narrow the rows to one project (multi-project mode)"`
	AllProjects *FlexBool `json:"all_projects,omitempty" jsonschema:"list every namespace, ignoring the server's default project; conflicts with project"`
}

func registerInbox(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_inbox",
		Description: "Show tickets needing human attention, sorted by priority then age. An action of blocked means unresolved dependencies (named in detail) or a parked question (which takes precedence in detail)." + allProjectsDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args inboxArgs) (*mcp.CallToolResult, any, error) {
		effectiveProject, r := scopeProject(args.Project, args.AllProjects, defaultProject)
		if r != nil {
			return r, nil, nil
		}
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
	Query       string    `json:"query" jsonschema:"search query; ranked by relevance across title, body, and notes"`
	Project     string    `json:"project,omitempty" jsonschema:"narrow the matches to one project (multi-project mode)"`
	AllProjects *FlexBool `json:"all_projects,omitempty" jsonschema:"search every namespace, ignoring the server's default project; conflicts with project"`
	Limit       *FlexInt  `json:"limit,omitempty" jsonschema:"max results to return (default 50, 0 for unlimited)"`
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
	Matches      []searchMatchJSON `json:"matches"`
	Total        int               `json:"total"`
	Snapshot     string            `json:"snapshot"`
	Complete     bool              `json:"complete"`
	SkippedFiles []fileSkipJSON    `json:"skipped_files,omitempty"`
}

func registerSearch(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_search",
		Description: "Search tickets by relevance across title, body, and notes. Use before creating a ticket to find similar or duplicate tickets. Returns matches ranked best-first, each with the match_field and a context snippet around the matched term. `snapshot` is the revision token of the store reading the matches came from and `complete` says whether that reading saw the whole store." + allProjectsDoc + skippedFilesDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		effectiveProject, r := scopeProject(args.Project, args.AllProjects, defaultProject)
		if r != nil {
			return r, nil, nil
		}
		snap, err := storeSnapshot(store)
		if err != nil {
			r, _ := errResult("failed to list tickets: %v", err)
			return r, nil, nil
		}

		tickets := snap.Tickets
		if effectiveProject != "" {
			tickets = filterByProject(tickets, effectiveProject)
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

		r, err = jsonResult(searchResultJSON{
			Matches:      items,
			Total:        total,
			Snapshot:     snap.Revision(),
			Complete:     snap.Complete,
			SkippedFiles: skippedFilesJSON(snap.Skips, effectiveProject),
		})
		return r, nil, err
	})
}

type verifyArgs struct {
	ID string `json:"id" jsonschema:"ticket ID (supports partial matching)"`
	// Dir is a pointer so an omitted dir and an explicit empty string stay
	// distinguishable: the empty case is refused, as `tk verify --dir ""` is,
	// rather than read as the default.
	Dir          *string `json:"dir,omitempty" jsonschema:"directory to run the commands in: the project's configured checkout or a linked git worktree of it; anything else is refused. Omit to run in the configured checkout"`
	AcceptanceID string  `json:"acceptance_id,omitempty" jsonschema:"acceptance_id returned by ticket_criteria; the run is refused before anything executes if the ticket's criteria no longer match it"`
	Candidate    string  `json:"candidate,omitempty" jsonschema:"your name for what is under test (a commit, a worktree, a run ID), recorded with the result as provenance, not as approval"`
}

type criteriaArgs struct {
	ID string `json:"id" jsonschema:"ticket ID (supports partial matching)"`
}

type criterionJSON struct {
	Index              int    `json:"index"`
	Text               string `json:"text"`
	Command            string `json:"command,omitempty"`
	Unverifiable       bool   `json:"unverifiable"`
	UnverifiableReason string `json:"unverifiable_reason,omitempty"`
}

type criteriaJSON struct {
	ID           string          `json:"id"`
	AcceptanceID string          `json:"acceptance_id"`
	Criteria     []criterionJSON `json:"criteria"`
}

func registerVerify(server *mcp.Server, store ticket.Store, defaultProject string) *verifyJobs {
	jobs := newVerifyJobs()
	registerVerifyLifecycle(server, store, jobs)
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_criteria",
		Description: "Return a ticket's acceptance criteria exactly as ticket_verify parses them — index (1-based, the same index tk verify --criterion uses), text, verify command if any, and whether it is declared unverifiable and why — with acceptance_id, the identity of that contract. Pass acceptance_id back to ticket_verify to have the run refused if the criteria change in between. Runs nothing. A ticket with no criteria returns an empty list, not an error.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args criteriaArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}
		criteria := ticket.ParseCriteria(ticket.AcceptanceCriteria(t.Body))
		out := criteriaJSON{ID: t.ID, AcceptanceID: ticket.CriteriaIdentity(criteria), Criteria: []criterionJSON{}}
		for i, c := range criteria {
			out.Criteria = append(out.Criteria, criterionJSON{
				Index:              i + 1,
				Text:               ticket.SanitizeControl(c.Text),
				Command:            ticket.SanitizeControl(c.Command),
				Unverifiable:       c.Unverifiable,
				UnverifiableReason: ticket.SanitizeControl(c.UnverifiableReason),
			})
		}
		r, err := jsonResult(out)
		return r, nil, err
	})
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_verify",
		Description: "Run the verify commands declared in a ticket's acceptance criteria (\"verify: <command>\" lines) and record the results on the ticket. Commands execute on the server host in the checkout registered on this machine for the ticket's own project, or, with dir, in a linked git worktree of that checkout; a Root ticket, a project with no checkout registered here, a registered checkout that is missing, and a dir that is not the configured checkout or a worktree root sharing its repository (a subdirectory, an unrelated or nested repository, a non-git directory, a missing path, an empty string, or a symlink resolving to any of those) are each refused before anything runs, naming the reason, and nothing is recorded. Pass acceptance_id from ticket_criteria to have the run refused before anything executes if the criteria changed since you read them; the report and the ticket's Test Results name the directory the commands ran in, the acceptance_id of the criteria that ran, and candidate (your name for what was under test, recorded as provenance only) if given. If the criteria change while commands are running, the result is returned but not recorded (record_error says so). Commands run as argv and never through a shell: quotes group arguments, but ;, |, &&, $(), backticks and ~ are literal text passed to the command. A command whose program is not in the host user's machine-local verify_allow list is reported as refused without running — you cannot widen that list, from this tool or from ticket content, so report a refusal to the user rather than working around it. Each command is bounded by the project's verify_timeout in the host user's machine-local config (default 120s), which you cannot change either. Both the allow-list and the bound are re-read from that config at the start of every new run, so an edit the host user makes applies to the next run without restarting the server. Criteria with no command are reported as unverified. Waits at most 10 seconds for commands: a longer run returns a verification_id and state, not a pass. Poll ticket_verify_status until terminal; it returns the complete report. Request cancellation does not cancel commands. Recover a lost response with ticket_verify_status using the ticket id, not another start. A repeated start with the same dir, acceptance_id and candidate joins the active run; one that differs in any of the three is refused, naming the active run, and starts nothing; after completion a start begins a new run. Jobs belong to this MCP session; disconnect cancels them. Use ticket_verify_cancel to stop explicitly.",
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
		// The contract under test is the one the caller read, or it is not run:
		// a check decided before any directory is resolved or command started.
		acceptanceID := ticket.CriteriaIdentity(criteria)
		if args.AcceptanceID != "" && args.AcceptanceID != acceptanceID {
			r, _ := errResult("acceptance criteria of %s changed: you read %s, the ticket now has %s; call ticket_criteria again and verify what it returns", t.ID, ticket.SanitizeControl(args.AcceptanceID), acceptanceID)
			return r, nil, nil
		}

		// Eligibility before anything runs: only a checkout registered on this
		// machine for the ticket's own project is somewhere its commands may
		// execute. Root has none, an unregistered project has none here, and a
		// checkout that is gone is not stood in for by the server's working
		// directory or the store. A bare ID belongs to the server's default
		// project, the one it was started in.
		ns, _ := ticket.ParseNamespacedID(t.ID)
		if ns == "" {
			ns = defaultProject
		}
		if ns == "" {
			r, _ := errResult("cannot resolve project directory: ticket ID %q has no project namespace", t.ID)
			return r, nil, nil
		}
		cfg, err := project.Load()
		if err != nil {
			r, _ := errResult("cannot resolve project directory: load config: %v", err)
			return r, nil, nil
		}
		dir, err := project.ExecutionDir(cfg, ns)
		if err != nil {
			r, _ := errResult("cannot run verify commands: %v", err)
			return r, nil, nil
		}
		// dir moves an eligible run within the ticket's own repository; it does
		// not make one eligible, and it cannot reach a directory that repository
		// does not own. The policy below is still the project's: a worktree
		// changes where the commands run, not what may run or for how long.
		if args.Dir != nil {
			dir, err = project.ValidateExecutionDir(dir, *args.Dir)
			if err != nil {
				// The refusal echoes the caller's path; sanitized like the
				// acceptance_id refusal above.
				r, _ := errResult("cannot run verify commands: %s", ticket.SanitizeControl(err.Error()))
				return r, nil, nil
			}
		}
		// An unusable verify_timeout is not a refusal here: it is refused per
		// criterion the way an unreadable allow-list is, naming the key in the
		// recorded output.
		timeout, timeoutErr := project.VerifyTimeout(cfg, ns)

		// The allow-list and the timeout come from machine-local config only — no
		// tool argument carries either, so a caller cannot widen what runs or how
		// long it may run.
		allow, allowErr := project.VerifyAllow()
		policy := ticket.VerifyPolicy{Allow: allow, AllowErr: allowErr, Timeout: timeout, TimeoutErr: timeoutErr}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		prov := ticket.VerifyProvenance{Dir: dir, AcceptanceID: acceptanceID, Candidate: args.Candidate}
		job, err := jobs.start(req.Session, t.ID, prov, func(ctx context.Context) (ticket.VerifyReport, string, error) {
			results, err := ticket.RunVerify(ctx, criteria, dir, policy)
			return ticket.NewVerifyReport(t.ID, prov, results), ticket.FormatVerifyRecord(results, prov, time.Now().UTC()), err
		}, func(record string) error {
			// The current body, so edits made during the run survive — unless
			// the criteria themselves changed, which RecordVerify refuses.
			return ticket.RecordVerify(ticket.WithSource(store, sourceFor(req, "")), t.ID, record, acceptanceID)
		})
		if err != nil {
			r, _ := errResult("cannot start verification: %v", err)
			return r, nil, nil
		}
		return job.response(ctx, true)
	})
	return jobs
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
