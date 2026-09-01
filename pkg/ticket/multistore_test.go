package ticket

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

func testMultiStore(t *testing.T, projects ...string) (*MultiStore, string) {
	t.Helper()
	dir := t.TempDir()
	for _, p := range projects {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return NewMultiStore(dir), dir
}

func TestMultiStoreProjects(t *testing.T) {
	ms, _ := testMultiStore(t, "alpha", "beta")
	projects, err := ms.projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestMultiStoreCreateAndGet(t *testing.T) {
	ms, _ := testMultiStore(t, "myproject")
	tk := sampleTicket("myproject/test-1234")
	if err := ms.Create(tk); err != nil {
		t.Fatal(err)
	}
	// ID should be namespaced after create
	if tk.ID != "myproject/test-1234" {
		t.Errorf("expected namespaced ID after create, got %q", tk.ID)
	}

	// Get with namespaced ID
	got, err := ms.Get("myproject/test-1234")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "myproject/test-1234" {
		t.Errorf("expected namespaced ID, got %q", got.ID)
	}
}

func TestMultiStoreGetBareID(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	tk := sampleTicket("proj/unique-abc1")
	if err := ms.Create(tk); err != nil {
		t.Fatal(err)
	}

	// Get with bare ID — should resolve
	got, err := ms.Get("unique-abc1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "proj/unique-abc1" {
		t.Errorf("expected proj/unique-abc1, got %q", got.ID)
	}
}

func TestMultiStoreGetAmbiguous(t *testing.T) {
	ms, _ := testMultiStore(t, "alpha", "beta")

	tk1 := sampleTicket("alpha/shared-id12")
	if err := ms.Create(tk1); err != nil {
		t.Fatal(err)
	}
	tk2 := sampleTicket("beta/shared-id12")
	if err := ms.Create(tk2); err != nil {
		t.Fatal(err)
	}

	// Bare ID should be ambiguous
	_, err := ms.Get("shared-id12")
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "ambiguous") {
		t.Errorf("expected ambiguous error, got: %s", got)
	}
}

func TestMultiStoreList(t *testing.T) {
	ms, _ := testMultiStore(t, "alpha", "beta")

	tk1 := sampleTicket("alpha/a-ticket-1111")
	if err := ms.Create(tk1); err != nil {
		t.Fatal(err)
	}
	tk2 := sampleTicket("beta/b-ticket-2222")
	if err := ms.Create(tk2); err != nil {
		t.Fatal(err)
	}

	tickets, err := ms.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(tickets))
	}

	ids := map[string]bool{}
	for _, tk := range tickets {
		ids[tk.ID] = true
	}
	if !ids["alpha/a-ticket-1111"] {
		t.Error("missing alpha/a-ticket-1111")
	}
	if !ids["beta/b-ticket-2222"] {
		t.Error("missing beta/b-ticket-2222")
	}
}

func TestMultiStoreListWithSkips(t *testing.T) {
	// A skip carries a base filename, so it names nothing without the project
	// whose directory held it — and the tickets beside it still list.
	ms, dir := testMultiStore(t, "alpha", "beta")
	if err := ms.Create(sampleTicket("alpha/a-ticket-1111")); err != nil {
		t.Fatal(err)
	}
	plantUnreadable(t, filepath.Join(dir, "beta"), "b-broken-2222.md")

	tickets, skips, err := ms.ListWithSkips()
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].ID != "alpha/a-ticket-1111" {
		t.Errorf("ListWithSkips returned %v, want the readable ticket namespaced", ids2(tickets))
	}
	if len(skips) != 1 || skips[0].File != "b-broken-2222.md" || skips[0].Project != "beta" {
		t.Fatalf("skips = %+v, want the beta file reported under its own project", skips)
	}
}

func TestMultiStoreGetBareIDNamesAnUnreadableFile(t *testing.T) {
	// The only candidate for the ID is a file that will not parse. Answered
	// "not found in any project", the caller would create the ticket again and
	// leave the file where it is.
	ms, dir := testMultiStore(t, "proj")
	plantUnreadable(t, filepath.Join(dir, "proj"), "broken-9999.md")

	_, err := ms.Get("broken-9999")
	var unreadable *UnreadableTicketError
	if !errors.As(err, &unreadable) {
		t.Fatalf("Get = %v, want an *UnreadableTicketError", err)
	}
	if !strings.Contains(err.Error(), "proj") || !strings.Contains(err.Error(), "broken-9999.md") {
		t.Errorf("error = %q, want it to name the project and the file", err)
	}
	if _, err := ms.Get("missing-0000"); errors.As(err, &unreadable) {
		t.Errorf("a ticket in no project reported as unreadable: %v", err)
	}
}

func TestMultiStoreUpdate(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	tk := sampleTicket("proj/up-ticket-3333")
	if err := ms.Create(tk); err != nil {
		t.Fatal(err)
	}

	// Update with namespaced ID
	tk.Title = "Updated title"
	if err := ms.Update(tk); err != nil {
		t.Fatal(err)
	}
	got, _ := ms.Get("proj/up-ticket-3333")
	if got.Title != "Updated title" {
		t.Errorf("expected updated title, got %q", got.Title)
	}
}

func TestMultiStoreUpdateBareID(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	tk := sampleTicket("proj/bare-up-4444")
	if err := ms.Create(tk); err != nil {
		t.Fatal(err)
	}

	// Update with bare ID
	tk.ID = "bare-up-4444"
	tk.Title = "Bare update"
	if err := ms.Update(tk); err != nil {
		t.Fatal(err)
	}
	got, _ := ms.Get("proj/bare-up-4444")
	if got.Title != "Bare update" {
		t.Errorf("expected 'Bare update', got %q", got.Title)
	}
}

func TestMultiStoreDelete(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	tk := sampleTicket("proj/del-ticket-5555")
	if err := ms.Create(tk); err != nil {
		t.Fatal(err)
	}

	if err := ms.Delete("proj/del-ticket-5555"); err != nil {
		t.Fatal(err)
	}
	_, err := ms.Get("proj/del-ticket-5555")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestMultiStoreDeleteBareID(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	tk := sampleTicket("proj/del-bare-6666")
	if err := ms.Create(tk); err != nil {
		t.Fatal(err)
	}

	if err := ms.Delete("del-bare-6666"); err != nil {
		t.Fatal(err)
	}
	_, err := ms.Get("proj/del-bare-6666")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestMultiStoreCreateRequiresProject(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	tk := sampleTicket("no-project-7777")
	err := ms.Create(tk)
	if err == nil {
		t.Fatal("expected error for bare ID create")
	}
	if got := err.Error(); !strings.Contains(got, "project is required") {
		t.Errorf("expected 'project is required' error, got: %s", got)
	}
}

func TestMultiStoreCreateRefusesUnknownProject(t *testing.T) {
	// Registration is read from config, so point HOME at an empty home to make
	// "unregistered" a property of the test rather than of the machine.
	t.Setenv("HOME", t.TempDir())
	ms, root := testMultiStore(t, "known")

	err := ms.Create(sampleTicket("unknown/stray-8888"))
	if err == nil {
		t.Fatal("expected error creating into a project with no directory")
	}
	for _, want := range []string{"unknown", "tk init"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "unknown")); !os.IsNotExist(err) {
		t.Errorf("project directory was created under the store root: %v", err)
	}
}

func TestMultiStoreCreateRefusesNonCentralProject(t *testing.T) {
	// project.Load merges local and shared config, and saveLocal writes only
	// `path` for a project once a shared config exists — so a project whose
	// shared entry was lost merges to an entry with no store. A directory under
	// the central root is only ours to create for a project registered central.
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"local-only": {Path: filepath.Join(home, "local-only")},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	root := filepath.Join(centralRoot, "tickets")
	ms := NewMultiStore(root)
	if err := ms.Create(sampleTicket("local-only/stray-6666")); err == nil {
		t.Fatal("expected error creating into a project not registered as central")
	}
	if _, err := os.Stat(filepath.Join(root, "local-only")); !os.IsNotExist(err) {
		t.Errorf("project directory was created under the store root: %v", err)
	}
}

func TestMultiStoreCreateRefusesSymlinkedProjectDir(t *testing.T) {
	// The central store is a git repo and git tracks symlinks, so one can arrive
	// from another committer. Following it would write outside the store, and
	// projects() does not follow it, so those tickets would never be listed.
	t.Setenv("HOME", t.TempDir())
	ms, root := testMultiStore(t)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := ms.Create(sampleTicket("linked/stray-7777")); err == nil {
		t.Fatal("expected error creating into a symlinked project directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("write landed outside the store: %v", entries)
	}
}

func TestMultiStoreCreateRegisteredProjectWithoutDir(t *testing.T) {
	// Git tracks no empty directories, so a registered project that has never
	// held a ticket has no directory on a freshly cloned central store. Its
	// registration is the authority the directory only stands in for.
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"fresh": {Path: filepath.Join(home, "fresh"), Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	root := filepath.Join(centralRoot, "tickets")
	ms := NewMultiStore(root)
	if err := ms.Create(sampleTicket("fresh/clone-1234")); err != nil {
		t.Fatalf("Create into a registered project: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "fresh")); err != nil {
		t.Errorf("project directory was not created: %v", err)
	}
	if _, err := ms.Get("fresh/clone-1234"); err != nil {
		t.Errorf("Get after create: %v", err)
	}
}

func TestMultiStoreGetEmptyID(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	if err := ms.Create(sampleTicket("proj/lone-abcd")); err != nil {
		t.Fatal(err)
	}

	got, err := ms.Get("")
	if err == nil {
		t.Fatalf("Get(\"\") should fail, got ticket %v", got)
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("Get(\"\") error = %q, want to contain %q", err.Error(), "id is required")
	}
}

func TestMultiStore_TraversalProject(t *testing.T) {
	// The project half of a namespaced ID is joined into the store root, so
	// "../evil" roots the per-project store one level above it and the bare
	// remainder "evil" then passes the FileStore's own ID check. Nest the root
	// inside an outer temp dir so the escape lands somewhere the test owns.
	outer := t.TempDir()
	root := filepath.Join(outer, "tickets")
	if err := os.MkdirAll(filepath.Join(root, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	ms := NewMultiStore(root)

	victim := filepath.Join(outer, "evil.md")
	const sentinel = "---\nid: evil\nstatus: ready\ndeps: []\nlinks: []\ncreated: 2026-01-01T00:00:00Z\ntype: feature\npriority: 2\n---\n# Secret\n\nBody.\n"
	if err := os.WriteFile(victim, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	got, err := ms.Get("../evil")
	assertInvalidError(t, `Get("../evil")`, "invalid project", "..", err)
	if got != nil {
		t.Errorf("Get read a ticket outside the store: %v", got)
	}

	assertInvalidError(t, `Update("../evil")`, "invalid project", "..", ms.Update(sampleTicket("../evil")))
	assertInvalidError(t, `Delete("../evil")`, "invalid project", "..", ms.Delete("../evil"))
	assertInvalidError(t, `Create("../planted")`, "invalid project", "..", ms.Create(sampleTicket("../planted")))

	if _, err := os.Stat(filepath.Join(outer, "planted.md")); !os.IsNotExist(err) {
		t.Errorf("file created outside store: %v", err)
	}
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(data) != sentinel {
		t.Errorf("file outside store was overwritten: %q", string(data))
	}
}

func TestMultiStoreEmptyRoot(t *testing.T) {
	ms, _ := testMultiStore(t)
	tickets, err := ms.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Errorf("expected 0 tickets, got %d", len(tickets))
	}
}
