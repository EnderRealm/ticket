package ticket

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// centralPair registers two repos as central-store projects and returns their
// repo paths and the central tickets root.
func centralPair(t *testing.T, src, dst string) (srcRepo, dstRepo, ticketsRoot string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")

	srcRepo = filepath.Join(home, src)
	dstRepo = filepath.Join(home, dst)
	for _, dir := range []string{srcRepo, dstRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			src: {Path: srcRepo, Store: "central"},
			dst: {Path: dstRepo, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	ticketsRoot = filepath.Join(centralRoot, "tickets")
	if err := os.MkdirAll(filepath.Join(ticketsRoot, src), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return srcRepo, dstRepo, ticketsRoot
}

func TestMoveIntoCentralProjectLandsInTheStore(t *testing.T) {
	// Joining ".tickets" onto the target repo path put the ticket somewhere the
	// destination project's own tk never looks, while the source was closed as
	// if it had relocated.
	_, dstRepo, ticketsRoot := centralPair(t, "mv-from", "mv-to")
	src := NewProjectFileStore(filepath.Join(ticketsRoot, "mv-from"), "mv-from")
	mkMovable(t, src, "central-move-0001", TypeFeature, StatusReady, "")

	dst, _, err := ResolveStoreForRepo(dstRepo)
	if err != nil {
		t.Fatalf("ResolveStoreForRepo: %v", err)
	}
	results, err := MoveTicket(src, dst, "central-move-0001", false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	newID := results[0].NewID
	if !strings.HasPrefix(newID, "mv-to/") {
		t.Errorf("NewID = %q, want it namespaced under the destination project", newID)
	}
	_, bare := ParseNamespacedID(newID)
	landed := filepath.Join(ticketsRoot, "mv-to", bare+".md")
	if _, err := os.Stat(landed); err != nil {
		t.Errorf("ticket did not land in the destination's central project dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRepo, ".tickets")); !os.IsNotExist(err) {
		t.Errorf("a stray .tickets/ was created in %s: %v", dstRepo, err)
	}
	// The destination's own store has to read it back — the namespaced ID is
	// what `tk show` and MCP are handed.
	if _, err := dst.Get(newID); err != nil {
		t.Errorf("Get %s from the destination store: %v", newID, err)
	}
	orig, err := src.Get("central-move-0001")
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if orig.Status != StatusClosed {
		t.Errorf("source status = %q, want %q", orig.Status, StatusClosed)
	}
}

func TestMoveIntoCentralProjectNamespacesRemappedRefs(t *testing.T) {
	// A central project's tickets reference each other namespaced, so the
	// remapped parent, dep and link have to carry the destination's prefix —
	// under the source's, nothing in the destination resolves them.
	_, dstRepo, ticketsRoot := centralPair(t, "ns-from", "ns-to")
	src := NewProjectFileStore(filepath.Join(ticketsRoot, "ns-from"), "ns-from")

	epic := &Ticket{
		ID: "ns-epic-0001", Status: StatusBacklog, Type: TypeEpic, Priority: 2,
		Title: "Namespaced epic", Deps: []string{}, Links: []string{},
	}
	child := &Ticket{
		ID: "ns-child-0002", Status: StatusReady, Type: TypeFeature, Priority: 2,
		Parent: "ns-from/ns-epic-0001", Title: "Namespaced child",
		Deps: []string{"ns-from/ns-epic-0001"}, Links: []string{"ns-from/ns-epic-0001"},
	}
	if err := src.Create(epic); err != nil {
		t.Fatalf("Create epic: %v", err)
	}
	if err := src.Create(child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	dst, _, err := ResolveStoreForRepo(dstRepo)
	if err != nil {
		t.Fatalf("ResolveStoreForRepo: %v", err)
	}
	results, err := MoveTicket(src, dst, "ns-epic-0001", true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	newEpic, newChild := results[0].NewID, results[1].NewID
	if !strings.HasPrefix(newEpic, "ns-to/") || !strings.HasPrefix(newChild, "ns-to/") {
		t.Fatalf("new IDs = %q, %q, want both namespaced under ns-to", newEpic, newChild)
	}

	moved, err := dst.Get(newChild)
	if err != nil {
		t.Fatalf("Get moved child: %v", err)
	}
	if moved.Parent != newEpic {
		t.Errorf("Parent = %q, want %q", moved.Parent, newEpic)
	}
	if len(moved.Deps) != 1 || moved.Deps[0] != newEpic {
		t.Errorf("Deps = %v, want [%s]", moved.Deps, newEpic)
	}
	if len(moved.Links) != 1 || moved.Links[0] != newEpic {
		t.Errorf("Links = %v, want [%s]", moved.Links, newEpic)
	}
}

func TestResolveStoreForRepoRefusesUnresolvableRepo(t *testing.T) {
	// No store to resolve is an error naming the repo — the previous behavior
	// minted a .tickets/ beside it and orphaned whatever landed there.
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()

	store, _, err := ResolveStoreForRepo(repo)
	if err == nil {
		t.Fatalf("resolved %q for an unregistered repo, want an error", store.Dir)
	}
	if !strings.Contains(err.Error(), repo) {
		t.Errorf("error %q does not name the repo %s", err, repo)
	}
	if !strings.Contains(err.Error(), "tk init") {
		t.Errorf("error %q does not name the command that registers the project", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".tickets")); !os.IsNotExist(err) {
		t.Errorf("a .tickets/ was created in %s: %v", repo, err)
	}
}

func TestResolveStoreForRepoReportsAnUnregisteredProject(t *testing.T) {
	// The store an unregistered central project resolves to is one nothing else
	// writes to, so the caller has to be told which state it landed in.
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")
	repo := filepath.Join(home, "unreg-to")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := project.Save(project.Config{CentralRoot: centralRoot}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	strayDir := filepath.Join(centralRoot, "tickets", "unreg-to")
	if err := os.MkdirAll(strayDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, unregistered, err := ResolveStoreForRepo(repo)
	if err != nil {
		t.Fatalf("ResolveStoreForRepo: %v", err)
	}
	if store.Dir != strayDir {
		t.Errorf("Dir = %q, want %q", store.Dir, strayDir)
	}
	if !unregistered {
		t.Error("resolution should report the project as unregistered")
	}
	if warning := UnregisteredWarning(store); !strings.Contains(warning, "unreg-to") || !strings.Contains(warning, "tk init") {
		t.Errorf("warning = %q, want it to name the project and `tk init`", warning)
	}
}

func TestResolveStoreForRepoNamesAConfigItCannotLoad(t *testing.T) {
	// A config that fails to load resolves nothing, but it is not an
	// unregistered repo: reporting it as one sends the user to `tk init`, which
	// does not fix a malformed config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ticket"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ticket", "config.yaml"), []byte("central_root: [unterminated\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := ResolveStoreForRepo(t.TempDir())
	if err == nil {
		t.Fatal("resolved a store with an unreadable config, want an error")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Errorf("error %q does not name the config that failed to load", err)
	}
	if strings.Contains(err.Error(), "tk init") {
		t.Errorf("error %q sends the user to `tk init`, which does not fix a malformed config", err)
	}
}

func TestResolveStoreForRepoNamesALegacyTicketsDirFromASubdirectory(t *testing.T) {
	// The user most likely to think their tickets vanished is standing somewhere
	// inside the repo rather than at its root, which is where the plain "no
	// ticket store found" reads as data loss. The probe is the git top level —
	// the same root `tk init` migrates — so the two agree on which .tickets/ is
	// meant.
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	local := filepath.Join(repo, ".tickets")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sub := filepath.Join(repo, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, _, err := ResolveStoreForRepo(sub)
	if err == nil {
		t.Fatal("resolved a store for a repo holding only a .tickets/, want an error")
	}
	// The git top level comes back canonicalized, so the directory is matched by
	// suffix rather than by the spelling this test passed in.
	if !strings.Contains(err.Error(), filepath.Join(filepath.Base(repo), ".tickets")) {
		t.Errorf("error %q does not name the repo root's .tickets/", err)
	}
	if !strings.Contains(err.Error(), "tk init") {
		t.Errorf("error %q does not name the command that migrates it", err)
	}
}

func TestResolveStoreForRepoNamesALegacyTicketsDir(t *testing.T) {
	// A repo carrying its own .tickets/ no longer resolves to it — tk reads the
	// central store alone — so the refusal names the directory and the command
	// that migrates it, and leaves its contents exactly as they were.
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	local := filepath.Join(repo, ".tickets")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := "---\nid: legacy-0001\nstatus: open\n---\n\nUntouched.\n"
	if err := os.WriteFile(filepath.Join(local, "legacy-0001.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store, _, err := ResolveStoreForRepo(repo)
	if err == nil {
		t.Fatalf("resolved %q for a repo holding only a .tickets/, want an error", store.Dir)
	}
	for _, want := range []string{local, "tk init"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	entries, readErr := os.ReadDir(local)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 1 {
		t.Fatalf(".tickets/ holds %v, want only the original ticket", entries)
	}
	raw, readErr := os.ReadFile(filepath.Join(local, "legacy-0001.md"))
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(raw) != body {
		t.Errorf("the legacy ticket file was rewritten:\n%s", string(raw))
	}
}
