package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	ticketmcp "github.com/EnderRealm/ticket/v8/internal/mcp"
	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverOver is a second server process over a store directory another
// session already wrote: the restart case, where nothing but the files
// survives.
func serverOver(t *testing.T, dir string) *mcp.ClientSession {
	t.Helper()
	server := ticketmcp.NewServer(ticket.NewFileStore(dir), "", "")
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	runServer(t, server.Run, st)
	client := mcp.NewClient(&mcp.Implementation{Name: "restarted", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// actionResponse decodes an action tool's response into its three parts.
func actionResponse(t *testing.T, result *mcp.CallToolResult, name string) (tk map[string]any, receipt map[string]any, replayed bool) {
	t.Helper()
	if result.IsError {
		t.Fatalf("%s error: %v", name, result.Content)
	}
	var resp struct {
		Ticket   map[string]any `json:"ticket"`
		Receipt  map[string]any `json:"receipt"`
		Replayed bool           `json:"replayed"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	return resp.Ticket, resp.Receipt, resp.Replayed
}

func applyAction(t *testing.T, session *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	return callTool(t, session, "ticket_apply_action", args)
}

func discover(t *testing.T, session *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	return callTool(t, session, "ticket_discover", args)
}

func notesOf(shown map[string]any) []any {
	notes, _ := shown["notes"].([]any)
	return notes
}

func actionsOf(shown map[string]any) []any {
	actions, _ := shown["actions"].([]any)
	return actions
}

// ─── AC1: a stale precondition is refused and the ticket left alone ────────

func TestApplyActionRefusesAStalePrecondition(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Precondition", "description": "d"})

	shown := showTicket(t, session, id)
	pre, _ := shown["precondition"].(string)
	if pre == "" {
		t.Fatal("ticket_show reported no precondition")
	}

	// A human edits in between.
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "status": "done"})
	edited := showTicket(t, session, id)
	if edited["precondition"] == pre {
		t.Fatal("an edit did not move the precondition")
	}

	result := applyAction(t, session, map[string]any{
		"id": id, "action_id": "obs-1", "precondition": pre, "note": "regressed", "transition": "reopen",
	})
	text := errorText(t, result, "an action on a stale precondition")
	if !strings.HasPrefix(text, "precondition conflict: ") {
		t.Errorf("error %q does not carry the precondition conflict prefix", text)
	}
	after := showTicket(t, session, id)
	if !reflect.DeepEqual(after, edited) {
		t.Errorf("a refused action changed the ticket:\n%v\nwas\n%v", after, edited)
	}
	if after["status"] != "done" || len(notesOf(after)) != 0 || len(actionsOf(after)) != 0 {
		t.Errorf("after the refusal: status %v, %d notes, %d actions", after["status"], len(notesOf(after)), len(actionsOf(after)))
	}

	// The current one lands.
	current, _ := edited["precondition"].(string)
	tk, receipt, replayed := actionResponse(t, applyAction(t, session, map[string]any{
		"id": id, "action_id": "obs-1", "precondition": current, "note": "regressed", "transition": "reopen",
	}), "ticket_apply_action")
	if replayed || tk["status"] != "open" || receipt["outcome"] != "open" || receipt["id"] != "obs-1" || receipt["kind"] != "apply" {
		t.Errorf("apply = replayed %v, status %v, receipt %v", replayed, tk["status"], receipt)
	}
	if receipt["source"] != "test" {
		t.Errorf("receipt source = %v, want the client name", receipt["source"])
	}
	final := showTicket(t, session, id)
	if final["status"] != "open" || len(notesOf(final)) != 1 || len(actionsOf(final)) != 1 || final["completed"] != nil {
		t.Errorf("after the action: %v", final)
	}
}

// ─── AC3: replay in-session, across a restart, and conflict ────────────────

func TestApplyActionReplaysAndRefusesConflicts(t *testing.T) {
	session, dir := testServerDir(t)
	id := createTicketID(t, session, map[string]any{"title": "Replay"})
	args := map[string]any{"id": id, "action_id": "obs-lost", "note": "evidence", "transition": "reopen", "source": "observer"}

	_, receipt, replayed := actionResponse(t, applyAction(t, session, args), "first")
	if replayed {
		t.Fatal("first application reported as replayed")
	}
	fileBefore, err := os.ReadFile(filepath.Join(dir, id+".md"))
	if err != nil {
		t.Fatal(err)
	}

	// The response was lost; the same call again, in this session and from
	// a fresh server over the same files.
	for name, s := range map[string]*mcp.ClientSession{"same session": session, "after restart": serverOver(t, dir)} {
		tk, again, replayed := actionResponse(t, applyAction(t, s, args), name)
		if !replayed || !reflect.DeepEqual(again, receipt) {
			t.Errorf("%s: replayed %v, receipt %v, want %v", name, replayed, again, receipt)
		}
		if tk["status"] != "open" {
			t.Errorf("%s: replayed ticket status = %v", name, tk["status"])
		}
	}
	fileAfter, _ := os.ReadFile(filepath.Join(dir, id+".md"))
	if string(fileAfter) != string(fileBefore) {
		t.Errorf("a replay changed the file:\n%s", fileAfter)
	}

	// Same id, other content.
	changed := map[string]any{"id": id, "action_id": "obs-lost", "note": "other evidence", "transition": "reopen"}
	text := errorText(t, applyAction(t, session, changed), "an action with other content under a used id")
	if !strings.HasPrefix(text, "action conflict: ") || !strings.Contains(text, "obs-lost") {
		t.Errorf("error %q does not carry the action conflict prefix and the id", text)
	}
	fileAfter, _ = os.ReadFile(filepath.Join(dir, id+".md"))
	if string(fileAfter) != string(fileBefore) {
		t.Errorf("a refused conflict changed the file:\n%s", fileAfter)
	}
	shown := showTicket(t, session, id)
	if len(notesOf(shown)) != 1 || len(actionsOf(shown)) != 1 {
		t.Errorf("notes = %d, actions = %d; want one of each", len(notesOf(shown)), len(actionsOf(shown)))
	}
}

func TestApplyActionRefusesTransitions(t *testing.T) {
	session := testServer(t)
	epic := createTicketID(t, session, map[string]any{"title": "Epic", "type": "epic"})
	leaf := createTicketID(t, session, map[string]any{"title": "Leaf", "parent": epic})

	text := errorText(t, applyAction(t, session, map[string]any{"id": epic, "action_id": "a", "note": "n", "transition": "reopen"}), "reopen on an epic")
	if !strings.HasPrefix(text, "transition not permitted: ") {
		t.Errorf("error %q does not carry the transition prefix", text)
	}
	text = errorText(t, applyAction(t, session, map[string]any{"id": leaf, "action_id": "a", "note": "n", "transition": "close"}), "an unknown transition")
	if !strings.HasPrefix(text, "transition not permitted: ") {
		t.Errorf("error %q does not carry the transition prefix", text)
	}
	text = errorText(t, applyAction(t, session, map[string]any{"id": leaf, "action_id": "a", "note": " "}), "an action with no note")
	if strings.HasPrefix(text, "precondition conflict: ") || strings.HasPrefix(text, "action conflict: ") || strings.HasPrefix(text, "transition not permitted: ") {
		t.Errorf("a missing note wore one of the protocol prefixes: %q", text)
	}
	for _, id := range []string{epic, leaf} {
		shown := showTicket(t, session, id)
		if len(notesOf(shown)) != 0 || len(actionsOf(shown)) != 0 {
			t.Errorf("%s: a refusal wrote %v", id, shown)
		}
	}
}

func TestApplyActionCompetingWriters(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Competing"})
	const n = 10

	var wg sync.WaitGroup
	var mu sync.Mutex
	var receipts []map[string]any
	fresh := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Half the writers share one action id, half carry their own.
			actionID := "shared"
			if i%2 == 1 {
				actionID = "own-" + string(rune('a'+i))
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "ticket_apply_action",
				Arguments: map[string]any{"id": id, "action_id": actionID, "note": "evidence for " + actionID},
			})
			if err != nil {
				t.Error(err)
				return
			}
			if result.IsError {
				t.Errorf("ticket_apply_action error: %v", result.Content)
				return
			}
			var resp struct {
				Receipt  map[string]any `json:"receipt"`
				Replayed bool           `json:"replayed"`
			}
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp); err != nil {
				t.Errorf("invalid JSON response: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if actionID == "shared" {
				receipts = append(receipts, resp.Receipt)
				if !resp.Replayed {
					fresh++
				}
			}
		}(i)
	}
	wg.Wait()
	if fresh != 1 {
		t.Errorf("%d writers applied the shared action, want 1", fresh)
	}
	for _, r := range receipts[1:] {
		if !reflect.DeepEqual(r, receipts[0]) {
			t.Errorf("shared receipts differ: %v vs %v", receipts[0], r)
		}
	}
	shown := showTicket(t, session, id)
	want := n/2 + 1
	if len(notesOf(shown)) != want || len(actionsOf(shown)) != want {
		t.Errorf("notes = %d, actions = %d; want %d of each", len(notesOf(shown)), len(actionsOf(shown)), want)
	}
}

// ─── AC4: discovery under a finding key ────────────────────────────────────

func TestDiscoverDeduplicatesByKeyPerProject(t *testing.T) {
	session, root := testCentralServer(t, "alpha", "beta")
	args := map[string]any{
		"finding_key": "flaky:TestFoo", "project": "alpha", "title": "Flaky TestFoo", "type": "bug",
		"description": "TestFoo failed 3 of 10 runs", "acceptance": "- passes 10 of 10\n  verify: go test -run TestFoo -count=10 ./...",
		"tags": "observer,flaky", "source": "observer",
	}
	const n = 8

	var wg sync.WaitGroup
	var mu sync.Mutex
	var ids []string
	fresh := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "ticket_discover", Arguments: args})
			if err != nil {
				t.Error(err)
				return
			}
			if result.IsError {
				t.Errorf("ticket_discover error: %v", result.Content)
				return
			}
			var resp struct {
				Ticket   map[string]any `json:"ticket"`
				Replayed bool           `json:"replayed"`
			}
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp); err != nil {
				t.Errorf("invalid JSON response: %v", err)
				return
			}
			id, ok := resp.Ticket["id"].(string)
			if !ok {
				t.Errorf("response ticket carries no id: %v", resp.Ticket)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ids = append(ids, id)
			if !resp.Replayed {
				fresh++
			}
		}()
	}
	wg.Wait()
	if fresh != 1 || len(ids) != n {
		t.Fatalf("%d of %d concurrent discoveries created a ticket, want 1", fresh, len(ids))
	}
	for _, id := range ids[1:] {
		if id != ids[0] {
			t.Errorf("callers got different tickets: %s vs %s", ids[0], id)
		}
	}
	id := ids[0]
	if !strings.HasPrefix(id, "alpha/") {
		t.Errorf("id %q is not in alpha", id)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "tickets", "alpha"))
	if len(entries) != 1 {
		t.Errorf("alpha holds %d files, want 1", len(entries))
	}

	// The retry after a lost response.
	tk, receipt, replayed := actionResponse(t, discover(t, session, args), "retry")
	if !replayed || tk["id"] != id {
		t.Errorf("retry = replayed %v, %v", replayed, tk["id"])
	}
	_, bare := ticket.ParseNamespacedID(id)
	if receipt["id"] != "flaky:TestFoo" || receipt["kind"] != "discover" || receipt["outcome"] != bare || receipt["source"] != "observer" {
		t.Errorf("receipt = %v", receipt)
	}
	if tk["title"] != "Flaky TestFoo" || tk["status"] != "backlog" || tk["type"] != "bug" {
		t.Errorf("replayed ticket = %v", tk)
	}
	if pre, _ := tk["precondition"]; pre != nil {
		// The action response is the ticket's serialization; the precondition
		// is ticket_show's to report.
		t.Errorf("action response carries a precondition: %v", pre)
	}
	shown := showTicket(t, session, id)
	if len(actionsOf(shown)) != 1 {
		t.Errorf("stored actions = %v, want the one receipt", actionsOf(shown))
	}

	// Changed content under the key.
	changed := map[string]any{}
	for k, v := range args {
		changed[k] = v
	}
	changed["title"] = "Flaky TestFoo — now failing every run"
	text := errorText(t, discover(t, session, changed), "a discovery with other content under a used key")
	if !strings.HasPrefix(text, "action conflict: ") || !strings.Contains(text, "flaky:TestFoo") || !strings.Contains(text, bare) {
		t.Errorf("error %q does not carry the action conflict prefix, the key and the existing ticket", text)
	}
	entries, _ = os.ReadDir(filepath.Join(root, "tickets", "alpha"))
	if len(entries) != 1 {
		t.Errorf("alpha holds %d files after a refused discovery, want 1", len(entries))
	}

	// The same key in another project is another finding.
	other := map[string]any{}
	for k, v := range args {
		other[k] = v
	}
	other["project"] = "beta"
	tk, _, replayed = actionResponse(t, discover(t, session, other), "beta")
	if replayed || !strings.HasPrefix(tk["id"].(string), "beta/") {
		t.Errorf("same key in beta = replayed %v, %v", replayed, tk["id"])
	}

	// A missing key, and no destination, are refused before anything is written.
	noKey := map[string]any{"finding_key": "", "project": "alpha", "title": "No key"}
	if r := discover(t, session, noKey); !r.IsError {
		t.Error("a discovery with no finding key was accepted")
	}
	noDest := map[string]any{"finding_key": "k", "title": "No destination"}
	text = errorText(t, discover(t, session, noDest), "a discovery with no destination")
	if !strings.Contains(text, "no destination") {
		t.Errorf("error %q does not name the missing destination", text)
	}
	entries, _ = os.ReadDir(filepath.Join(root, "tickets", "alpha"))
	if len(entries) != 1 {
		t.Errorf("alpha holds %d files after refused discoveries, want 1", len(entries))
	}
}

func TestDiscoverCarriesCreateWarningsAndRefusals(t *testing.T) {
	session := testServer(t)
	tk, _, _ := actionResponse(t, discover(t, session, map[string]any{
		"finding_key": "k1", "title": "Bare", "description": "described", "acceptance": "- something",
	}), "discover")
	if tk["bare_acceptance_warning"] == nil || tk["bare_acceptance_criteria"] == nil {
		t.Errorf("a discovery with a bare criterion carried no warning: %v", tk)
	}
	tk, _, _ = actionResponse(t, discover(t, session, map[string]any{
		"finding_key": "k2", "title": "Empty", "description": "described",
	}), "discover")
	if tk["empty_acceptance_warning"] == nil {
		t.Errorf("a discovery with no acceptance carried no warning: %v", tk)
	}
	// A replay does not repeat the create-time warnings: nothing was created.
	tk, _, replayed := actionResponse(t, discover(t, session, map[string]any{
		"finding_key": "k2", "title": "Empty", "description": "described",
	}), "replay")
	if !replayed || tk["empty_acceptance_warning"] != nil {
		t.Errorf("replay = replayed %v, warning %v", replayed, tk["empty_acceptance_warning"])
	}

	result := discover(t, session, map[string]any{"finding_key": "k3", "title": "Fragment", "description": corruptedDescription()})
	text := errorText(t, result, "a discovery ending in an envelope fragment")
	if !strings.Contains(text, "envelope fragment") {
		t.Errorf("error %q does not name the fragment", text)
	}
	if r := discover(t, session, map[string]any{"finding_key": "k4", "title": ""}); !r.IsError {
		t.Error("a discovery with no title was accepted")
	}
}

func TestDiscoverThroughRepoResolvesTheProjectStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tickets", "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := project.Config{
		CentralRoot: root,
		Projects:    map[string]project.ProjectConfig{"beta": {Path: repoDir, Store: "central"}},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session := testServer(t)
	args := map[string]any{"finding_key": "lint:x", "title": "Via repo", "repo": repoDir}
	tk, receipt, replayed := actionResponse(t, discover(t, session, args), "discover")
	id, _ := tk["id"].(string)
	if replayed || !strings.HasPrefix(id, "beta/") {
		t.Fatalf("discover via repo = replayed %v, %q", replayed, id)
	}
	_, bare := ticket.ParseNamespacedID(id)
	if receipt["outcome"] != bare {
		t.Errorf("receipt outcome = %v, want %s", receipt["outcome"], bare)
	}
	tk, _, replayed = actionResponse(t, discover(t, session, args), "retry")
	if !replayed || tk["id"] != id {
		t.Errorf("retry via repo = replayed %v, %v; want %s", replayed, tk["id"], id)
	}
}

// ─── Compatibility: the ordinary tools over a ticket carrying receipts ─────

func TestOrdinaryToolsKeepReceipts(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Compat"})
	actionResponse(t, applyAction(t, session, map[string]any{"id": id, "action_id": "a", "note": "evidence"}), "apply")

	noted := callObject(t, session, "ticket_add_note", map[string]any{"id": id, "text": "a plain note"})
	if n, _ := noted["notes"].([]any); len(n) != 2 {
		t.Errorf("ticket_add_note left %d notes, want 2", len(n))
	}
	edited := callObject(t, session, "ticket_edit", map[string]any{"id": id, "title": "Compat (edited)", "status": "done"})
	if edited["title"] != "Compat (edited)" || edited["status"] != "done" {
		t.Errorf("ticket_edit = %v", edited)
	}
	callObject(t, session, "ticket_dep", map[string]any{"id": id, "dep_id": createTicketID(t, session, map[string]any{"title": "Dep"}), "action": "add"})

	shown := showTicket(t, session, id)
	actions := actionsOf(shown)
	if len(actions) != 1 {
		t.Fatalf("actions after ordinary writes = %v, want the one receipt", actions)
	}
	if r, _ := actions[0].(map[string]any); r["id"] != "a" || r["kind"] != "apply" || r["outcome"] != "backlog" {
		t.Errorf("receipt after ordinary writes = %v", actions[0])
	}
	if len(notesOf(shown)) != 2 || shown["status"] != "done" {
		t.Errorf("after ordinary writes: %d notes, status %v", len(notesOf(shown)), shown["status"])
	}
	// A verdict beside a receipt: both ledgers append independently.
	if r := recordVerdict(t, session, map[string]any{"id": id, "sha": verdictHead, "class": "test-verified", "role": "worker", "evidence": "go test"}); r.IsError {
		t.Fatalf("ticket_verdict_record: %v", r.Content)
	}
	shown = showTicket(t, session, id)
	if v, _ := shown["verdicts"].([]any); len(v) != 1 || len(actionsOf(shown)) != 1 {
		t.Errorf("verdicts = %v, actions = %v", shown["verdicts"], shown["actions"])
	}
	// And the action protocol keeps working over the edited ticket.
	tk, _, replayed := actionResponse(t, applyAction(t, session, map[string]any{"id": id, "action_id": "b", "note": "later", "transition": "reopen"}), "apply")
	if replayed || tk["status"] != "open" {
		t.Errorf("apply after edits = replayed %v, %v", replayed, tk["status"])
	}
}
