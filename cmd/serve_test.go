package cmd

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ticketmcp "github.com/EnderRealm/ticket/v7/internal/mcp"
	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/journal"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// TestServeIsolatedStoreLeavesConfiguredStoreUntouched drives a create through
// the MCP path the way `tk serve` does, with TK_STORE_ROOT pointing elsewhere,
// and holds the configured store to being byte-identical afterwards. The store
// standing in for the real central store is a temp dir: no test may reach the
// machine's own.
func TestServeIsolatedStoreLeavesConfiguredStoreUntouched(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The stand-in "configured" central store, with a registered project and a
	// ticket in it, reachable only through ~/.ticket/config.yaml.
	configured := t.TempDir()
	configuredProject := filepath.Join(configured, "tickets", "standin")
	if err := os.MkdirAll(configuredProject, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := project.Config{
		CentralRoot: configured,
		Projects: map[string]project.ProjectConfig{
			"standin": {Path: t.TempDir(), Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("save configured config: %v", err)
	}
	seed := ticket.NewProjectFileStore(configuredProject, "standin")
	if err := seed.Create(&ticket.Ticket{
		ID:      "standin-0001",
		Title:   "Pre-existing ticket",
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed configured store: %v", err)
	}

	before := snapshotTree(t, configured)
	homeBefore := snapshotTree(t, home)

	// The isolated store: a separate root, registered in its own config, which
	// under the override lands inside that root rather than in HOME.
	isolated := t.TempDir()
	t.Setenv(project.StoreRootEnv, isolated)
	isolatedProject := filepath.Join(isolated, "tickets", "sandbox")
	if err := os.MkdirAll(isolatedProject, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(project.Config{
		Projects: map[string]project.ProjectConfig{
			"sandbox": {Path: t.TempDir(), Store: "central"},
		},
	}); err != nil {
		t.Fatalf("save isolated config: %v", err)
	}

	store, root, err := serveStore()
	if err != nil {
		t.Fatalf("serveStore: %v", err)
	}
	if root != isolated {
		t.Fatalf("serve resolved root %q, want the override %q", root, isolated)
	}

	// Resolved the way serve resolves it, from the config the override put in
	// the sandbox.
	sandboxCfg, _ := project.Load()
	cwd, _ := os.Getwd()
	defaultProject, _ := project.ResolveName(sandboxCfg, cwd, "")

	session := serveSession(t, store, defaultProject, root)

	info, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "ticket_store_info"})
	if err != nil {
		t.Fatal(err)
	}
	var storeInfo map[string]any
	if err := json.Unmarshal([]byte(info.Content[0].(*gomcp.TextContent).Text), &storeInfo); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if storeInfo["central_root"] != isolated {
		t.Errorf("ticket_store_info reported central_root %v, want the override %q", storeInfo["central_root"], isolated)
	}

	result, err := session.CallTool(context.Background(), &gomcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":   "Created in the sandbox",
			"type":    "feature",
			"project": "sandbox",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_create returned error: %v", result.Content)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*gomcp.TextContent).Text), &created); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	_, bareID := ticket.ParseNamespacedID(created["id"].(string))
	if _, err := os.Stat(filepath.Join(isolatedProject, bareID+".md")); err != nil {
		t.Errorf("ticket %v did not land in the isolated store at %s: %v", created["id"], isolatedProject, err)
	}

	assertTreeUnchanged(t, "configured store", configured, before)
	assertTreeUnchanged(t, "home", home, homeBefore)
}

// TestServeIsolatedStoreDoesNotSync holds the sandbox to being un-synced: the
// override root is a git repo with a 100ms sync interval configured, so a
// running syncLoop would commit it well inside the wait.
func TestServeIsolatedStoreDoesNotSync(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	isolated := t.TempDir()
	runGit(t, isolated, "init")
	runGit(t, isolated, "config", "user.email", "test@test.com")
	runGit(t, isolated, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(isolated, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, isolated, "add", "-A")
	runGit(t, isolated, "commit", "-m", "init")

	t.Setenv(project.StoreRootEnv, isolated)
	if err := project.Save(project.Config{SyncInterval: "100ms"}); err != nil {
		t.Fatalf("save isolated config: %v", err)
	}
	if got := syncInterval(); got != 100*time.Millisecond {
		t.Fatalf("syncInterval = %v, want 100ms — the test would not detect a sync", got)
	}

	if err := os.MkdirAll(filepath.Join(isolated, "tickets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(isolated, "tickets", "sandbox.md"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := startBackgroundLoops(ctx); err != nil {
		t.Fatalf("startBackgroundLoops: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	out, _ := execCommand("git", "-C", isolated, "log", "--oneline")
	if contains(out, "tk: sync tickets") {
		t.Errorf("sync ran against the isolated store, log: %s", out)
	}
}

// The serve-hosted journal loop is the one most people run, and a fully skipped
// project set looks exactly like a healthy one on stderr — the summary at
// startup is the only thing that tells them apart. The context is cancelled
// before the call so the loop logs and returns without a tick.
func TestServeWatchLoopLogsJournalingSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := project.Config{
		CentralRoot: filepath.Join(home, "central"),
		Projects: map[string]project.ProjectConfig{
			"inert-proj": {Path: filepath.Join(home, "inert-proj"), Store: "central"},
		},
		JournalDefaultsMigrated: true,
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	var logged strings.Builder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	watchLoop(ctx, time.Second)

	if want := "watch: projects=1 journaling=0 skipped=[inert-proj]"; !contains(logged.String(), want) {
		t.Errorf("log missing %q:\n%s", want, logged.String())
	}
}

func TestServeRelativeStoreRootRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(project.StoreRootEnv, "relative/store")

	// Cancellable, and cancelled when the test returns: if the guard below ever
	// regresses, this call starts watchLoop, and a loop on an uncancellable
	// context would outlive the test's HOME and run journal cycles — auto-closing
	// tickets — against the machine's real store.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, _, err := serveStore()
	if err == nil {
		t.Fatal("serveStore should refuse a relative TK_STORE_ROOT")
	}
	// Unwrapped: "serve requires a configured central store" would send the
	// reader to `tk init` when the fix is the variable.
	_, _, want := project.StoreRootOverride()
	if err.Error() != want.Error() {
		t.Errorf("serveStore error = %q, want the override's own error %q", err, want)
	}
	if err := startBackgroundLoops(ctx); err == nil {
		t.Error("startBackgroundLoops should refuse a relative TK_STORE_ROOT")
	}
}

// TestInvalidStoreRootRefusedBeforeResolution holds every gated command to the
// same fail-closed rule serve gets. Without it, IsConfigured reports the broken
// override as configured, project.Load's error is swallowed downstream, and a
// `tk create` with a typo'd variable writes into whatever .tickets/ is nearest
// the cwd instead of naming the variable.
func TestInvalidStoreRootRefusedBeforeResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Configured, so the gate below can only be tripped by the override.
	configured := t.TempDir()
	if err := project.Save(project.Config{CentralRoot: configured}); err != nil {
		t.Fatalf("save configured config: %v", err)
	}

	t.Setenv(project.StoreRootEnv, "relative/store")

	// A throwaway command, not the package-global createCmd: the gate sets
	// SilenceUsage on whatever it is handed, and that mutation would outlive
	// this test and be visible to every other one in the package. Only
	// cmd.Name() is read.
	err := rootCmd.PersistentPreRunE(&cobra.Command{Use: "create"}, nil)
	if err == nil {
		t.Fatal("a relative TK_STORE_ROOT should refuse the command before any store is resolved")
	}
	if !strings.Contains(err.Error(), project.StoreRootEnv) {
		t.Errorf("error %q should name %s", err, project.StoreRootEnv)
	}
}

// TestSyncWatchAndRecomputeRefuseIsolatedStore holds the documented "no sync,
// commit, push or journal watch runs" to the whole CLI: `tk serve` not starting
// the loops is not enough while a second command can commit and push the sandbox
// out of the git tree that encloses it, or rebuild the machine owner's commit
// journal — which lives under HOME, where the override does not reach — from
// the sandbox's contents.
func TestSyncWatchAndRecomputeRefuseIsolatedStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	isolated := t.TempDir()
	runGit(t, isolated, "init")
	runGit(t, isolated, "config", "user.email", "test@test.com")
	runGit(t, isolated, "config", "user.name", "test")
	if err := os.MkdirAll(filepath.Join(isolated, "tickets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(isolated, "tickets", "sandbox.md"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(project.StoreRootEnv, isolated)

	if err := runSync(syncCmd, nil); err == nil {
		t.Error("tk sync should refuse while TK_STORE_ROOT is set")
	} else if !strings.Contains(err.Error(), project.StoreRootEnv) {
		t.Errorf("sync refusal %q should name %s", err, project.StoreRootEnv)
	}

	out, _ := execCommand("git", "-C", isolated, "log", "--oneline")
	if contains(out, "tk: sync tickets") {
		t.Errorf("sync committed the isolated store, log: %s", out)
	}

	// Everything `tk recompute` needs to reach the destructive part: a project
	// registered in the sandbox's own config, pointed at a git repo, named the
	// same as one the machine has a journal for. Only the refusal stands between
	// the two, since JournalPath resolves under HOME either way. The project name
	// reaches runRecompute through the flag variable it reads, restored to its
	// zero value afterwards so no other test in the package sees it.
	if err := project.Save(project.Config{
		Projects: map[string]project.ProjectConfig{
			"sandbox": {Path: isolated, Store: "central"},
		},
	}); err != nil {
		t.Fatalf("save isolated config: %v", err)
	}
	recomputeProject = "sandbox"
	t.Cleanup(func() { recomputeProject = "" })

	jPath, err := journal.JournalPath("sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(jPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const ownersJournal = `{"sha":"deadbeef","ticket":"sandbox-0001"}` + "\n"
	if err := os.WriteFile(jPath, []byte(ownersJournal), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		run  func(*cobra.Command, []string) error
		cmd  *cobra.Command
	}{
		{"watch start", runWatchStart, watchStartCmd},
		{"watch run", runWatchRun, watchRunCmd},
		{"watch stop", runWatchStop, watchStopCmd},
		{"watch status", runWatchStatus, watchStatusCmd},
		{"watch logs", runWatchLogs, watchLogsCmd},
		{"recompute", runRecompute, recomputeCmd},
	} {
		err := tc.run(tc.cmd, nil)
		if err == nil {
			t.Errorf("tk %s should refuse while %s is set", tc.name, project.StoreRootEnv)
			continue
		}
		if !strings.Contains(err.Error(), project.StoreRootEnv) {
			t.Errorf("tk %s refusal %q should name %s", tc.name, err, project.StoreRootEnv)
		}
	}

	got, err := os.ReadFile(jPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if string(got) != ownersJournal {
		t.Errorf("the machine owner's journal at %s was rewritten: %q", jPath, got)
	}
}

// serveSession connects an in-process MCP client to a server over the given
// store, default project and central root — the wiring `tk serve` does over
// stdio, with the values it resolves rather than empty ones, so a tool that
// reports the root back (ticket_store_info) is exercised as served.
func serveSession(t *testing.T, store ticket.Store, defaultProject, centralRoot string) *gomcp.ClientSession {
	t.Helper()
	server := ticketmcp.NewServer(store, defaultProject, centralRoot)

	st, ct := gomcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go server.Run(ctx, st)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// snapshotTree records every path under root and the contents of every file,
// so a later comparison catches a write anywhere in the tree.
//
// The user cache directory, and the directories leading to it, are excluded:
// tk keeps its per-ticket lock files there (see FileStore.lockFile), and a lock
// is machine-local coordination between tk processes keyed on the store
// directory — deliberately not moved by TK_STORE_ROOT, and holding no content
// of its own. An isolated run takes locks like any other run, so what the
// callers assert stays what it is about: no store, config or journal touched.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	cacheRel := ""
	skip := map[string]bool{}
	if cache, err := os.UserCacheDir(); err == nil {
		if rel, err := filepath.Rel(root, cache); err == nil && !strings.HasPrefix(rel, "..") {
			cacheRel = rel
			for p := rel; p != "."; p = filepath.Dir(p) {
				skip[p] = true
			}
		}
	}
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[rel] {
				if rel == cacheRel {
					return filepath.SkipDir
				}
				return nil
			}
			tree[rel+string(filepath.Separator)] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return tree
}

func assertTreeUnchanged(t *testing.T, label, root string, before map[string]string) {
	t.Helper()
	after := snapshotTree(t, root)
	for path, content := range after {
		prev, ok := before[path]
		if !ok {
			t.Errorf("%s: %s appeared under %s", label, path, root)
			continue
		}
		if prev != content {
			t.Errorf("%s: %s was rewritten under %s", label, path, root)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("%s: %s disappeared from %s", label, path, root)
		}
	}
}
