package ticket

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// catalogRoot is a throwaway central root, with TK_STORE_ROOT pointing at it
// so the config a write path loads on its own (MultiStore.Create) is the one
// the test wrote there and never the machine's. The layout is the production
// one: tickets under <root>/tickets/<namespace>, the shared config and the
// catalog beside that directory.
func catalogRoot(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv(project.StoreRootEnv, root)
	return root
}

func writeCatalog(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(CatalogPath(root), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkNamespaceDir(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, "tickets", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// registered is a config registering each named project with the central
// store, at a repo path under root so the entry looks like `tk init` wrote it.
func registered(root string, names ...string) project.Config {
	cfg := project.Config{CentralRoot: root, Projects: map[string]project.ProjectConfig{}}
	for _, n := range names {
		cfg.Projects[n] = project.ProjectConfig{Path: filepath.Join(root, "repos", n), Store: "central"}
	}
	return cfg
}

func findNamespace(t *testing.T, inv Inventory, name string) Namespace {
	t.Helper()
	for _, ns := range inv.Namespaces {
		if ns.Name == name {
			return ns
		}
	}
	t.Fatalf("namespace %q not in inventory: %+v", name, inv.Namespaces)
	return Namespace{}
}

func diagnosticsMention(inv Inventory, text string) bool {
	for _, d := range inv.Diagnostics {
		if strings.Contains(d, text) {
			return true
		}
	}
	return false
}

// snapshotTree lists every path under root with its size, so a test can assert
// that a read-only call or a refused write left the tree exactly as it was.
func snapshotTree(t *testing.T, root string) map[string]int64 {
	t.Helper()
	tree := map[string]int64{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		tree[path] = info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func assertTreeUnchanged(t *testing.T, before, after map[string]int64) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("tree changed: %d entries before, %d after", len(before), len(after))
	}
	for path, size := range before {
		if got, ok := after[path]; !ok || got != size {
			t.Errorf("tree changed at %s: size %d before, %d after (present: %v)", path, size, got, ok)
		}
	}
}

func TestInventoryBuiltInRootDistinguishesRegisteredAndLegacy(t *testing.T) {
	root := catalogRoot(t)
	mkNamespaceDir(t, root, "warp")
	if err := os.WriteFile(filepath.Join(root, "tickets", "warp", "w-0001.md"), []byte("---\nid: w-0001\nstatus: open\n---\n# One\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkNamespaceDir(t, root, "code")
	cfg := registered(root, "warp")

	before := snapshotTree(t, root)
	inv, err := ReadInventory(root, cfg)
	if err != nil {
		t.Fatalf("ReadInventory: %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, root))

	rootNS := findNamespace(t, inv, project.RootNamespace)
	if rootNS.Kind != KindRoot || rootNS.Registered || rootNS.RepoPath != "" || rootNS.Directory || rootNS.State != StateMissing {
		t.Errorf("Root should be built in with no repository and no directory, got %+v", rootNS)
	}
	warp := findNamespace(t, inv, "warp")
	if warp.Kind != KindProject || !warp.Registered || warp.RepoPath != cfg.Projects["warp"].Path || warp.State != StatePresent {
		t.Errorf("registered project misreported: %+v", warp)
	}
	code := findNamespace(t, inv, "code")
	if code.Kind != KindLegacy || code.Registered || code.RepoPath != "" || code.State != StateEmpty {
		t.Errorf("legacy directory misreported: %+v", code)
	}

	// No catalog is the preactivation state: readable, but not complete, and
	// the diagnostic names the catalog as the source.
	if inv.Complete {
		t.Error("an uncatalogued store must not report complete")
	}
	if !diagnosticsMention(inv, CatalogPath(root)) {
		t.Errorf("diagnostics should name the missing catalog, got %q", inv.Diagnostics)
	}
	if inv.Catalog != nil {
		t.Error("Catalog should be nil before activation")
	}
}

func TestInventoryReportsRootRepoBinding(t *testing.T) {
	root := catalogRoot(t)
	cfg := registered(root, project.RootNamespace)

	inv, err := ReadInventory(root, cfg)
	if err != nil {
		t.Fatalf("ReadInventory: %v", err)
	}
	rootNS := findNamespace(t, inv, project.RootNamespace)
	if rootNS.RepoPath != "" {
		t.Errorf("Root must never carry a repo path, got %q", rootNS.RepoPath)
	}
	if inv.Complete || !diagnosticsMention(inv, project.ErrRootBinding.Error()) {
		t.Errorf("a config entry binding _root to a repo must be diagnosed, got complete=%v %q", inv.Complete, inv.Diagnostics)
	}
}

func TestInventoryEmptyClonedNamespaceIsNotMissing(t *testing.T) {
	root := catalogRoot(t)
	writeCatalog(t, root, "required_features: [root-namespace]\nnamespaces:\n  _root: {kind: root}\n  cloned: {kind: project}\n  absent: {kind: project}\n")
	// A clone carries a namespace that holds no tickets only through its
	// marker; the directory alone would not have survived the push.
	for _, name := range []string{project.RootNamespace, "cloned"} {
		if err := WriteNamespaceMarker(root, name); err != nil {
			t.Fatalf("WriteNamespaceMarker %s: %v", name, err)
		}
	}
	cfg := registered(root, "cloned", "absent")

	inv, err := ReadInventory(root, cfg)
	if err != nil {
		t.Fatalf("ReadInventory: %v", err)
	}
	cloned := findNamespace(t, inv, "cloned")
	if !cloned.Directory || !cloned.Marker || cloned.State != StateEmpty {
		t.Errorf("cloned empty namespace should read empty with its marker, got %+v", cloned)
	}
	absent := findNamespace(t, inv, "absent")
	if absent.Directory || absent.Marker || absent.State != StateMissing {
		t.Errorf("catalogued namespace with nothing on disk should read missing, got %+v", absent)
	}
	rootNS := findNamespace(t, inv, project.RootNamespace)
	if rootNS.State != StateEmpty || !rootNS.Marker {
		t.Errorf("activated Root with its marker should read empty, got %+v", rootNS)
	}
	if inv.Complete {
		t.Error("a missing catalogued namespace must not report complete")
	}
	if !diagnosticsMention(inv, `"absent"`) {
		t.Errorf("diagnostics should name the missing namespace, got %q", inv.Diagnostics)
	}
	if diagnosticsMention(inv, `"cloned"`) {
		t.Errorf("an empty cloned namespace is healthy and must not be diagnosed, got %q", inv.Diagnostics)
	}
}

func TestInventoryCataloguedWithoutMarkerIsIncomplete(t *testing.T) {
	root := catalogRoot(t)
	writeCatalog(t, root, "namespaces:\n  bare: {kind: project}\n")
	mkNamespaceDir(t, root, "bare")

	inv, err := ReadInventory(root, registered(root, "bare"))
	if err != nil {
		t.Fatalf("ReadInventory: %v", err)
	}
	if inv.Complete || !diagnosticsMention(inv, NamespaceMarker) {
		t.Errorf("a catalogued directory without its marker must be diagnosed, got complete=%v %q", inv.Complete, inv.Diagnostics)
	}
}

func TestInventoryUncataloguedDirectoryIsLegacyAndNamed(t *testing.T) {
	root := catalogRoot(t)
	writeCatalog(t, root, "namespaces:\n  warp: {kind: project}\n")
	if err := WriteNamespaceMarker(root, "warp"); err != nil {
		t.Fatal(err)
	}
	mkNamespaceDir(t, root, "code")

	inv, err := ReadInventory(root, registered(root, "warp"))
	if err != nil {
		t.Fatalf("ReadInventory: %v", err)
	}
	code := findNamespace(t, inv, "code")
	if code.Kind != KindLegacy {
		t.Errorf("uncatalogued directory should be discovered as legacy, got %+v", code)
	}
	// Discovered, not incomplete: its tickets are readable. Named so that
	// activation can admit it.
	if !inv.Complete {
		t.Errorf("a discovered legacy directory alone must not make the inventory incomplete: %q", inv.Diagnostics)
	}
	if !diagnosticsMention(inv, `"code"`) {
		t.Errorf("diagnostics should name the uncatalogued namespace, got %q", inv.Diagnostics)
	}
}

func TestInventoryRefusesMalformedCatalog(t *testing.T) {
	root := catalogRoot(t)
	writeCatalog(t, root, ":::bad yaml{{{")

	if _, err := ReadInventory(root, registered(root)); err == nil {
		t.Fatal("a malformed catalog must be an error, not an empty inventory")
	}
	if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), CatalogPath(root)) {
		t.Errorf("LoadCatalog should name the file, got %v", err)
	}
}

func TestInventoryRefusesUnreadableCatalog(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	root := catalogRoot(t)
	writeCatalog(t, root, "namespaces: {}\n")
	if err := os.Chmod(CatalogPath(root), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(CatalogPath(root), 0o644) })

	if _, err := ReadInventory(root, registered(root)); err == nil {
		t.Fatal("an unreadable catalog must be an error, not an empty inventory")
	}
}

func TestLoadCatalogRejectsRootKindMismatch(t *testing.T) {
	root := catalogRoot(t)
	for _, body := range []string{
		"namespaces:\n  _root: {kind: project}\n",
		"namespaces:\n  other: {kind: root}\n",
		"namespaces:\n  other: {kind: whatever}\n",
		"namespaces:\n  ../escape: {kind: project}\n",
	} {
		writeCatalog(t, root, body)
		if _, err := LoadCatalog(root); err == nil {
			t.Errorf("catalog %q should be refused", body)
		}
	}
}

func TestUnreadableConfigIsAnErrorNotEmpty(t *testing.T) {
	root := catalogRoot(t)
	// The shared config under the override root is the one project.Load reads.
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("<<<<<<< HEAD\nprojects:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Load(); err == nil {
		t.Fatal("a corrupt shared config must fail to load rather than read as no projects")
	}
}

func TestNamespaceMarkerIsNotATicket(t *testing.T) {
	root := catalogRoot(t)
	if err := WriteNamespaceMarker(root, "empty"); err != nil {
		t.Fatalf("WriteNamespaceMarker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tickets", "empty", NamespaceMarker)); err != nil {
		t.Fatalf("marker not written: %v", err)
	}

	store := NewProjectFileStore(filepath.Join(root, "tickets", "empty"), "empty")
	tickets, skips, err := store.ListWithSkips()
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 || len(skips) != 0 {
		t.Errorf("the marker must be neither a ticket nor a skipped file, got %d tickets, %d skips", len(tickets), len(skips))
	}
	all, err := NewMultiStore(filepath.Join(root, "tickets")).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("MultiStore.List should see no tickets, got %d", len(all))
	}
}

// TestWriteNamespaceMarkerRefusesSymlinkedNamespaceDir: the central store is
// a git repo and git tracks symlinks, so a namespace directory can arrive as a
// link to somewhere outside the store. MkdirAll accepts one; the write must
// not.
func TestWriteNamespaceMarkerRefusesSymlinkedNamespaceDir(t *testing.T) {
	root := catalogRoot(t)
	outside := t.TempDir()
	mkNamespaceDir(t, root, "proj")
	if err := os.Symlink(outside, filepath.Join(root, "tickets", "linked")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, outside)

	err := WriteNamespaceMarker(root, "linked")
	if err == nil || !strings.Contains(err.Error(), "outside the store") {
		t.Fatalf("a symlinked namespace directory must be refused, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, outside))
	if _, err := os.Lstat(filepath.Join(outside, NamespaceMarker)); !os.IsNotExist(err) {
		t.Errorf("marker written through the link (lstat err: %v)", err)
	}
}

// TestWriteNamespaceMarkerRefusesSymlinkedMarker: a .namespace that is itself
// a symlink to a file outside the store must be refused, not written through.
// os.WriteFile follows the link and truncates its target.
func TestWriteNamespaceMarkerRefusesSymlinkedMarker(t *testing.T) {
	root := catalogRoot(t)
	victim := filepath.Join(t.TempDir(), "victim.txt")
	const sentinel = "do not touch\n"
	if err := os.WriteFile(victim, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := mkNamespaceDir(t, root, "proj")
	if err := os.Symlink(victim, filepath.Join(dir, NamespaceMarker)); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)

	err := WriteNamespaceMarker(root, "proj")
	if err == nil || !strings.Contains(err.Error(), "outside the store") {
		t.Fatalf("a symlinked marker must be refused, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, root))
	got, readErr := os.ReadFile(victim)
	if readErr != nil || string(got) != sentinel {
		t.Errorf("target changed through the link: %q (err %v)", got, readErr)
	}
	if info, err := os.Lstat(filepath.Join(dir, NamespaceMarker)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link itself must be left as found (err %v)", err)
	}
}

func TestGuardRefusesUnsupportedFeatureBeforeWriting(t *testing.T) {
	root := catalogRoot(t)
	store := NewProjectFileStore(mkNamespaceDir(t, root, "proj"), "proj")
	if err := store.Create(sampleTicket("keep-0001")); err != nil {
		t.Fatal(err)
	}
	// Written after the first ticket, the way an activation on a newer tk
	// lands in a store this binary was already writing to.
	writeCatalog(t, root, "required_features: [root-namespace, time-travel]\nnamespaces:\n  proj: {kind: project}\n")
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	assertUnsupported := func(op string, err error) {
		t.Helper()
		var unsupported *UnsupportedFeatureError
		if !errors.As(err, &unsupported) {
			t.Fatalf("%s: want UnsupportedFeatureError, got %v", op, err)
		}
		if len(unsupported.Features) != 1 || unsupported.Features[0] != "time-travel" {
			t.Errorf("%s: should name the unsupported feature alone, got %q", op, unsupported.Features)
		}
		if !strings.Contains(err.Error(), "replace this binary") {
			t.Errorf("%s: should say the binary must be replaced, got %v", op, err)
		}
	}

	ms := NewMultiStore(filepath.Join(root, "tickets"))
	assertUnsupported("MultiStore.Create", ms.Create(sampleTicket("proj/new-0002")))

	existing, err := store.Get("keep-0001")
	if err != nil {
		t.Fatal(err)
	}
	existing.Title = "changed"
	assertUnsupported("FileStore.Update", store.Update(existing))
	assertUnsupported("FileStore.Delete", store.Delete("keep-0001"))
	_, err = Mutate(store, "keep-0001", func(t *Ticket) error { t.Title = "changed"; return nil })
	assertUnsupported("Mutate", err)

	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	// Reads are not gated.
	if _, err := ms.Get("proj/keep-0001"); err != nil {
		t.Errorf("Get under an unsupported catalog: %v", err)
	}
}

func TestGuardRefusesRootBeforeActivation(t *testing.T) {
	root := catalogRoot(t)
	mkNamespaceDir(t, root, "proj")
	ms := NewMultiStore(filepath.Join(root, "tickets"))

	// No catalog at all: the store this ticket leaves behind.
	err := ms.Create(sampleTicket(project.RootNamespace + "/idea-0001"))
	if !errors.Is(err, ErrRootNotActivated) {
		t.Fatalf("write to _root without a catalog: want ErrRootNotActivated, got %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "tickets", project.RootNamespace)); !os.IsNotExist(statErr) {
		t.Errorf("a refused Root write must not create its directory (stat err: %v)", statErr)
	}

	// A catalog that lists Root but does not yet require the feature.
	writeCatalog(t, root, "namespaces:\n  _root: {kind: root}\n  proj: {kind: project}\n")
	err = ms.Create(sampleTicket(project.RootNamespace + "/idea-0001"))
	if !errors.Is(err, ErrRootNotActivated) {
		t.Fatalf("write to _root before activation: want ErrRootNotActivated, got %v", err)
	}
	// Ordinary projects are untouched by the gate.
	if err := ms.Create(sampleTicket("proj/plain-0001")); err != nil {
		t.Fatalf("write to an ordinary project under the catalog: %v", err)
	}

	// Activated: the feature is required, so Root accepts the write, and the
	// ticket keeps the project/id shape every other ticket has.
	writeCatalog(t, root, "required_features: [root-namespace]\nnamespaces:\n  _root: {kind: root}\n  proj: {kind: project}\n")
	tk := sampleTicket(project.RootNamespace + "/idea-0001")
	if err := ms.Create(tk); err != nil {
		t.Fatalf("write to activated Root: %v", err)
	}
	if tk.ID != project.RootNamespace+"/idea-0001" {
		t.Errorf("Root ticket ID = %q", tk.ID)
	}
	if _, err := ms.Get(project.RootNamespace + "/idea-0001"); err != nil {
		t.Errorf("Get Root ticket: %v", err)
	}
}

func TestLegacyStoreLoadsWithExistingIDs(t *testing.T) {
	root := catalogRoot(t)
	dir := mkNamespaceDir(t, root, "legacy")
	if err := os.WriteFile(filepath.Join(dir, "old-0001.md"), []byte("---\nid: old-0001\nstatus: open\ntype: feature\n---\n# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ms := NewMultiStore(filepath.Join(root, "tickets"))
	all, err := ms.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != "legacy/old-0001" {
		t.Fatalf("legacy store should list its ticket under its existing ID, got %+v", all)
	}
	got, err := ms.Get("old-0001")
	if err != nil {
		t.Fatal(err)
	}
	got.Title = "still writable"
	if err := ms.Update(got); err != nil {
		t.Fatalf("Update on a store with no catalog: %v", err)
	}
	if got.ID != "legacy/old-0001" {
		t.Errorf("ID rewritten by the update: %q", got.ID)
	}
	if _, err := os.Stat(CatalogPath(root)); !os.IsNotExist(err) {
		t.Errorf("a write must not conjure a catalog (stat err: %v)", err)
	}
	// Foreign parents stay refused before activation, as they were.
	child := sampleTicket("child-0002")
	child.Parent = "other/epic-0001"
	if err := NewProjectFileStore(dir, "legacy").Create(child); err == nil || !strings.Contains(err.Error(), "another project") {
		t.Errorf("foreign parent should still be refused, got %v", err)
	}
}

func TestPreflightActivation(t *testing.T) {
	root := catalogRoot(t)
	mkNamespaceDir(t, root, "proj")

	if err := PreflightActivation(root, registered(root, "proj")); err != nil {
		t.Errorf("clean store should pass preflight: %v", err)
	}

	bound := registered(root, "proj", project.RootNamespace)
	err := PreflightActivation(root, bound)
	if err == nil || !strings.Contains(err.Error(), project.ErrRootBinding.Error()) {
		t.Errorf("a config entry binding _root should refuse preflight, got %v", err)
	}

	// A _root directory nobody catalogued: refused, and left exactly as found.
	dir := mkNamespaceDir(t, root, project.RootNamespace)
	if err := os.WriteFile(filepath.Join(dir, "theirs-0001.md"), []byte("---\nid: theirs-0001\nstatus: open\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	err = PreflightActivation(root, registered(root, "proj"))
	if err == nil || !strings.Contains(err.Error(), "not catalogued as Root") {
		t.Errorf("an uncatalogued _root directory should refuse preflight, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, root))

	// Catalogued as Root already, the directory is the reserved one.
	writeCatalog(t, root, "namespaces:\n  _root: {kind: root}\n")
	if err := PreflightActivation(root, registered(root, "proj")); err != nil {
		t.Errorf("a _root catalogued as root is no collision: %v", err)
	}

	writeCatalog(t, root, "required_features: [time-travel]\nnamespaces:\n  _root: {kind: root}\n")
	var unsupported *UnsupportedFeatureError
	if err := PreflightActivation(root, registered(root, "proj")); !errors.As(err, &unsupported) {
		t.Errorf("a catalog this binary cannot honour should refuse preflight, got %v", err)
	}
}

func TestSupportedFeaturesContract(t *testing.T) {
	cat := &Catalog{RequiredFeatures: append([]string{"time-travel"}, SupportedFeatures...)}
	got := cat.UnsupportedFeatures()
	if len(got) != 1 || got[0] != "time-travel" {
		t.Errorf("UnsupportedFeatures = %q", got)
	}
	if !cat.Requires(FeatureRootNamespace) || !cat.RootActivated() {
		t.Error("a catalog requiring root-namespace should read as activated")
	}
	var none *Catalog
	if none.RootActivated() || none.UnsupportedFeatures() != nil || none.CheckFeatures("") != nil {
		t.Error("a nil catalog requires nothing")
	}
}
