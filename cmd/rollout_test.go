package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// rolloutCatalog is the shape the live catalog has before activation: a
// comment, legacy namespaces only, no required features, and most namespaces
// not catalogued at all.
const rolloutCatalog = "# Archived namespaces: retired products; tickets remain for history.\nnamespaces:\n  anvil:\n    kind: legacy\n"

// legacyTicket is a ticket file as an older tk wrote it, planted rather than
// created so its bytes are exactly what a pre-catalog store holds.
const legacyTicket = "---\nid: old-0001\nstatus: open\ntype: feature\n---\n# Old ticket\n\nWritten before the catalog existed.\n"

// rolloutCentral is a preactivation central store: warp registered at the
// test process's working directory, loom registered elsewhere, code a legacy
// directory nothing registers, anvil a legacy namespace the catalog already
// lists. warp and loom each hold an epic with the same bare ID, epic-1111,
// with one local child each, and code and anvil each hold old-0001.
func rolloutCentral(t *testing.T) (string, map[string]*ticket.FileStore) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	cfg := project.Config{
		CentralRoot: root,
		Projects: map[string]project.ProjectConfig{
			"warp": {Path: mustGetwd(), Store: "central"},
			"loom": {Path: t.TempDir(), Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	if err := os.WriteFile(ticket.CatalogPath(root), []byte(rolloutCatalog), 0o644); err != nil {
		t.Fatal(err)
	}
	stores := map[string]*ticket.FileStore{}
	for _, ns := range []string{"warp", "loom", "code", "anvil"} {
		dir := filepath.Join(root, "tickets", ns)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		stores[ns] = ticket.NewProjectFileStore(dir, ns)
	}
	for _, ns := range []string{"code", "anvil"} {
		if err := os.WriteFile(filepath.Join(stores[ns].Dir, "old-0001.md"), []byte(legacyTicket), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkCentral(t, stores["warp"], "epic-1111", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, stores["warp"], "wchild-0001", ticket.TypeFeature, ticket.StatusOpen, "epic-1111")
	mkCentral(t, stores["loom"], "epic-1111", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, stores["loom"], "lchild-0001", ticket.TypeFeature, ticket.StatusReady, "epic-1111")
	return root, stores
}

// rolloutSession is the MCP server `tk serve` runs over the central store,
// outside any repo.
func rolloutSession(t *testing.T, root string) *gomcp.ClientSession {
	t.Helper()
	return serveSession(t, ticket.NewMultiStore(filepath.Join(root, "tickets")), "", root)
}

// storeTree is every file under root with its content and every directory,
// for asserting a read-only or refused command left the store exactly as it
// was.
func storeTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			tree[path] = "<dir>"
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			tree[path] = "<symlink> " + target
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// ticketFiles is the subset of storeTree that is ticket files.
func ticketFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for path, content := range storeTree(t, filepath.Join(root, "tickets")) {
		if strings.HasSuffix(path, ".md") {
			files[path] = content
		}
	}
	return files
}

func assertSameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	for path, content := range before {
		if got, ok := after[path]; !ok {
			t.Errorf("%s: %s removed", what, path)
		} else if got != content {
			t.Errorf("%s: %s rewritten:\n--- before\n%s\n--- after\n%s", what, path, content, got)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("%s: %s added", what, path)
		}
	}
}

// runActivateWith runs tk activate against a fresh flag set, flags given as
// name/value pairs, and returns what it printed.
func runActivateWith(t *testing.T, flags ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	registerActivateFlags(cmd)
	for i := 0; i+1 < len(flags); i += 2 {
		if err := cmd.Flags().Set(flags[i], flags[i+1]); err != nil {
			t.Fatalf("set %s: %v", flags[i], err)
		}
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := runActivate(cmd, nil)
	return out.String(), err
}

// withProject runs fn with --project set to ns.
func withProject(ns string, fn func() error) error {
	old := projectFlag
	projectFlag = ns
	defer func() { projectFlag = old }()
	return fn()
}

// rolloutCall calls an MCP tool and returns its decoded object, or the error
// text when the tool refused.
func rolloutCall(t *testing.T, s *gomcp.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	result, err := s.CallTool(context.Background(), &gomcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*gomcp.TextContent).Text
	if result.IsError {
		return nil, text
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("%s: invalid JSON response: %v\n%s", name, err, text)
	}
	return decoded, ""
}

func rolloutOK(t *testing.T, s *gomcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	resp, refused := rolloutCall(t, s, name, args)
	if refused != "" {
		t.Fatalf("%s %v refused: %s", name, args, refused)
	}
	return resp
}

func rolloutRefused(t *testing.T, s *gomcp.ClientSession, name string, args map[string]any, want string) string {
	t.Helper()
	resp, refused := rolloutCall(t, s, name, args)
	if refused == "" {
		t.Fatalf("%s %v should have been refused, got %v", name, args, resp)
	}
	if !strings.Contains(refused, want) {
		t.Errorf("%s %v refusal should say %q, got: %s", name, args, want, refused)
	}
	return refused
}

func rolloutCreate(t *testing.T, s *gomcp.ClientSession, args map[string]any) string {
	t.Helper()
	id, _ := rolloutOK(t, s, "ticket_create", args)["id"].(string)
	if id == "" {
		t.Fatalf("ticket_create %v returned no id", args)
	}
	return id
}

func rolloutEdit(t *testing.T, s *gomcp.ClientSession, id string, fields map[string]any) {
	t.Helper()
	args := map[string]any{"id": id}
	for k, v := range fields {
		args[k] = v
	}
	rolloutOK(t, s, "ticket_edit", args)
}

// epicView is the part of ticket_show an epic rollup is read from.
type epicView struct {
	status   string
	closed   string
	complete bool
	children []string
	counts   map[string]any
}

func showEpic(t *testing.T, s *gomcp.ClientSession, id string) epicView {
	t.Helper()
	resp := rolloutOK(t, s, "ticket_show", map[string]any{"id": id, "metadata_only": true})
	v := epicView{status: fmt.Sprint(resp["status"]), complete: resp["complete"] == true}
	v.closed, _ = resp["closed"].(string)
	v.counts, _ = resp["counts"].(map[string]any)
	children, _ := resp["children"].([]any)
	for _, c := range children {
		v.children = append(v.children, c.(map[string]any)["id"].(string))
	}
	sort.Strings(v.children)
	return v
}

func closedAt(t *testing.T, s *gomcp.ClientSession, id string) string {
	t.Helper()
	closed, _ := rolloutOK(t, s, "ticket_show", map[string]any{"id": id, "metadata_only": true})["closed"].(string)
	if closed == "" {
		t.Fatalf("%s carries no closed time", id)
	}
	return closed
}

// idByTitle is the qualified ID of the one ticket in store with this title.
func idByTitle(t *testing.T, store *ticket.FileStore, title string) string {
	t.Helper()
	tickets, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tickets {
		if tk.Title == title {
			return ticket.QualifyRef(store.Project, tk.ID)
		}
	}
	t.Fatalf("no ticket titled %q in %s", title, store.Project)
	return ""
}

// queryRow is the CLI's own reading of one ticket, through `tk query`.
func queryRow(t *testing.T, ns, id string) map[string]any {
	t.Helper()
	var rows map[string]map[string]any
	if err := withProject(ns, func() error { rows = captureQuery(t); return nil }); err != nil {
		t.Fatal(err)
	}
	_, bare := ticket.ParseNamespacedID(id)
	for _, key := range []string{id, bare} {
		if row, ok := rows[key]; ok {
			return row
		}
	}
	t.Fatalf("tk query in %s has no row for %s", ns, id)
	return nil
}

// TestRolloutEndToEnd drives one central store from its preactivation shape
// through `tk activate` and then through the CLI and MCP surfaces together:
// Root capture, Root and project epics with leaves in other namespaces,
// identical bare IDs, closed, reopened and unreadable children, the global
// completion date, paginated membership, combined cycles and the destructive
// operations that must refuse.
func TestRolloutEndToEnd(t *testing.T) {
	root, stores := rolloutCentral(t)
	s := rolloutSession(t, root)

	// ── Before activation: the store behaves as it always has. ──
	if err := withProject(project.RootNamespace, func() error { return runCreateQuiet(t, "Too early", "") }); err == nil || !strings.Contains(err.Error(), ticket.FeatureRootNamespace) {
		t.Fatalf("CLI create in _root before activation = %v, want a refusal naming %s", err, ticket.FeatureRootNamespace)
	}
	rolloutRefused(t, s, "ticket_create", map[string]any{"title": "Too early", "type": "feature", "project": project.RootNamespace}, ticket.FeatureRootNamespace)
	rolloutRefused(t, s, "ticket_create", map[string]any{"title": "Foreign child", "type": "feature", "project": "loom", "parent": "warp/epic-1111"}, "another project")
	if err := withProject("loom", func() error { return runCreateQuiet(t, "Foreign child", "warp/epic-1111") }); err == nil || !strings.Contains(err.Error(), "another project") {
		t.Fatalf("CLI create with a foreign parent before activation = %v, want refused", err)
	}
	if v := showEpic(t, s, "warp/epic-1111"); v.status != "open" || !v.complete || fmt.Sprint(v.children) != "[warp/wchild-0001]" {
		t.Errorf("warp epic before activation = %+v, want open over its one local child", v)
	}
	if v := showEpic(t, s, "loom/epic-1111"); v.status != "backlog" || fmt.Sprint(v.children) != "[loom/lchild-0001]" {
		t.Errorf("loom epic before activation = %+v, want backlog over its one local child", v)
	}
	if row := queryRow(t, "warp", "warp/epic-1111"); row["status"] != "open" {
		t.Errorf("tk query reads the warp epic as %v, want open", row["status"])
	}
	tickets := ticketFiles(t, root)

	// ── Activation: dry run, the real one, and a re-run. ──
	tree := storeTree(t, root)
	out, err := runActivateWith(t, "dry-run", "true")
	if err != nil {
		t.Fatalf("tk activate --dry-run: %v", err)
	}
	for _, want := range []string{"Would activate", "root-namespace, cross-project-parents", "_root (root)", "code (legacy)", "loom (project)", "warp (project)", "Dry run: nothing was written."} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output should say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "anvil (") {
		t.Errorf("dry run re-catalogues anvil, which the catalog already holds:\n%s", out)
	}
	assertSameTree(t, "dry run", tree, storeTree(t, root))

	out, err = runActivateWith(t)
	if err != nil {
		t.Fatalf("tk activate: %v", err)
	}
	if !strings.Contains(out, "Activated") || !strings.Contains(out, "tk sync") {
		t.Errorf("activation output = %s", out)
	}
	cat, err := ticket.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range ticket.DefaultActivationFeatures {
		if !cat.Requires(f) {
			t.Errorf("activated catalog does not require %s: %+v", f, cat)
		}
	}
	for ns, kind := range map[string]ticket.NamespaceKind{project.RootNamespace: ticket.KindRoot, "warp": ticket.KindProject, "loom": ticket.KindProject, "code": ticket.KindLegacy, "anvil": ticket.KindLegacy} {
		if got := cat.Namespaces[ns].Kind; got != kind {
			t.Errorf("catalog kind of %s = %q, want %q", ns, got, kind)
		}
		if info, err := os.Lstat(filepath.Join(root, "tickets", ns, ticket.NamespaceMarker)); err != nil || !info.Mode().IsRegular() {
			t.Errorf("%s has no marker after activation (err %v)", ns, err)
		}
	}
	raw, _ := os.ReadFile(ticket.CatalogPath(root))
	if !strings.Contains(string(raw), "# Archived namespaces: retired products; tickets remain for history.") {
		t.Errorf("activation dropped the catalog's comment:\n%s", raw)
	}
	assertSameTree(t, "ticket files across activation", tickets, ticketFiles(t, root))
	cfg, err := project.Load()
	if err != nil {
		t.Fatal(err)
	}
	if inv, err := ticket.ReadInventory(root, cfg); err != nil || !inv.Complete {
		t.Errorf("inventory after activation: complete = %v, err = %v, diagnostics %q", inv.Complete, err, inv.Diagnostics)
	}
	tree = storeTree(t, root)
	if out, err := runActivateWith(t); err != nil || !strings.Contains(out, "already activated") {
		t.Errorf("re-running activation = %q, %v; want nothing to write", out, err)
	}
	assertSameTree(t, "re-run", tree, storeTree(t, root))
	// The ticket a pre-activation leaf names is still its own namespace's.
	if v := showEpic(t, s, "loom/epic-1111"); fmt.Sprint(v.children) != "[loom/lchild-0001]" {
		t.Errorf("loom epic after activation = %+v, want its own child alone", v)
	}

	// ── Root capture and a Root epic with leaves in two projects. ──
	if err := withProject(project.RootNamespace, func() error { return runCreateQuiet(t, "Explore cross-project planning", "") }); err != nil {
		t.Fatalf("CLI create in _root: %v", err)
	}
	idea := idByTitle(t, ticket.NewProjectFileStore(filepath.Join(root, "tickets", project.RootNamespace), project.RootNamespace), "Explore cross-project planning")
	if ns, _ := ticket.ParseNamespacedID(idea); ns != project.RootNamespace {
		t.Errorf("Root idea id = %s", idea)
	}
	rootEpic := rolloutCreate(t, s, map[string]any{"title": "Unified work", "type": "epic", "project": project.RootNamespace})
	if err := withProject("loom", func() error { return runCreateQuiet(t, "Loom leaf of the Root epic", rootEpic) }); err != nil {
		t.Fatalf("CLI create in loom under the Root epic: %v", err)
	}
	loomLeaf := idByTitle(t, stores["loom"], "Loom leaf of the Root epic")
	warpLeaf := rolloutCreate(t, s, map[string]any{"title": "Warp leaf of the Root epic", "type": "feature", "project": "warp", "parent": rootEpic})
	if v := showEpic(t, s, rootEpic); fmt.Sprint(v.children) != fmt.Sprint(sorted(loomLeaf, warpLeaf)) || !v.complete {
		t.Errorf("Root epic = %+v, want both foreign leaves", v)
	}

	// ── A project epic with a foreign child, beside a same-named epic. ──
	foreign := rolloutCreate(t, s, map[string]any{"title": "Loom child of the warp epic", "type": "feature", "project": "loom", "parent": "warp/epic-1111"})
	local := rolloutCreate(t, s, map[string]any{"title": "Loom child named bare", "type": "feature", "project": "loom", "parent": "epic-1111"})
	if parent := rolloutOK(t, s, "ticket_show", map[string]any{"id": local})["parent"]; parent != "loom/epic-1111" {
		t.Errorf("a bare parent in loom resolved to %v, want loom's own epic", parent)
	}
	if v := showEpic(t, s, "warp/epic-1111"); fmt.Sprint(v.children) != fmt.Sprint(sorted(foreign, "warp/wchild-0001")) {
		t.Errorf("warp epic children = %v, want its local child and the loom child", v.children)
	}
	if v := showEpic(t, s, "loom/epic-1111"); fmt.Sprint(v.children) != fmt.Sprint(sorted(local, "loom/lchild-0001")) {
		t.Errorf("loom epic children = %v, want loom's own children only", v.children)
	}

	// ── Closed, then terminal, then reopened, then done: the global rollup. ──
	rolloutEdit(t, s, foreign, map[string]any{"status": "closed"})
	if v := showEpic(t, s, "warp/epic-1111"); v.status != "open" || v.counts["closed"] != 1.0 || v.closed != "" {
		t.Errorf("warp epic with a closed foreign child and an open local one = %+v", v)
	}
	if err := runEditWith(t, "wchild-0001", "status", "done"); err != nil {
		t.Fatalf("CLI edit: %v", err)
	}
	v := showEpic(t, s, "warp/epic-1111")
	if latest := maxString(closedAt(t, s, foreign), closedAt(t, s, "warp/wchild-0001")); v.status != "closed" || v.closed != latest {
		t.Errorf("warp epic over a done and a closed child = %s closed at %q, want closed at the latest child completion %q", v.status, v.closed, latest)
	}
	rolloutEdit(t, s, foreign, map[string]any{"status": "open"})
	if v := showEpic(t, s, "warp/epic-1111"); v.status != "open" || v.closed != "" {
		t.Errorf("reopening the foreign child left the warp epic %s closed at %q, want open with no completion", v.status, v.closed)
	}
	rolloutEdit(t, s, foreign, map[string]any{"status": "done"})
	v = showEpic(t, s, "warp/epic-1111")
	if latest := maxString(closedAt(t, s, foreign), closedAt(t, s, "warp/wchild-0001")); v.status != "done" || v.closed != latest {
		t.Errorf("warp epic over two done children = %s closed at %q, want done at %q", v.status, v.closed, latest)
	}
	if row := queryRow(t, "warp", "warp/epic-1111"); row["status"] != "done" || row["closed"] != v.closed {
		t.Errorf("tk query reads the warp epic as %v closed at %v, MCP as done at %s", row["status"], row["closed"], v.closed)
	}

	// ── An unreadable file anywhere uncertifies the epic and every absence proof. ──
	broken := filepath.Join(stores["loom"].Dir, "broken.md")
	if err := os.WriteFile(broken, []byte("---\nid: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := showEpic(t, s, "warp/epic-1111"); v.status == "done" || v.complete || v.closed != "" {
		t.Errorf("warp epic over an unreadable loom file = %+v, want neither done nor complete", v)
	}
	if row := queryRow(t, "warp", "warp/epic-1111"); row["status"] == "done" {
		t.Errorf("tk query certifies the warp epic done over an unreadable loom file")
	}
	rolloutRefused(t, s, "ticket_delete", map[string]any{"id": idea}, "complete snapshot")
	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}
	if v := showEpic(t, s, "warp/epic-1111"); v.status != "done" || !v.complete {
		t.Errorf("warp epic once the file is gone = %+v, want done again", v)
	}

	// ── Paginated membership is one revision or a refusal. ──
	page := map[string]any{"parent": rootEpic, "all_projects": true, "include_closed": true, "limit": 1}
	first := rolloutOK(t, s, "ticket_list", page)
	token, _ := first["snapshot"].(string)
	if first["total"] != 2.0 || len(first["tickets"].([]any)) != 1 || token == "" {
		t.Fatalf("page 1 = %v, want one of two members and a token", first)
	}
	page["offset"], page["snapshot"] = 1, token
	second := rolloutOK(t, s, "ticket_list", page)
	got := sorted(first["tickets"].([]any)[0].(map[string]any)["id"].(string), second["tickets"].([]any)[0].(map[string]any)["id"].(string))
	if fmt.Sprint(got) != fmt.Sprint(sorted(loomLeaf, warpLeaf)) {
		t.Errorf("two pages = %v, want both members once", got)
	}
	rolloutEdit(t, s, warpLeaf, map[string]any{"status": "ready"})
	rolloutRefused(t, s, "ticket_list", page, "snapshot changed")

	// ── Cycles through deps and epic membership, across namespaces. ──
	// A child waiting on its own epic, and a loop through a foreign child that
	// is already done: terminal nodes are in the graph, so a reopen cannot
	// activate it later.
	rolloutRefused(t, s, "ticket_dep", map[string]any{"id": foreign, "dep_id": "warp/epic-1111", "action": "add"}, "would wait for itself")
	check := rolloutCreate(t, s, map[string]any{"title": "Integration check", "type": "feature", "project": "warp"})
	rolloutOK(t, s, "ticket_dep", map[string]any{"id": foreign, "dep_id": check, "action": "add"})
	_, bareCheck := ticket.ParseNamespacedID(check)
	if err := runDepWith(t, bareCheck, "epic-1111", "", false); err == nil || !strings.Contains(err.Error(), "would wait for itself") {
		t.Errorf("CLI dep closing %s -> warp/epic-1111 -> %s -> %s = %v, want a cycle refusal", check, foreign, check, err)
	}

	// ── Destructive operations refuse, and refusing writes nothing. ──
	before := ticketFiles(t, root)
	rolloutRefused(t, s, "ticket_delete", map[string]any{"id": "warp/epic-1111"}, "still referenced")
	refusal := rolloutRefused(t, s, "ticket_edit", map[string]any{"id": rootEpic, "status": "closed"}, "unfinished children in other projects")
	for _, id := range []string{loomLeaf, warpLeaf} {
		if _, bare := ticket.ParseNamespacedID(id); !strings.Contains(refusal, bare) {
			t.Errorf("abandon refusal should name %s: %s", id, refusal)
		}
	}
	rolloutRefused(t, s, "ticket_edit", map[string]any{"id": rootEpic, "type": "feature"}, "while it has children")
	_, bareLoomLeaf := ticket.ParseNamespacedID(loomLeaf)
	if err := withProject("loom", func() error { return runMove(moveCmd, []string{bareLoomLeaf, "warp"}) }); err == nil || !strings.Contains(err.Error(), "no move performed") {
		t.Errorf("tk move of a parented foreign leaf = %v, want refused", err)
	}
	assertSameTree(t, "refused destructive operations", before, ticketFiles(t, root))
}

func sorted(ids ...string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func maxString(a, b string) string {
	if a > b {
		return a
	}
	return b
}

// outsideTickets is where the symlinked-tickets case moves the store's real
// tickets directory: outside the central root, so a write through the link
// would land somewhere the store does not own. Its parent is the per-case
// directory outside the store, whose tree every case asserts unchanged.
var outsideTickets string

// TestActivateRefusesAndWritesNothing: every state activation cannot start
// from is refused by `tk activate` and its dry run alike, names the cause,
// and leaves every file in the store as it was.
func TestActivateRefusesAndWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string, stores map[string]*ticket.FileStore)
		flags []string
		want  string
	}{
		{
			name: "uncatalogued _root directory",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				dir := filepath.Join(root, "tickets", project.RootNamespace)
				os.MkdirAll(dir, 0o755)
				os.WriteFile(filepath.Join(dir, "theirs-0001.md"), []byte(legacyTicket), 0o644)
			},
			want: "not catalogued as Root",
		},
		{
			name: "config binding _root to a repository",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				cfg, err := project.Load()
				if err != nil {
					t.Fatal(err)
				}
				cfg.UpsertProject(project.RootNamespace, project.ProjectConfig{Path: t.TempDir(), Store: "central"})
				if err := project.Save(cfg); err != nil {
					t.Fatal(err)
				}
			},
			want: project.ErrRootBinding.Error(),
		},
		{
			name: "catalogued namespace with no directory",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				os.WriteFile(ticket.CatalogPath(root), []byte(rolloutCatalog+"  ghost:\n    kind: legacy\n"), 0o644)
			},
			want: `namespace "ghost" is catalogued but has no directory`,
		},
		{
			name: "invalid catalog entry",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				os.WriteFile(ticket.CatalogPath(root), []byte("namespaces:\n  warp: {kind: root}\n"), 0o644)
			},
			want: `namespace "warp" has kind "root"`,
		},
		{
			name: "malformed catalog",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				os.WriteFile(ticket.CatalogPath(root), []byte(":::{{"), 0o644)
			},
			want: "parsing catalog",
		},
		{
			name: "no tickets directory",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				os.RemoveAll(filepath.Join(root, "tickets"))
			},
			want: "no tickets directory",
		},
		{
			name: "tickets directory that is a symlink",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				tickets := filepath.Join(root, "tickets")
				if err := os.Rename(tickets, outsideTickets); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outsideTickets, tickets); err != nil {
					t.Fatal(err)
				}
			},
			want: "is a symlink — refusing to write outside the store",
		},
		{
			name: "catalog that is a symlink",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				secret := filepath.Join(filepath.Dir(outsideTickets), "kubeconfig.yaml")
				if err := os.WriteFile(secret, []byte(rolloutCatalog+"users:\n  - name: admin\n    token: dummy-secret-token\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				os.Remove(ticket.CatalogPath(root))
				if err := os.Symlink(secret, ticket.CatalogPath(root)); err != nil {
					t.Fatal(err)
				}
			},
			want: "is not a regular file",
		},
		{
			name: "namespace path that is not a directory",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				cfg, _ := project.Load()
				cfg.UpsertProject("linked", project.ProjectConfig{Path: t.TempDir(), Store: "central"})
				if err := project.Save(cfg); err != nil {
					t.Fatal(err)
				}
				os.Symlink(t.TempDir(), filepath.Join(root, "tickets", "linked"))
			},
			want: "refusing to write outside the store",
		},
		{
			name: "marker that is not a regular file",
			setup: func(t *testing.T, root string, stores map[string]*ticket.FileStore) {
				os.Mkdir(filepath.Join(stores["warp"].Dir, ticket.NamespaceMarker), 0o755)
			},
			want: "is not a regular file",
		},
		{
			name:  "requested feature this tk does not implement",
			flags: []string{"feature", "time-travel"},
			want:  `feature "time-travel" is not one this tk implements`,
		},
		{
			name: "catalog requiring a feature this tk does not implement",
			setup: func(t *testing.T, root string, _ map[string]*ticket.FileStore) {
				os.WriteFile(ticket.CatalogPath(root), []byte("required_features: [time-travel]\n"+rolloutCatalog), 0o644)
			},
			want: "replace this binary",
		},
		{
			name: "legacy ticket already naming a foreign parent",
			setup: func(t *testing.T, root string, stores map[string]*ticket.FileStore) {
				os.WriteFile(filepath.Join(stores["loom"].Dir, "stray-0001.md"), []byte("---\nid: stray-0001\nstatus: open\ntype: feature\nparent: warp/epic-1111\n---\n# Stray\n"), 0o644)
			},
			want: "loom/stray-0001 (parent warp/epic-1111)",
		},
		{
			name: "unreadable ticket file",
			setup: func(t *testing.T, root string, stores map[string]*ticket.FileStore) {
				os.WriteFile(filepath.Join(stores["loom"].Dir, "broken.md"), []byte("---\nid: [\n"), 0o644)
			},
			want: "needs a complete read of the store",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outside := t.TempDir()
			outsideTickets = filepath.Join(outside, "tickets")
			root, stores := rolloutCentral(t)
			if tc.setup != nil {
				tc.setup(t, root, stores)
			}
			tree := storeTree(t, root)
			outsideTree := storeTree(t, outside)
			for _, dryRun := range []string{"true", "false"} {
				out, err := runActivateWith(t, append([]string{"dry-run", dryRun}, tc.flags...)...)
				if !errors.Is(err, ticket.ErrActivationRefused) {
					t.Fatalf("dry-run=%s: want ErrActivationRefused, got %v (output %q)", dryRun, err, out)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("dry-run=%s: refusal should say %q, got: %v", dryRun, tc.want, err)
				}
				assertSameTree(t, "refused activation (dry-run="+dryRun+")", tree, storeTree(t, root))
				assertSameTree(t, "outside the store (dry-run="+dryRun+")", outsideTree, storeTree(t, outside))
			}
		})
	}
}

// TestActivateOneFeatureLeavesTheOtherOff: --feature turns on only what it
// names. Root alone does not consult foreign parents, so a legacy one is no
// refusal and stays no epic's child; turning cross-project parents on later
// is refused until it is cleared.
func TestActivateOneFeatureLeavesTheOtherOff(t *testing.T) {
	root, stores := rolloutCentral(t)
	stray := filepath.Join(stores["loom"].Dir, "stray-0001.md")
	if err := os.WriteFile(stray, []byte("---\nid: stray-0001\nstatus: open\ntype: feature\nparent: warp/epic-1111\n---\n# Stray\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runActivateWith(t, "feature", ticket.FeatureRootNamespace); err != nil {
		t.Fatalf("tk activate --feature root-namespace: %v", err)
	}
	cat, err := ticket.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cat.RootActivated() || cat.CrossProjectParentsActivated() {
		t.Fatalf("catalog features = %v, want root-namespace alone", cat.RequiredFeatures)
	}
	s := rolloutSession(t, root)
	if v := showEpic(t, s, "warp/epic-1111"); fmt.Sprint(v.children) != "[warp/wchild-0001]" {
		t.Errorf("warp epic adopted the legacy foreign parent without cross-project parents: %v", v.children)
	}

	if _, err := runActivateWith(t); err == nil || !strings.Contains(err.Error(), "loom/stray-0001") {
		t.Fatalf("activating cross-project parents over a legacy foreign parent = %v, want refused naming it", err)
	}
	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}
	out, err := runActivateWith(t)
	if err != nil {
		t.Fatalf("tk activate once the stray is gone: %v", err)
	}
	if !strings.Contains(out, "required features added: "+ticket.FeatureCrossProjectParents+"\n") || !strings.Contains(out, "namespaces catalogued:   none") {
		t.Errorf("second activation should add the one feature and nothing else:\n%s", out)
	}
}

// TestBinaryRollbackAcrossActivation: before activation a binary that knows no
// feature writes as it always did, so rolling the binary back is safe; after
// it, a guarded binary lacking a required feature refuses every write through
// every surface, activation included, and still reads.
func TestBinaryRollbackAcrossActivation(t *testing.T) {
	root, stores := rolloutCentral(t)
	s := rolloutSession(t, root)
	supported := ticket.SupportedFeatures
	t.Cleanup(func() { ticket.SupportedFeatures = supported })

	ticket.SupportedFeatures = nil
	rolloutEdit(t, s, "warp/wchild-0001", map[string]any{"title": "Written by an older binary"})
	if err := runEditWith(t, "wchild-0001", "title", "Still writable before activation"); err != nil {
		t.Fatalf("CLI edit before activation with no features supported: %v", err)
	}

	ticket.SupportedFeatures = supported
	if _, err := runActivateWith(t); err != nil {
		t.Fatalf("tk activate: %v", err)
	}

	// A guarded binary from before cross-project parents.
	ticket.SupportedFeatures = []string{ticket.FeatureRootNamespace}
	tree := storeTree(t, root)
	assertUnsupported := func(what string, err error) {
		t.Helper()
		var unsupported *ticket.UnsupportedFeatureError
		if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), ticket.FeatureCrossProjectParents) {
			t.Errorf("%s after activation on a binary lacking %s = %v, want UnsupportedFeatureError", what, ticket.FeatureCrossProjectParents, err)
		}
	}
	assertUnsupported("CLI create", runCreateQuiet(t, "After rollback", ""))
	assertUnsupported("CLI edit", runEditWith(t, "wchild-0001", "status", "done"))
	_, err := runActivateWith(t)
	assertUnsupported("tk activate", err)
	assertUnsupported("library update", func() error {
		tk, err := stores["loom"].Get("lchild-0001")
		if err != nil {
			return err
		}
		tk.Title = "changed"
		return stores["loom"].Update(tk)
	}())
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"ticket_create", map[string]any{"title": "After rollback", "type": "feature", "project": "warp"}},
		{"ticket_edit", map[string]any{"id": "warp/wchild-0001", "status": "done"}},
		{"ticket_add_note", map[string]any{"id": "warp/wchild-0001", "text": "note"}},
		{"ticket_delete", map[string]any{"id": "loom/lchild-0001"}},
	} {
		rolloutRefused(t, s, call.name, call.args, "replace this binary")
	}
	assertSameTree(t, "writes refused after activation", tree, storeTree(t, root))

	// Reads are not gated: the rolled-back binary still sees the store.
	if v := showEpic(t, s, "warp/epic-1111"); v.status != "open" {
		t.Errorf("read after rollback = %+v", v)
	}
	if row := queryRow(t, "warp", "warp/wchild-0001"); row["title"] != "Still writable before activation" {
		t.Errorf("tk query after rollback = %v", row)
	}
}
