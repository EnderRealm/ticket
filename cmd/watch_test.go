package cmd

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/journal"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// TestWatchCycleWritesOnlyToCentralProjects holds the watch cycle to the one
// store tk resolves. An old `store: local` entry is not a registration, so it
// gets no store at all: the commit that would auto-close a ticket in a
// .tickets/ beside that repo leaves the file exactly as it was, while the
// registered project's ticket is closed in the same cycle — without which this
// test would pass on a watcher that ran nothing.
//
// `store: local` rather than an absent store because only a non-empty value
// survives into the shared config, which is where the auto flags live.
func TestWatchCycleWritesOnlyToCentralProjects(t *testing.T) {
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")

	centralRepo := filepath.Join(home, "wt-central")
	legacyRepo := filepath.Join(home, "wt-legacy")
	for _, dir := range []string{centralRepo, legacyRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", "test@test.com")
		runGit(t, dir, "config", "user.name", "test")
	}

	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"wt-central": {Path: centralRepo, Store: "central", AutoLink: true, AutoClose: true},
			"wt-legacy":  {Path: legacyRepo, Store: "local", AutoLink: true, AutoClose: true},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	central := ticket.NewProjectFileStore(filepath.Join(centralRoot, "tickets", "wt-central"), "wt-central")
	mkCentral(t, central, "wt-central-0001", ticket.TypeFeature, ticket.StatusOpen, "")

	legacyDir := filepath.Join(legacyRepo, ".tickets")
	legacyStore := ticket.NewFileStore(legacyDir)
	mkCentral(t, legacyStore, "wt-legacy-0002", ticket.TypeFeature, ticket.StatusOpen, "")
	legacyFile := filepath.Join(legacyDir, "wt-legacy-0002.md")
	before, err := os.ReadFile(legacyFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	commitClosing(t, centralRepo, "wt-central-0001")
	commitClosing(t, legacyRepo, "wt-legacy-0002")

	watchOnce = true
	watchInterval = time.Second
	t.Cleanup(func() { watchOnce = false })

	if err := runWatchRun(watchRunCmd, nil); err != nil {
		t.Fatalf("runWatchRun: %v", err)
	}

	closed, err := central.Get("wt-central-0001")
	if err != nil {
		t.Fatalf("Get central ticket: %v", err)
	}
	if closed.Status != ticket.StatusDone {
		t.Errorf("central ticket status = %q, want %q — the cycle wrote nothing at all", closed.Status, ticket.StatusDone)
	}

	after, err := os.ReadFile(legacyFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("the watcher wrote into a .tickets/:\n%s", string(after))
	}
}

// The "no store" watchStoreFor returns for an unregistered project has to be a
// nil interface rather than a nil *FileStore carried in one: RunWatchCycle
// decides whether to auto-close on `store != nil`, which a typed nil passes, so
// a typed one would send every unregistered project's auto-close into a store
// with no directory.
func TestWatchStoreForUnregisteredProjectIsANilInterface(t *testing.T) {
	home := setupTestHome(t)
	cfg := project.Config{
		CentralRoot: filepath.Join(home, "central"),
		Projects: map[string]project.ProjectConfig{
			"ws-central": {Store: "central"},
			"ws-legacy":  {Store: "local"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	store, err := watchStoreFor(cfg, "ws-legacy")
	if err != nil {
		t.Fatalf("watchStoreFor: %v", err)
	}
	if store != nil {
		t.Errorf("store = %#v, want a nil interface", store)
	}

	store, err = watchStoreFor(cfg, "ws-central")
	if err != nil {
		t.Fatalf("watchStoreFor: %v", err)
	}
	want := filepath.Join(home, "central", "tickets", "ws-central")
	if fs, ok := store.(*ticket.FileStore); !ok || fs.Dir != want || fs.Project != "ws-central" {
		t.Errorf("store = %#v, want a project-scoped store over %q", store, want)
	}
}

// The journal loop joins a config map key into the central store path, and the
// shared config replicates from other machines, so a key like "../../elsewhere"
// would resolve the store an auto-close writes into outside the central tickets
// root. The escaped directory is seeded with the ticket the commit closes, so a
// loop without the bound rewrites a file no project owns; the valid project
// closed in the same cycle proves the loop ran at all rather than stopping on
// the bad key.
func TestWatchCycleRefusesATraversingProjectKey(t *testing.T) {
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")

	escapeRepo := filepath.Join(home, "wt-escape")
	validRepo := filepath.Join(home, "wt-valid")
	for _, dir := range []string{escapeRepo, validRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", "test@test.com")
		runGit(t, dir, "config", "user.name", "test")
	}

	escapeName := "../../elsewhere"
	escapedDir := filepath.Join(centralRoot, "tickets", escapeName)
	escaped := ticket.NewFileStore(escapedDir)
	mkCentral(t, escaped, "esc-0001", ticket.TypeFeature, ticket.StatusOpen, "")
	escapedFile := filepath.Join(escapedDir, "esc-0001.md")
	before, err := os.ReadFile(escapedFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	valid := ticket.NewProjectFileStore(filepath.Join(centralRoot, "tickets", "wt-valid"), "wt-valid")
	mkCentral(t, valid, "wt-valid-0001", ticket.TypeFeature, ticket.StatusOpen, "")

	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			escapeName: {Path: escapeRepo, Store: "central", AutoLink: true, AutoClose: true},
			"wt-valid": {Path: validRepo, Store: "central", AutoLink: true, AutoClose: true},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	commitClosing(t, escapeRepo, "esc-0001")
	commitClosing(t, validRepo, "wt-valid-0001")

	watchOnce = true
	watchInterval = time.Second
	t.Cleanup(func() { watchOnce = false })

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if err := runWatchRun(watchRunCmd, nil); err != nil {
		t.Fatalf("runWatchRun: %v", err)
	}

	closed, err := valid.Get("wt-valid-0001")
	if err != nil {
		t.Fatalf("Get valid ticket: %v", err)
	}
	if closed.Status != ticket.StatusDone {
		t.Fatalf("valid ticket status = %q, want %q — the cycle wrote nothing at all", closed.Status, ticket.StatusDone)
	}

	after, err := os.ReadFile(escapedFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("the watcher wrote outside the central tickets root, into %s:\n%s", escapedFile, string(after))
	}
	if out := logs.String(); !contains(out, "invalid project name") {
		t.Errorf("the refused project was skipped with no report:\n%s", out)
	}
}

func TestWatchCycleQuotesProjectNamesInLogs(t *testing.T) {
	home := setupTestHome(t)
	name := "ordinary\nFORGED\x1b[31m"
	cfg := project.Config{
		CentralRoot: filepath.Join(home, "central"),
		Projects: map[string]project.ProjectConfig{
			name: {
				Path:      filepath.Join(home, "missing"),
				Store:     "central",
				AutoLink:  true,
				AutoClose: true,
			},
		},
		JournalDefaultsMigrated: true,
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	watchOnce = true
	watchInterval = time.Second
	t.Cleanup(func() { watchOnce = false })

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if err := runWatchRun(watchRunCmd, nil); err != nil {
		t.Fatalf("runWatchRun: %v", err)
	}

	out := logs.String()
	if contains(out, "\nFORGED") || contains(out, "\x1b") {
		t.Fatalf("project name injected control bytes into the log:\n%q", out)
	}
	if want := `"ordinary\nFORGED\x1b[31m"`; !contains(out, want) {
		t.Errorf("log missing quoted project name %q:\n%s", want, out)
	}
}

// TestWatchRunMigratesJournalDefaultsAndJournals is the wiring `tk watch run`
// depends on: a project registered with both flags false — what `tk init` wrote
// for every project on the machine — is flipped on and journalled by the same
// run, and the marker lands in the shared config so the flip is once-ever.
// Without it the watcher walks past every project and the commit journal stays
// empty.
func TestWatchRunMigratesJournalDefaultsAndJournals(t *testing.T) {
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")

	repo := filepath.Join(home, "wm-proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@test.com")
	runGit(t, repo, "config", "user.name", "test")

	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"wm-proj": {Path: repo, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	central := ticket.NewProjectFileStore(filepath.Join(centralRoot, "tickets", "wm-proj"), "wm-proj")
	mkCentral(t, central, "wm-proj-0001", ticket.TypeFeature, ticket.StatusOpen, "")
	commitClosing(t, repo, "wm-proj-0001")

	watchOnce = true
	watchInterval = time.Second
	t.Cleanup(func() { watchOnce = false })

	if err := runWatchRun(watchRunCmd, nil); err != nil {
		t.Fatalf("runWatchRun: %v", err)
	}

	shared, err := os.ReadFile(filepath.Join(centralRoot, "config.yaml"))
	if err != nil {
		t.Fatalf("read shared config: %v", err)
	}
	if !contains(string(shared), "journal_defaults_migrated: true") {
		t.Errorf("shared config carries no marker, so the flip would re-run:\n%s", string(shared))
	}

	reloaded, err := project.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p := reloaded.Projects["wm-proj"]; !p.AutoLink || !p.AutoClose {
		t.Errorf("wm-proj = %+v, want both flags flipped on", p)
	}

	entries, err := journal.ReadEntries("wm-proj")
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Ticket != "wm-proj-0001" {
		t.Fatalf("journal = %+v, want the commit journalled in the same run", entries)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	statusErr := runWatchStatus(watchStatusCmd, nil)
	w.Close()
	os.Stdout = oldStdout
	if statusErr != nil {
		t.Fatalf("runWatchStatus: %v", statusErr)
	}
	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	if out := string(buf[:n]); !contains(out, "wm-proj                  auto_link=true auto_close=true auto_retrospect=false") {
		t.Errorf("watch status does not list the project's flags:\n%s", out)
	}
}

// A config that will not load is exactly when someone asks whether the watcher
// is running, and the pid file answers that on its own — so status reports the
// error and drops the project list rather than failing outright.
func TestWatchStatusDegradesOnUnreadableConfig(t *testing.T) {
	home := setupTestHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".ticket"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ticket", "config.yaml"), []byte("projects:\n  <<<<<<< HEAD\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runWatchStatus(watchStatusCmd, nil)
	w.Close()
	os.Stdout = oldStdout
	if err != nil {
		t.Fatalf("runWatchStatus: %v", err)
	}

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	for _, want := range []string{"watch is not running", "log:", "config error:"} {
		if !contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Without a central root there is nowhere durable to record the marker, so the
// migration does not run at all: flipping the flags anyway would persist them
// through the local path with no marker beside them, re-running the flip on
// every start and re-enabling journaling a user turned off.
func TestMigrateJournalDefaultsWithoutCentralRootIsANoOp(t *testing.T) {
	home := setupTestHome(t)

	cfg := project.Config{
		Projects: map[string]project.ProjectConfig{
			"nr-proj": {Path: filepath.Join(home, "nr-proj")},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	migrateJournalDefaults(&cfg)

	if cfg.JournalDefaultsMigrated {
		t.Error("marker set with no shared config to hold it")
	}
	if p := cfg.Projects["nr-proj"]; p.AutoLink || p.AutoClose {
		t.Errorf("nr-proj = %+v, want the flags untouched", p)
	}
}

// The summary has to name the same set the cycle loops process, or a
// retrospect-only project reads as skipped while being processed every tick —
// and since the string is the reload change-detection key, toggling the flag
// would never log a config change either.
func TestJournalingSummaryCountsRetrospectOnlyProjects(t *testing.T) {
	cfg := project.Config{
		Projects: map[string]project.ProjectConfig{
			"js-journal":    {AutoLink: true, AutoClose: true},
			"js-retrospect": {AutoRetrospect: true},
			"js-inert":      {},
		},
	}

	summary := journalingSummary(cfg)
	if want := "projects=3 journaling=2 skipped=[js-inert]"; summary != want {
		t.Errorf("journalingSummary = %q, want %q", summary, want)
	}

	cfg.Projects["js-inert"] = project.ProjectConfig{AutoRetrospect: true}
	if reloaded := journalingSummary(cfg); reloaded == summary {
		t.Errorf("summary unchanged at %q after a project opted in, so no config change is logged", reloaded)
	}
}

// The retrospect scan runs ahead of the git work, so a cycle that ends in a git
// error has still seeded or fired and still has warnings to report. `tk watch
// run` logs both before the error, or a project whose repo has moved mines its
// closes silently forever.
func TestWatchRunReportsRetrospectsOnAFailingCycle(t *testing.T) {
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")
	fakeLoom(t)

	// A registered path with no .git: the cycle fails on git after the scan.
	noRepo := filepath.Join(home, "wf-nogit")
	if err := os.MkdirAll(noRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"wf-nogit": {Path: noRepo, Store: "central", AutoLink: true, AutoClose: true, AutoRetrospect: true},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	central := ticket.NewProjectFileStore(filepath.Join(centralRoot, "tickets", "wf-nogit"), "wf-nogit")
	mkCentral(t, central, "wf-nogit-0001", ticket.TypeFeature, ticket.StatusDone, "")

	watchOnce = true
	watchInterval = time.Second
	t.Cleanup(func() { watchOnce = false })

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if err := runWatchRun(watchRunCmd, nil); err != nil {
		t.Fatalf("runWatchRun: %v", err)
	}
	out := logs.String()
	if !contains(out, "no git repository") {
		t.Fatalf("the cycle did not fail on git, so the report is untested:\n%s", out)
	}
	if !contains(out, "seeded 1 closed ticket") {
		t.Errorf("the seed warning was dropped with the error:\n%s", out)
	}

	logs.Reset()
	mkCentral(t, central, "wf-nogit-0002", ticket.TypeFeature, ticket.StatusDone, "")
	if err := runWatchRun(watchRunCmd, nil); err != nil {
		t.Fatalf("runWatchRun: %v", err)
	}
	if out := logs.String(); !contains(out, "retrospect: fired 1") {
		t.Errorf("the fired count was dropped with the error:\n%s", out)
	}
}

// fakeLoom puts an executable `loom` on PATH ahead of anything else, so a cycle
// that fires a retrospect never reaches the machine's own loom.
func fakeLoom(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "loom"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func commitClosing(t *testing.T, repo, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, id+".txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "Closes: ["+id+"] Done")
}
