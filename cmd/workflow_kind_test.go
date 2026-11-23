package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// genericHow and genericCommands are lines a YAML writer would mangle if it
// wrote them plain: comments, mapping and sequence indicators, quotes, shell
// operators, surrounding spaces, backslashes, scalars that read as another type.
var genericHow = []string{
	"Install the build on the phone from TestFlight.",
	"Open the app: the About screen should read v2 # not a comment",
	"  - indented, with a leading dash  ",
}

var genericCommands = []string{
	`ssh mac2 'sudo launchctl kickstart -k system/com.example.server' && echo "restarted: ok"`,
	`tail -n 50 /var/log/app.log | grep -E 'error|warn' > /tmp/out.txt # keep`,
	`echo $(date +%s) ${HOME} $((1+2))`,
	`  padded command  `,
	`C:\path\to\thing \\ trailing\`,
	`- looks like a list item`,
	`key: value`,
	`[a, b], {c: d}`,
	`null`,
	`true`,
	`0x1f`,
	`'single' "double" ` + "`backtick`",
	"tab\there",
	`? question & *alias !tag %dir @at | >`,
}

// createWith runs `tk create` with repeatable flags set on the shared command,
// restoring every flag afterwards. It returns the created ticket's ID.
func createWith(t *testing.T, store *ticket.FileStore, title string, flags ...string) (string, error) {
	t.Helper()
	f := createCmd.Flags()
	// Registered first, so a Set that fails part way still leaves the shared
	// command clean for the next test.
	t.Cleanup(func() { resetFlags(f) })
	for i := 0; i+1 < len(flags); i += 2 {
		if err := f.Set(flags[i], flags[i+1]); err != nil {
			t.Fatalf("set %s: %v", flags[i], err)
		}
	}

	before := map[string]bool{}
	if existing, err := store.List(); err == nil {
		for _, tk := range existing {
			before[tk.ID] = true
		}
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	createErr := func() error {
		// Deferred so a panic in runCreate cannot leave the package writing
		// into a closed file.
		oldStdout, oldStderr := os.Stdout, os.Stderr
		defer func() {
			os.Stdout, os.Stderr = oldStdout, oldStderr
			devNull.Close()
		}()
		os.Stdout, os.Stderr = devNull, devNull
		return runCreate(createCmd, []string{title})
	}()
	if createErr != nil {
		return "", createErr
	}

	after, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range after {
		if !before[tk.ID] {
			return tk.ID, nil
		}
	}
	t.Fatalf("create %q left no new ticket in the store", title)
	return "", nil
}

// resetFlags puts a shared flag set back to its defaults: a StringArray's Set
// appends, so its value is replaced rather than set.
func resetFlags(f *pflag.FlagSet) {
	f.VisitAll(func(fl *pflag.Flag) {
		if !fl.Changed {
			return
		}
		if sv, ok := fl.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = fl.Value.Set(fl.DefValue)
		}
		fl.Changed = false
	})
}

// lineFlags expands a list into repeated name/value flag pairs.
func lineFlags(name string, lines []string) []string {
	var out []string
	for _, l := range lines {
		out = append(out, name, l)
	}
	return out
}

// callToolErr calls a tool expected to refuse and returns the refusal text.
func callToolErr(t *testing.T, session *gomcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	text := result.Content[0].(*gomcp.TextContent).Text
	if !result.IsError {
		t.Fatalf("%s accepted %v: %s", name, args, text)
	}
	return text
}

// stringsOf reads a JSON array of strings off a decoded response.
func stringsOf(v any) []string {
	items, _ := v.([]any)
	out := []string{}
	for _, it := range items {
		s, _ := it.(string)
		out = append(out, s)
	}
	return out
}

// rowIDs collects the IDs of the rows under key in a listing response.
func rowIDs(resp map[string]any, key string) map[string]bool {
	ids := map[string]bool{}
	for _, r := range resp[key].([]any) {
		ids[r.(map[string]any)["id"].(string)] = true
	}
	return ids
}

func assertGeneric(t *testing.T, where string, tk *ticket.Ticket) {
	t.Helper()
	if tk.WorkflowKind != ticket.WorkflowGeneric {
		t.Errorf("%s: workflow_kind = %q, want generic", where, tk.WorkflowKind)
	}
	if !reflect.DeepEqual(tk.How, genericHow) {
		t.Errorf("%s: how = %q, want %q", where, tk.How, genericHow)
	}
	if !reflect.DeepEqual(tk.Commands, genericCommands) {
		t.Errorf("%s: commands = %q, want %q", where, tk.Commands, genericCommands)
	}
}

func assertGenericJSON(t *testing.T, where string, resp map[string]any) {
	t.Helper()
	if resp["workflow_kind"] != "generic" {
		t.Errorf("%s: workflow_kind = %v, want generic", where, resp["workflow_kind"])
	}
	if got := stringsOf(resp["how"]); !reflect.DeepEqual(got, genericHow) {
		t.Errorf("%s: how = %q, want %q", where, got, genericHow)
	}
	if got := stringsOf(resp["commands"]); !reflect.DeepEqual(got, genericCommands) {
		t.Errorf("%s: commands = %q, want %q", where, got, genericCommands)
	}
}

// A ticket created or edited as generic, through the CLI and through MCP,
// reads back with its kind, steps and commands unchanged — the commands byte
// for byte, from the file itself as well as from every read surface.
func TestGenericKindRoundTrips(t *testing.T) {
	store := centralStore(t, "wk-trip")

	flags := append([]string{"workflow-kind", "generic"}, lineFlags("how", genericHow)...)
	flags = append(flags, lineFlags("command", genericCommands)...)
	cliCreated, err := createWith(t, store, "Install the build on a phone", flags...)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.Get(cliCreated)
	if err != nil {
		t.Fatal(err)
	}
	assertGeneric(t, "tk create", got)

	// Parsed from the bytes on disk, not a cached struct.
	data, err := os.ReadFile(filepath.Join(store.Dir, cliCreated+".md"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ticket.Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse stored file: %v\n%s", err, data)
	}
	assertGeneric(t, "stored file", parsed)

	if err := store.Create(&ticket.Ticket{ID: "wk-edit", Status: ticket.StatusReady, Type: ticket.TypeFeature, Created: time.Now(), Title: "Edited to generic", Body: "\n"}); err != nil {
		t.Fatal(err)
	}
	if err := runEditWith(t, "wk-edit", flags...); err != nil {
		t.Fatalf("edit: %v", err)
	}
	got, err = store.Get("wk-edit")
	if err != nil {
		t.Fatal(err)
	}
	assertGeneric(t, "tk edit", got)

	// An edit naming the flag replaces the list; an empty value clears it.
	if err := runEditWith(t, "wk-edit", "command", "make deploy", "how", ""); err != nil {
		t.Fatalf("edit replace: %v", err)
	}
	got, _ = store.Get("wk-edit")
	if !reflect.DeepEqual(got.Commands, []string{"make deploy"}) || len(got.How) != 0 || got.WorkflowKind != ticket.WorkflowGeneric {
		t.Errorf("after replace: kind %q how %q commands %q", got.WorkflowKind, got.How, got.Commands)
	}

	rows := captureQuery(t)
	assertGenericJSON(t, "tk query", rows[cliCreated])

	served, root, err := serveStore()
	if err != nil {
		t.Fatal(err)
	}
	session := serveSession(t, served, "wk-trip", root)

	assertGenericJSON(t, "ticket_show of CLI create", callTool(t, session, "ticket_show", map[string]any{"id": "wk-trip/" + cliCreated}))

	created := callTool(t, session, "ticket_create", map[string]any{
		"title":         "Restart the server on the other Mac",
		"workflow_kind": "generic",
		"how":           genericHow,
		"commands":      genericCommands,
	})
	assertGenericJSON(t, "ticket_create response", created)
	mcpID := created["id"].(string)
	assertGenericJSON(t, "ticket_show of ticket_create", callTool(t, session, "ticket_show", map[string]any{"id": mcpID}))

	if err := store.Create(&ticket.Ticket{ID: "wk-mcp-edit", Status: ticket.StatusReady, Type: ticket.TypeFeature, Created: time.Now(), Title: "MCP edited to generic", Body: "\n"}); err != nil {
		t.Fatal(err)
	}
	callTool(t, session, "ticket_edit", map[string]any{
		"id":            "wk-trip/wk-mcp-edit",
		"workflow_kind": "generic",
		"how":           genericHow,
		"commands":      genericCommands,
	})
	assertGenericJSON(t, "ticket_show of ticket_edit", callTool(t, session, "ticket_show", map[string]any{"id": "wk-trip/wk-mcp-edit"}))
	got, _ = store.Get("wk-mcp-edit")
	assertGeneric(t, "ticket_edit stored", got)

	// An edit that does not name the lists leaves them; an empty array clears.
	callTool(t, session, "ticket_edit", map[string]any{"id": "wk-trip/wk-mcp-edit", "title": "Renamed"})
	got, _ = store.Get("wk-mcp-edit")
	assertGeneric(t, "ticket_edit of another field", got)
	callTool(t, session, "ticket_edit", map[string]any{"id": "wk-trip/wk-mcp-edit", "how": []string{}})
	got, _ = store.Get("wk-mcp-edit")
	if len(got.How) != 0 || !reflect.DeepEqual(got.Commands, genericCommands) {
		t.Errorf("after clearing how: how %q commands %q", got.How, got.Commands)
	}

	// One non-empty line each: a value carrying a line break is refused, not
	// split, and an empty entry is refused rather than stored as a blank step.
	if text := callToolErr(t, session, "ticket_create", map[string]any{"title": "Two lines", "workflow_kind": "generic", "commands": []string{"echo a\necho b"}}); !strings.Contains(text, "commands") {
		t.Errorf("multi-line command refusal does not name commands: %s", text)
	}
	if text := callToolErr(t, session, "ticket_create", map[string]any{"title": "Blank step", "workflow_kind": "generic", "how": []string{""}}); !strings.Contains(text, "how") {
		t.Errorf("empty how entry refusal does not name how: %s", text)
	}
	if text := callToolErr(t, session, "ticket_edit", map[string]any{"id": "wk-trip/wk-mcp-edit", "commands": []string{"make deploy", ""}}); !strings.Contains(text, "commands") {
		t.Errorf("empty command entry refusal does not name commands: %s", text)
	}
	if got, _ := store.Get("wk-mcp-edit"); !reflect.DeepEqual(got.Commands, genericCommands) {
		t.Errorf("refused edit changed commands to %q", got.Commands)
	}
	if err := runEditWith(t, "wk-edit", "how", "step one\rstep two"); err == nil || !strings.Contains(err.Error(), "how") {
		t.Errorf("multi-line how step: err = %v, want a refusal naming how", err)
	}
}

// Ready and frontier list what a run can start, so a generic ticket is left
// out of both on every surface; as a dependency it still holds its dependent
// until it is finished.
func TestFrontierLeavesOutGeneric(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	alpha := stores["alpha"]
	frontierTicket(t, alpha, "fr-gen", ticket.StatusReady)
	frontierTicket(t, alpha, "fr-gen-open", ticket.StatusOpen)
	frontierTicket(t, alpha, "fr-after", ticket.StatusReady, "fr-gen")
	frontierTicket(t, alpha, "fr-plain", ticket.StatusReady)
	for _, id := range []string{"fr-gen", "fr-gen-open"} {
		if _, err := ticket.Mutate(alpha, id, func(tk *ticket.Ticket) error {
			tk.WorkflowKind = ticket.WorkflowGeneric
			tk.Commands = []string{"open https://example.com/release"}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	served, root, err := serveStore()
	if err != nil {
		t.Fatal(err)
	}
	session := serveSession(t, served, "alpha", root)

	frontierIDs := func() map[string]bool {
		jsonOutput = true
		defer func() { jsonOutput = false }()
		ids := map[string]bool{}
		out := captureFrontier(t)
		if strings.TrimSpace(out) == "" || !strings.HasPrefix(strings.TrimSpace(out), "[") {
			return ids
		}
		var rows []map[string]any
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("frontier --json: %v\n%s", err, out)
		}
		for _, r := range rows {
			ids[r["id"].(string)] = true
		}
		return ids
	}
	check := func(stage string, want map[string]bool) {
		t.Helper()
		surfaces := map[string]map[string]bool{
			"tk frontier":     frontierIDs(),
			"ticket_frontier": rowIDs(callTool(t, session, "ticket_frontier", map[string]any{}), "tickets"),
			"ticket_ready":    rowIDs(callTool(t, session, "ticket_ready", map[string]any{}), "tickets"),
		}
		readyLib, err := ticket.ReadyTickets(served)
		if err != nil {
			t.Fatal(err)
		}
		lib := map[string]bool{}
		for _, tk := range readyLib {
			lib[tk.ID] = true
		}
		surfaces["ticket.ReadyTickets"] = lib
		for surface, ids := range surfaces {
			for id, present := range want {
				if ids["alpha/"+id] != present {
					t.Errorf("%s: %s lists %s = %v, want %v (rows %v)", stage, surface, id, ids["alpha/"+id], present, ids)
				}
			}
		}
	}

	check("generic unfinished", map[string]bool{"fr-gen": false, "fr-gen-open": false, "fr-after": false, "fr-plain": true})
	blocked := rowIDs(callTool(t, session, "ticket_blocked", map[string]any{}), "tickets")
	if !blocked["alpha/fr-after"] {
		t.Errorf("ticket_blocked does not hold the dependent of an unfinished generic ticket: %v", blocked)
	}

	if _, err := ticket.Mutate(alpha, "fr-gen", func(tk *ticket.Ticket) error {
		tk.Status = ticket.StatusDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	check("generic done", map[string]bool{"fr-gen": false, "fr-gen-open": false, "fr-after": true, "fr-plain": true})
}

// A workflow_kind outside coding and generic is refused on every write path,
// and the refusal names the field. The field is not reachable as an extra
// either, so the kind has one home.
func TestUnknownWorkflowKindRefused(t *testing.T) {
	store := centralStore(t, "wk-bad")

	if _, err := createWith(t, store, "Bad kind", "workflow-kind", "manual"); err == nil || !strings.Contains(err.Error(), "workflow_kind") {
		t.Errorf("tk create --workflow-kind manual: err = %v, want a refusal naming workflow_kind", err)
	}
	if tickets, _ := store.List(); len(tickets) != 0 {
		t.Errorf("refused create left %d tickets", len(tickets))
	}

	if err := store.Create(&ticket.Ticket{ID: "wk-bad-1", Status: ticket.StatusReady, Type: ticket.TypeFeature, Created: time.Now(), Title: "Coding", Body: "\n"}); err != nil {
		t.Fatal(err)
	}
	if err := runEditWith(t, "wk-bad-1", "workflow-kind", "manual"); err == nil || !strings.Contains(err.Error(), "workflow_kind") {
		t.Errorf("tk edit --workflow-kind manual: err = %v, want a refusal naming workflow_kind", err)
	}
	if err := runEditWith(t, "wk-bad-1", "set", "workflow_kind=generic"); err == nil || !strings.Contains(err.Error(), "workflow_kind") {
		t.Errorf("tk edit --set workflow_kind=generic: err = %v, want the key refused as reserved", err)
	}
	if got, _ := store.Get("wk-bad-1"); got.WorkflowKind != "" || len(got.Extra) != 0 {
		t.Errorf("refused edits changed the ticket: kind %q extra %v", got.WorkflowKind, got.Extra)
	}

	served, root, err := serveStore()
	if err != nil {
		t.Fatal(err)
	}
	session := serveSession(t, served, "wk-bad", root)
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"ticket_create", map[string]any{"title": "Bad kind", "workflow_kind": "manual"}},
		{"ticket_edit", map[string]any{"id": "wk-bad/wk-bad-1", "workflow_kind": "Generic"}},
		{"ticket_create", map[string]any{"title": "Kind as extra", "set": map[string]string{"workflow_kind": "generic"}}},
		{"ticket_edit", map[string]any{"id": "wk-bad/wk-bad-1", "set": map[string]string{"workflow_kind": "generic"}}},
	} {
		if text := callToolErr(t, session, call.tool, call.args); !strings.Contains(text, "workflow_kind") {
			t.Errorf("%s %v refusal does not name workflow_kind: %s", call.tool, call.args, text)
		}
	}

	// A bad value already in a file, as one pushed from another machine
	// arrives: the ticket still loads, and writing it back is refused.
	path := filepath.Join(store.Dir, "wk-bad-2.md")
	raw := "---\nid: wk-bad-2\nstatus: ready\ndeps: []\nlinks: []\ncreated: 2026-11-01T00:00:00Z\ntype: feature\npriority: 2\nworkflow_kind: manual\n---\n# Hand edited\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("wk-bad-2")
	if err != nil {
		t.Fatalf("Get of a stored bad kind: %v", err)
	}
	if got.WorkflowKind != "manual" {
		t.Errorf("stored kind read as %q, want manual", got.WorkflowKind)
	}
	if err := runEditWith(t, "wk-bad-2", "title", "Renamed"); err == nil || !strings.Contains(err.Error(), "workflow_kind") {
		t.Errorf("edit of a ticket storing a bad kind: err = %v, want a refusal naming workflow_kind", err)
	}
}

// Every list and query row carries the kind, coding when the ticket stores
// none; show and query carry how and commands. The file of a ticket written
// without a kind keeps no workflow_kind key.
func TestRowsCarryWorkflowKind(t *testing.T) {
	store := centralStore(t, "wk-rows")
	if err := store.Create(&ticket.Ticket{ID: "wk-coding", Status: ticket.StatusReady, Type: ticket.TypeFeature, Created: time.Now(), Title: "Coding", Body: "\n"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&ticket.Ticket{ID: "wk-generic", Status: ticket.StatusReady, Type: ticket.TypeFeature, Created: time.Now(), Title: "Generic", Body: "\n",
		WorkflowKind: ticket.WorkflowGeneric, How: genericHow, Commands: genericCommands}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(store.Dir, "wk-coding.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "workflow_kind") {
		t.Errorf("a ticket written without a kind stores one:\n%s", data)
	}

	rows := captureQuery(t)
	if rows["wk-coding"]["workflow_kind"] != "coding" {
		t.Errorf("tk query coding row workflow_kind = %v", rows["wk-coding"]["workflow_kind"])
	}
	for _, key := range []string{"how", "commands"} {
		if v, ok := rows["wk-coding"][key]; ok {
			t.Errorf("tk query coding row carries %s = %v", key, v)
		}
	}
	assertGenericJSON(t, "tk query generic row", rows["wk-generic"])

	// tk ls: a KIND column beside TYPE on every row.
	ls := captureLs(t)
	lsLines := strings.Split(ls, "\n")
	kindCol := -1
	for _, line := range lsLines {
		if strings.HasPrefix(line, "ID") {
			kindCol = strings.Index(line, "KIND")
			if typeCol := strings.Index(line, "TYPE"); kindCol < 0 || typeCol < 0 || kindCol < typeCol {
				t.Fatalf("tk ls header lacks KIND after TYPE: %q\n%s", line, ls)
			}
			break
		}
	}
	for id, kind := range map[string]string{"wk-coding": "coding", "wk-generic": "generic"} {
		found := false
		for _, line := range lsLines {
			if strings.HasPrefix(line, id+" ") {
				found = true
				if kindCol < 0 || !strings.HasPrefix(line[kindCol:], kind+" ") {
					t.Errorf("tk ls row %q does not carry kind %s under KIND", line, kind)
				}
			}
		}
		if !found {
			t.Errorf("tk ls has no row for %s:\n%s", id, ls)
		}
	}

	out := captureShow(t, store, "wk-generic", false)
	for _, want := range []string{"workflow_kind: generic", "how:", "commands:", "Install the build on the phone from TestFlight.", "padded command"} {
		if !strings.Contains(out, want) {
			t.Errorf("tk show lacks %q:\n%s", want, out)
		}
	}
	shownFront, _, _ := strings.Cut(strings.TrimPrefix(out, "---\n"), "\n---\n")
	var front map[string]any
	if err := yaml.Unmarshal([]byte(shownFront), &front); err != nil {
		t.Fatalf("tk show frontmatter: %v\n%s", err, shownFront)
	}
	if got := stringsOf(front["commands"]); !reflect.DeepEqual(got, genericCommands) {
		t.Errorf("tk show commands read back %q, want %q", got, genericCommands)
	}

	served, root, err := serveStore()
	if err != nil {
		t.Fatal(err)
	}
	session := serveSession(t, served, "wk-rows", root)

	kinds := map[string]string{"wk-rows/wk-coding": "coding", "wk-rows/wk-generic": "generic"}
	listed := callTool(t, session, "ticket_list", map[string]any{})
	if len(listed["tickets"].([]any)) != 2 {
		t.Fatalf("ticket_list rows = %v", listed["tickets"])
	}
	for _, r := range listed["tickets"].([]any) {
		row := r.(map[string]any)
		if row["workflow_kind"] != kinds[row["id"].(string)] {
			t.Errorf("ticket_list row %v workflow_kind = %v", row["id"], row["workflow_kind"])
		}
	}
	for _, tool := range []string{"ticket_ready", "ticket_frontier"} {
		for _, r := range callTool(t, session, tool, map[string]any{})["tickets"].([]any) {
			row := r.(map[string]any)
			if row["workflow_kind"] != "coding" {
				t.Errorf("%s row %v workflow_kind = %v", tool, row["id"], row["workflow_kind"])
			}
		}
	}
	assertGenericJSON(t, "ticket_show", callTool(t, session, "ticket_show", map[string]any{"id": "wk-rows/wk-generic"}))
	if shown := callTool(t, session, "ticket_show", map[string]any{"id": "wk-rows/wk-coding"}); shown["workflow_kind"] != "coding" {
		t.Errorf("ticket_show coding workflow_kind = %v", shown["workflow_kind"])
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()
	var frontier []map[string]any
	if err := json.Unmarshal([]byte(captureFrontier(t)), &frontier); err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 1 || frontier[0]["workflow_kind"] != "coding" {
		t.Errorf("tk frontier --json rows = %v, want the coding ticket carrying its kind", frontier)
	}
}
