package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// crossProjectCentral is a central store with two registered projects — warp
// at the test process's working directory, loom elsewhere — and a catalog
// requiring cross-project-parents, so a leaf in one may name an epic in the
// other. Returns the central root and a store per project.
func crossProjectCentral(t *testing.T) (string, map[string]*ticket.FileStore) {
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
	if err := os.WriteFile(ticket.CatalogPath(root), []byte("required_features: [cross-project-parents]\nnamespaces:\n  warp: {kind: project}\n  loom: {kind: project}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stores := map[string]*ticket.FileStore{}
	for _, ns := range []string{"warp", "loom"} {
		dir := filepath.Join(root, "tickets", ns)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		stores[ns] = ticket.NewProjectFileStore(dir, ns)
	}
	return root, stores
}

// activateRoot writes the catalog that activates Root and makes its directory.
func activateRoot(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(ticket.CatalogPath(root), []byte("required_features: [root-namespace]\nnamespaces:\n  _root: {kind: root}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tickets", project.RootNamespace), 0o755); err != nil {
		t.Fatal(err)
	}
}

// chdir moves the process into dir for the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

// runCreateQuiet runs the create command with stdout discarded — a successful
// create prints the ticket — and the parent flag set when given.
func runCreateQuiet(t *testing.T, title, parent string) error {
	t.Helper()
	f := createCmd.Flags()
	if err := f.Set("parent", parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Set("parent", "") })
	oldStdout := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = devNull
	defer func() {
		os.Stdout = oldStdout
		devNull.Close()
	}()
	return runCreate(createCmd, []string{title})
}

func onlyTicket(t *testing.T, store *ticket.FileStore) *ticket.Ticket {
	t.Helper()
	tickets, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("%s holds %d tickets, want 1", store.Dir, len(tickets))
	}
	return tickets[0]
}

func TestProjectSelectorSelectsRootFromARegisteredRepo(t *testing.T) {
	store := centralStore(t, "pr-repo")
	root := filepath.Dir(filepath.Dir(store.Dir))
	if err := os.WriteFile(ticket.CatalogPath(root), []byte("namespaces:\n  pr-repo: {kind: project}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selectProject(t, project.RootNamespace)

	dir, name, _, err := resolveTicketsDir()
	if err != nil {
		t.Fatalf("--project _root did not resolve: %v", err)
	}
	if want := filepath.Join(root, "tickets", project.RootNamespace); dir != want || name != project.RootNamespace {
		t.Errorf("resolution = (%q, %q), want (%q, _root)", dir, name, want)
	}

	// Selectable is not writable: the catalog guard decides that.
	err = runCreateQuiet(t, "An idea", "")
	if err == nil || !contains(err.Error(), ticket.FeatureRootNamespace) {
		t.Fatalf("create in _root before activation = %v, want a refusal naming %s", err, ticket.FeatureRootNamespace)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("a refused create left %s behind", dir)
	}

	activateRoot(t, root)
	if err := runCreateQuiet(t, "An idea", ""); err != nil {
		t.Fatalf("create in _root after activation: %v", err)
	}
	created := onlyTicket(t, ticket.NewProjectFileStore(dir, project.RootNamespace))
	if created.Title != "An idea" {
		t.Errorf("Root holds %q, want the created ticket", created.Title)
	}
	if tickets, _ := store.List(); len(tickets) != 0 {
		t.Errorf("the repo's own project received the Root ticket: %v", tickets)
	}
}

func TestProjectSelectorSelectsRootFromNoProject(t *testing.T) {
	home := setupTestHome(t)
	root := filepath.Join(home, "central")
	if err := project.Save(project.Config{CentralRoot: root}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	activateRoot(t, root)
	chdir(t, t.TempDir())
	selectProject(t, project.RootNamespace)

	if err := runCreateQuiet(t, "An idea from nowhere", ""); err != nil {
		t.Fatalf("create with --project _root outside any project: %v", err)
	}
	rootStore := ticket.NewProjectFileStore(filepath.Join(root, "tickets", project.RootNamespace), project.RootNamespace)
	if got := onlyTicket(t, rootStore).Title; got != "An idea from nowhere" {
		t.Errorf("Root holds %q", got)
	}
}

func TestProjectSelectorRefusesRepoTogether(t *testing.T) {
	centralStore(t, "pr-both")
	selectProject(t, "pr-both")
	repoFlag = mustGetwd()
	defer func() { repoFlag = "" }()

	_, _, _, err := resolveTicketsDir()
	if err == nil || !contains(err.Error(), "--project and --repo are both selectors") {
		t.Fatalf("--project with --repo = %v, want the conflict refused", err)
	}
}

func TestProjectSelectorRefusesAnUnknownNamespace(t *testing.T) {
	centralStore(t, "pr-known")
	selectProject(t, "pr-nope")

	_, _, _, err := resolveTicketsDir()
	if err == nil || !contains(err.Error(), `namespace "pr-nope" is not in the central store`) {
		t.Fatalf("--project pr-nope = %v, want it refused as unknown", err)
	}
	// Selectable by directory alone, and by catalog alone: a namespace the
	// config never registered still holds tickets somebody wrote.
	root := filepath.Dir(filepath.Dir(mustResolveDir(t, "pr-known")))
	if err := os.MkdirAll(filepath.Join(root, "tickets", "pr-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ticket.CatalogPath(root), []byte("namespaces:\n  pr-cat: {kind: legacy}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ns := range []string{"pr-dir", "pr-cat"} {
		selectProject(t, ns)
		if _, name, _, err := resolveTicketsDir(); err != nil || name != ns {
			t.Errorf("--project %s = (%q, %v), want it selectable", ns, name, err)
		}
	}
}

// mustResolveDir is the tickets directory --project ns selects.
func mustResolveDir(t *testing.T, ns string) string {
	t.Helper()
	selectProject(t, ns)
	dir, _, _, err := resolveTicketsDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCreateOutsideARecognizedProjectRequiresADestination(t *testing.T) {
	home := setupTestHome(t)
	root := filepath.Join(home, "central")
	if err := project.Save(project.Config{CentralRoot: root}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	// A central directory named for this directory exists, so a read would
	// fall back to it with a warning; a create does not.
	stray := t.TempDir()
	strayDir := filepath.Join(root, "tickets", filepath.Base(stray))
	if err := os.MkdirAll(strayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, stray)

	err := runCreateQuiet(t, "Homeless", "")
	for _, want := range []string{"not inside a registered project", "--project <namespace>", "--project _root", "tk init"} {
		if err == nil || !contains(err.Error(), want) {
			t.Errorf("create outside a registered project = %v, want a refusal containing %q", err, want)
		}
	}
	if entries, _ := os.ReadDir(strayDir); len(entries) != 0 {
		t.Errorf("the refused create wrote into %s: %v", strayDir, entries)
	}
}

func TestCreateForeignChildThroughProjectSelector(t *testing.T) {
	_, stores := crossProjectCentral(t)
	mkCentral(t, stores["warp"], "epic-x", ticket.TypeEpic, ticket.StatusBacklog, "")
	selectProject(t, "loom")

	if err := runCreateQuiet(t, "Loom's part of warp's epic", "warp/epic-x"); err != nil {
		t.Fatalf("create --project loom --parent warp/epic-x: %v", err)
	}
	child := onlyTicket(t, stores["loom"])
	if child.Parent != "warp/epic-x" {
		t.Errorf("child parent = %q, want warp/epic-x", child.Parent)
	}
	snap, err := stores["warp"].Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	children := snap.Children("warp/epic-x")
	if len(children) != 1 || children[0].ID != "loom/"+child.ID {
		t.Errorf("epic children = %v, want the loom child", children)
	}
}

// captureLsWithStderr runs ls with the given flags and returns stdout and
// stderr. "project" among the flags sets the persistent selector.
func captureLsWithStderr(t *testing.T, flags ...string) (string, string, error) {
	t.Helper()
	selectProject(t, "")
	f := lsCmd.Flags()
	// Flag values persist on the shared command, so every flag a call can set
	// is restored to its default first.
	for _, name := range []string{"parent", "all", "all-projects"} {
		if err := f.Set(name, f.Lookup(name).DefValue); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Set(name, f.Lookup(name).DefValue) })
	}
	for i := 0; i+1 < len(flags); i += 2 {
		if flags[i] == "project" {
			projectFlag = flags[i+1]
			continue
		}
		if err := f.Set(flags[i], flags[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW
	err := runLs(lsCmd, nil)
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	out, _ := io.ReadAll(outR)
	errOut, _ := io.ReadAll(errR)
	return string(out), string(errOut), err
}

func TestLsParentMembershipIsGlobalAndTheListingIsNarrowed(t *testing.T) {
	_, stores := crossProjectCentral(t)
	mkCentral(t, stores["warp"], "epic-x", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, stores["warp"], "w-child", ticket.TypeFeature, ticket.StatusOpen, "epic-x")
	mkCentral(t, stores["warp"], "w-closed", ticket.TypeFeature, ticket.StatusClosed, "epic-x")
	mkCentral(t, stores["loom"], "l-child", ticket.TypeFeature, ticket.StatusOpen, "warp/epic-x")
	mkCentral(t, stores["loom"], "l-other", ticket.TypeFeature, ticket.StatusOpen, "")

	out, errOut, err := captureLsWithStderr(t, "project", "warp", "parent", "epic-x")
	if err != nil {
		t.Fatalf("ls --project warp --parent epic-x: %v", err)
	}
	if !contains(out, "w-child") || contains(out, "l-child") || contains(out, "w-closed") {
		t.Errorf("ls --parent in warp should list warp's open child alone:\n%s", out)
	}
	if want := "children of warp/epic-x: 3 across namespaces, 2 in warp (--all-projects lists them all)"; !contains(errOut, want) {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}

	// --all keeps the closed child in; membership is unchanged.
	out, _, err = captureLsWithStderr(t, "project", "warp", "parent", "epic-x", "all", "true")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "w-closed") || !contains(out, "w-child") {
		t.Errorf("ls --parent --all should list the closed child:\n%s", out)
	}

	// Every namespace, IDs qualified, no slice line.
	out, errOut, err = captureLsWithStderr(t, "all-projects", "true", "parent", "warp/epic-x")
	if err != nil {
		t.Fatalf("ls --all-projects --parent warp/epic-x: %v", err)
	}
	for _, want := range []string{"warp/w-child", "loom/l-child"} {
		if !contains(out, want) {
			t.Errorf("ls --all-projects missing %s:\n%s", want, out)
		}
	}
	if contains(out, "l-other") || contains(errOut, "children of") {
		t.Errorf("ls --all-projects listed a non-child or printed the slice line:\n%s\n%s", out, errOut)
	}

	// An unresolvable parent is an error, not an empty listing.
	if _, _, err := captureLsWithStderr(t, "project", "warp", "parent", "nope-0000"); err == nil || !contains(err.Error(), "not found") {
		t.Errorf("ls --parent nope-0000 = %v, want not found", err)
	}
	if _, _, err := captureLsWithStderr(t, "all-projects", "true", "parent", "epic-x"); err == nil || !contains(err.Error(), "must be qualified") {
		t.Errorf("ls --all-projects --parent epic-x = %v, want the bare parent refused", err)
	}
	if _, _, err := captureLsWithStderr(t, "project", "warp", "parent", "w-child"); err == nil || !contains(err.Error(), "not an epic") {
		t.Errorf("ls --parent w-child = %v, want the leaf refused as a parent", err)
	}
	if _, _, err := captureLsWithStderr(t, "project", "warp", "all-projects", "true"); err == nil || !contains(err.Error(), "conflict") {
		t.Errorf("ls --project --all-projects = %v, want the conflict refused", err)
	}
}

func TestShowEpicProgressMatchesDerivedStatus(t *testing.T) {
	_, stores := crossProjectCentral(t)
	mkCentral(t, stores["warp"], "epic-p", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, stores["warp"], "p-done", ticket.TypeFeature, ticket.StatusDone, "epic-p")
	mkCentral(t, stores["loom"], "p-open", ticket.TypeFeature, ticket.StatusOpen, "warp/epic-p")
	mkCentral(t, stores["loom"], "p-closed", ticket.TypeFeature, ticket.StatusClosed, "warp/epic-p")

	out := captureShow(t, stores["warp"], "epic-p", false)
	for _, want := range []string{
		"status: open",
		"## Progress",
		"children: 3 — done 1, closed 1, open 1, ready 0, backlog 0",
		"complete: yes",
		"- loom/p-open [open]",
		"- warp/p-done [done]",
	} {
		if !contains(out, want) {
			t.Errorf("show missing %q:\n%s", want, out)
		}
	}

	// The foreign child's parent line carries the epic's title, and a leaf
	// with a valid relationship has no Relationship section.
	out = captureShow(t, stores["loom"], "p-open", false)
	if !contains(out, "parent: warp/epic-p  # Ticket epic-p") {
		t.Errorf("foreign parent not annotated:\n%s", out)
	}
	if contains(out, "## Relationship") {
		t.Errorf("a valid leaf printed a Relationship section:\n%s", out)
	}
}

func TestShowLeafRelationshipIssue(t *testing.T) {
	store := centralStore(t, "sh-issue")
	mkCentral(t, store, "epic-r", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, store, "child-r", ticket.TypeFeature, ticket.StatusOpen, "epic-r")
	// Legacy state: the parent names a project that is not there, which no
	// write produces and which the graph reports rather than places.
	path := filepath.Join(store.Dir, "child-r.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "parent: sh-issue/epic-r", "parent: gone/epic-r", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureShow(t, store, "child-r", false)
	if !contains(out, "## Relationship") || !contains(out, "gone/epic-r") {
		t.Errorf("show did not report the relationship issue:\n%s", out)
	}
}

func TestFrontierParentKeepsTheEpicsChildrenInEveryNamespace(t *testing.T) {
	_, stores := crossProjectCentral(t)
	mkCentral(t, stores["warp"], "epic-f", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, stores["warp"], "f-warp", ticket.TypeFeature, ticket.StatusReady, "epic-f")
	mkCentral(t, stores["loom"], "f-loom", ticket.TypeFeature, ticket.StatusReady, "warp/epic-f")
	mkCentral(t, stores["loom"], "f-free", ticket.TypeFeature, ticket.StatusReady, "")

	out := captureFrontier(t, "parent", "warp/epic-f")
	for _, want := range []string{"warp/f-warp", "loom/f-loom"} {
		if !contains(out, want) {
			t.Errorf("frontier --parent missing %s:\n%s", want, out)
		}
	}
	if contains(out, "f-free") {
		t.Errorf("frontier --parent listed a ticket outside the epic:\n%s", out)
	}

	// A bare parent names --project's namespace, and --project narrows the
	// rows after the membership is decided.
	out = captureFrontier(t, "project", "warp", "parent", "epic-f")
	if !contains(out, "warp/f-warp") || contains(out, "f-loom") {
		t.Errorf("frontier --project warp --parent epic-f should list warp's child alone:\n%s", out)
	}

	selectProject(t, "")
	if err := frontierCmd.Flags().Set("parent", "epic-f"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = frontierCmd.Flags().Set("parent", "") })
	if err := runFrontier(frontierCmd, nil); err == nil || !contains(err.Error(), "must be qualified") {
		t.Errorf("frontier --parent epic-f without --project = %v, want the bare parent refused", err)
	}
	if err := frontierCmd.Flags().Set("parent", "warp/nope-0000"); err != nil {
		t.Fatal(err)
	}
	if err := runFrontier(frontierCmd, nil); err == nil || !contains(err.Error(), "not found") {
		t.Errorf("frontier --parent warp/nope-0000 = %v, want not found", err)
	}
	if err := frontierCmd.Flags().Set("parent", "warp/f-warp"); err != nil {
		t.Fatal(err)
	}
	if err := runFrontier(frontierCmd, nil); err == nil || !contains(err.Error(), "not an epic") {
		t.Errorf("frontier --parent warp/f-warp = %v, want the leaf refused as a parent", err)
	}
}

func TestSearchAndQueryAllProjects(t *testing.T) {
	_, stores := crossProjectCentral(t)
	mkCentral(t, stores["warp"], "sq-warp", ticket.TypeFeature, ticket.StatusOpen, "")
	mkCentral(t, stores["loom"], "sq-loom", ticket.TypeFeature, ticket.StatusOpen, "")

	for name, run := range map[string]func() error{
		"search": func() error { return runSearch(searchCmd, []string{"Ticket"}) },
		"query":  func() error { return runQuery(queryCmd, nil) },
	} {
		cmd := searchCmd
		if name == "query" {
			cmd = queryCmd
		}
		if err := cmd.Flags().Set("all-projects", "true"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Flags().Set("all-projects", "false") })

		selectProject(t, "")
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		err := run()
		w.Close()
		os.Stdout = oldStdout
		out, _ := io.ReadAll(r)
		if err != nil {
			t.Fatalf("%s --all-projects: %v", name, err)
		}
		for _, want := range []string{"warp/sq-warp", "loom/sq-loom"} {
			if !contains(string(out), want) {
				t.Errorf("%s --all-projects missing %s:\n%s", name, want, out)
			}
		}

		selectProject(t, "loom")
		if err := run(); err == nil || !contains(err.Error(), "conflict") {
			t.Errorf("%s --all-projects --project loom = %v, want the conflict refused", name, err)
		}
	}
}
