package ticket

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain points the user cache directory at a temp tree for the whole
// package. Writing a ticket takes its lock there (FileStore.lockFile), and a
// lock file's name carries a hash of the store directory — a fresh t.TempDir on
// every run — so nothing is ever reused and each run would otherwise leave a
// permanent empty file in the developer's real cache directory. Set here rather
// than in the store helpers because tests build stores inline as often as
// through one.
//
// Both variables, because os.UserCacheDir reads HOME on darwin and prefers
// XDG_CACHE_HOME elsewhere. A test that sandboxes HOME for its own reasons
// overrides this and lands somewhere equally temporary.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tk-test-cache")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func testStore(t *testing.T) (*FileStore, func()) {
	t.Helper()
	dir := t.TempDir()
	store := NewFileStore(dir)
	return store, func() {}
}

// writeLegacy writes a ticket straight to disk, bypassing the write-path
// validation. Stores written before the one-level parent rule hold shapes
// Create and Update now reject — a non-epic parent, a nested epic, a parent in
// another project, a cycle — and reads must still cope with them.
func writeLegacy(t *testing.T, store *FileStore, tk *Ticket) {
	t.Helper()
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	if err := store.writeTicket(tk); err != nil {
		t.Fatalf("write %s: %v", tk.ID, err)
	}
}

func sampleTicket(id string) *Ticket {
	return &Ticket{
		ID:       id,
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Test ticket " + id,
		Body:     "\nDescription.\n",
	}
}

func TestFileStore_CreateAndGet(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-abc1")

	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get("t-abc1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "t-abc1" {
		t.Errorf("ID = %q, want %q", got.ID, "t-abc1")
	}
	if got.Title != "Test ticket t-abc1" {
		t.Errorf("Title = %q, want %q", got.Title, "Test ticket t-abc1")
	}
}

func TestFileStore_CreateDuplicate(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-dup1")

	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(tk); err == nil {
		t.Error("duplicate Create should fail")
	}
}

func TestFileStore_CreateInvalid(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-bad1")
	tk.Status = "invalid"

	if err := store.Create(tk); err == nil {
		t.Error("Create with invalid status should fail")
	}
}

func TestFileStore_Update(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-upd1")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tk.Status = StatusOpen
	tk.Priority = 0
	if err := store.Update(tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get("t-upd1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusOpen {
		t.Errorf("Status = %q, want %q", got.Status, StatusOpen)
	}
	if got.Priority != 0 {
		t.Errorf("Priority = %d, want 0", got.Priority)
	}
}

func TestFileStore_Update_MigratesLegacy(t *testing.T) {
	store, _ := testStore(t)
	// Write a raw legacy ticket file with stage only, no status.
	raw := "---\nid: t-mig1\nstage: implement\ndeps: []\nlinks: []\ncreated: 2026-01-01T00:00:00Z\ntype: task\npriority: 2\n---\n# Legacy ticket\n\nDescription.\n"
	if err := os.WriteFile(filepath.Join(store.Dir, "t-mig1.md"), []byte(raw), 0644); err != nil {
		t.Fatalf("write raw ticket: %v", err)
	}

	// Get triggers auto-migration in Parse.
	tk, err := store.Get("t-mig1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tk.Status != StatusOpen {
		t.Errorf("Status = %q, want %q (auto-migrated from stage implement)", tk.Status, StatusOpen)
	}

	// Update persists the migrated status.
	tk.Priority = 1
	if err := store.Update(tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get("t-mig1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusOpen {
		t.Errorf("Status = %q, want %q after update", got.Status, StatusOpen)
	}
}

func TestFileStore_Delete(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-del1")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete("t-del1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Get("t-del1")
	if err == nil {
		t.Error("Get after Delete should fail")
	}
}

func TestFileStore_List(t *testing.T) {
	store, _ := testStore(t)
	for _, id := range []string{"t-ls01", "t-ls02", "t-ls03"} {
		if err := store.Create(sampleTicket(id)); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	tickets, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tickets) != 3 {
		t.Errorf("len(List) = %d, want 3", len(tickets))
	}
}

// plantUnreadable writes a file into a store directory that no parse can
// structure — the class Parse still refuses once a mistyped field is tolerated.
func plantUnreadable(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("---\nid: x\n  status: open\n---\n# Broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plantTicketFile writes a ticket into a store directory under a filename of
// the caller's choosing, bypassing the write path — which refuses an ID that is
// not its own filename, and would strip a namespace before storing one. It is
// the shape a file synced from another machine can hold.
func plantTicketFile(t *testing.T, dir, name string, tk *Ticket) {
	t.Helper()
	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize %s: %v", tk.ID, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureWarnings redirects the library's warning sink for the test's duration.
func captureWarnings(t *testing.T) *[]string {
	t.Helper()
	var warnings []string
	orig := Warnf
	Warnf = func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }
	t.Cleanup(func() { Warnf = orig })
	return &warnings
}

func TestOneLineFlattensAndDisarmsAReason(t *testing.T) {
	// A skip's reason quotes a file another machine wrote — yaml.v3's TypeError
	// is multi-line and echoes the offending scalar back — and it is printed into
	// a warning block where every line is an entry, on a terminal that acts on
	// escapes.
	got := oneLine(fmt.Errorf("yaml: unmarshal errors:\n  line 2: cannot unmarshal !!str `\x1b[2K\x07evil\n  line 9: spoofed` into bool"))
	if strings.ContainsAny(got, "\n\r\x1b\x07") {
		t.Errorf("oneLine = %q, want no newlines and no control bytes", got)
	}
	if !strings.Contains(got, "cannot unmarshal") || !strings.Contains(got, "evil") {
		t.Errorf("oneLine = %q, want the reason still legible", got)
	}
}

func TestFileStore_ListReportsAndWarnsAboutUnreadableFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectFileStore(dir, "proj")
	if err := store.Create(sampleTicket("t-ls01")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	plantUnreadable(t, dir, "broken-9999.md")

	tickets, skips, err := store.listStored()
	if err != nil {
		t.Fatalf("listStored: %v", err)
	}
	if len(tickets) != 1 || tickets[0].ID != "t-ls01" {
		t.Errorf("listStored returned %d tickets, want the one that parses", len(tickets))
	}
	if len(skips) != 1 || skips[0].File != "broken-9999.md" || skips[0].Error == "" {
		t.Fatalf("skips = %+v, want the unreadable file named with a reason", skips)
	}

	warnings := captureWarnings(t)
	if _, err := store.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(*warnings) != 1 {
		t.Fatalf("List emitted %d warnings, want 1: %v", len(*warnings), *warnings)
	}
	if !strings.Contains((*warnings)[0], "proj/broken-9999.md") || !strings.Contains((*warnings)[0], "not shown") {
		t.Errorf("warning = %q, want it to name the project, the file, and that the ticket is not shown", (*warnings)[0])
	}
}

func TestFileStore_ListWarnsNothingWithoutSkips(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("t-ls01")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	warnings := captureWarnings(t)
	if _, err := store.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(*warnings) != 0 {
		t.Errorf("a store that read in full warned: %v", *warnings)
	}
}

func TestFileStore_ListEmptyDir(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "nonexistent"))
	tickets, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tickets) != 0 {
		t.Errorf("len(List) = %d, want 0", len(tickets))
	}
}

func TestFileStore_Resolve_Exact(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("t-exact")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	path, err := store.Resolve("t-exact")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if filepath.Base(path) != "t-exact.md" {
		t.Errorf("path = %q, want t-exact.md", filepath.Base(path))
	}
}

func TestFileStore_Resolve_Partial(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("t-abcd")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	path, err := store.Resolve("abcd")
	if err != nil {
		t.Fatalf("Resolve partial: %v", err)
	}
	if filepath.Base(path) != "t-abcd.md" {
		t.Errorf("path = %q, want t-abcd.md", filepath.Base(path))
	}
}

func TestFileStore_Resolve_Ambiguous(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("t-ab01")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(sampleTicket("t-ab02")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := store.Resolve("ab0")
	if err == nil {
		t.Error("ambiguous Resolve should fail")
	}
}

func TestFileStore_Resolve_NotFound(t *testing.T) {
	store, _ := testStore(t)
	_, err := store.Resolve("nonexistent")
	if err == nil {
		t.Error("Resolve nonexistent should fail")
	}
}

func TestFileStore_Resolve_NamespacedID(t *testing.T) {
	proj := NewProjectFileStore(t.TempDir(), "alpha")
	if err := proj.Create(sampleTicket("t-ns01")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	path, err := proj.Resolve("alpha/t-ns01")
	if err != nil {
		t.Fatalf("Resolve own-project prefix: %v", err)
	}
	if filepath.Base(path) != "t-ns01.md" {
		t.Errorf("path = %q, want t-ns01.md", filepath.Base(path))
	}

	if _, err := proj.Resolve("beta/t-ns01"); err == nil {
		t.Error("Resolve of another project's prefix should fail")
	}

	bare, _ := testStore(t)
	if err := bare.Create(sampleTicket("t-ns01")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := bare.Resolve("alpha/t-ns01"); err == nil {
		t.Error("Resolve of a namespaced ID with no store project should fail")
	}
}

func TestFileStore_Get_EmptyID(t *testing.T) {
	store, _ := testStore(t)
	// Single-ticket store is the regression scenario: partial matching would
	// resolve an empty ID to the lone ticket.
	if err := store.Create(sampleTicket("t-lone1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, id := range []string{"", "   "} {
		got, err := store.Get(id)
		if err == nil {
			t.Errorf("Get(%q) should fail, got ticket %v", id, got)
		} else if !strings.Contains(err.Error(), "id is required") {
			t.Errorf("Get(%q) error = %q, want to contain %q", id, err.Error(), "id is required")
		}
		if got != nil {
			t.Errorf("Get(%q) ticket = %v, want nil (must not resolve to lone ticket)", id, got)
		}
	}
}

func TestFileStore_Delete_EmptyID(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("t-lone2")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := store.Delete("")
	if err == nil {
		t.Fatal("Delete(\"\") should fail")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("Delete(\"\") error = %q, want to contain %q", err.Error(), "id is required")
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "t-lone2.md")); err != nil {
		t.Errorf("lone ticket file should still exist after Delete(\"\"): %v", err)
	}
}

func TestFileStore_Resolve_ProjectPrefixOnlyID(t *testing.T) {
	// "proj/" strips to an empty bare ID, so the empty-ID guard has to run
	// after the strip: with one ticket it would otherwise resolve to the lone
	// ticket, with several it would report a bogus ambiguous match.
	store := NewProjectFileStore(t.TempDir(), "proj")
	for i, id := range []string{"t-lone3", "t-lone4"} {
		if err := store.Create(sampleTicket(id)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		_, err := store.Resolve("proj/")
		if err == nil {
			t.Fatalf("Resolve(\"proj/\") with %d ticket(s) should fail", i+1)
		}
		if !strings.Contains(err.Error(), "id is required") {
			t.Errorf("Resolve(\"proj/\") error = %q, want to contain %q", err.Error(), "id is required")
		}
	}

	// Delete resolves through the same guard.
	if err := store.Delete("proj/"); err == nil {
		t.Error("Delete(\"proj/\") should fail")
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "t-lone3.md")); err != nil {
		t.Errorf("ticket file should still exist after Delete(\"proj/\"): %v", err)
	}
}

// nestedStore returns a store two directories deep inside an outer temp dir, so
// a traversal ID that escapes the store still lands somewhere the test owns.
func nestedStore(t *testing.T, project string) (outer string, store *FileStore) {
	t.Helper()
	outer = t.TempDir()
	store = NewProjectFileStore(filepath.Join(outer, "repo", ".tickets"), project)
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	return outer, store
}

// assertInvalidError checks that err rejects wantValue by name under wantPrefix
// ("invalid ticket ID" or "invalid project"), so a fix that errors for an
// unrelated reason (e.g. "not found") does not pass.
func assertInvalidError(t *testing.T, what, wantPrefix, wantValue string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s should fail", what)
		return
	}
	if !strings.Contains(err.Error(), wantPrefix) || !strings.Contains(err.Error(), fmt.Sprintf("%q", wantValue)) {
		t.Errorf("%s error = %q, want it to report %s %q", what, err, wantPrefix, wantValue)
	}
}

func TestFileStore_Create_TraversalID(t *testing.T) {
	outer, store := nestedStore(t, "")

	// filepath.Join cleans traversal segments rather than failing, so an ID
	// read from a hand-edited or synced ticket file resolves outside the store.
	for _, id := range []string{"../../evil", "/etc/passwd", "a/b", "foo/"} {
		assertInvalidError(t, fmt.Sprintf("Create(%q)", id), "invalid ticket ID", id, store.Create(sampleTicket(id)))
	}

	err := filepath.Walk(outer, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !strings.HasPrefix(path, store.Dir+string(filepath.Separator)) {
			t.Errorf("file written outside store: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", outer, err)
	}
}

func TestFileStore_Update_TraversalID(t *testing.T) {
	outer, store := nestedStore(t, "")

	// Update writes only over an existing file, so the escape target has to
	// exist for the traversal to be observable.
	victim := filepath.Join(outer, "evil.md")
	const sentinel = "untouched\n"
	if err := os.WriteFile(victim, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	assertInvalidError(t, `Update("../../evil")`, "invalid ticket ID", "../../evil", store.Update(sampleTicket("../../evil")))

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(got) != sentinel {
		t.Errorf("file outside store was overwritten: %q", string(got))
	}
}

func TestFileStore_Resolve_TraversalID(t *testing.T) {
	// Get and Delete reach the filesystem through Resolve, whose exact-match
	// branch builds the same path. A project store is the exposed one: it
	// strips its own prefix, so "proj/../../evil" reaches the join, while a
	// bare store rejects any ID with a slash as another project's.
	outer, store := nestedStore(t, "proj")

	victim := filepath.Join(outer, "evil.md")
	raw := "---\nid: evil\nstatus: ready\ndeps: []\nlinks: []\ncreated: 2026-01-01T00:00:00Z\ntype: feature\npriority: 2\n---\n# Secret\n\nBody.\n"
	if err := os.WriteFile(victim, []byte(raw), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	assertInvalidError(t, `Delete("proj/../../evil")`, "invalid ticket ID", "../../evil", store.Delete("proj/../../evil"))
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("file outside store was deleted: %v", err)
	}

	got, err := store.Get("proj/../../evil")
	assertInvalidError(t, `Get("proj/../../evil")`, "invalid ticket ID", "../../evil", err)
	if got != nil {
		t.Errorf("Get read a ticket outside the store: %v", got)
	}
}

func TestFileStore_RealTicketsDir(t *testing.T) {
	dir := filepath.Join("..", "..", ".tickets")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("no .tickets directory")
	}

	store := NewFileStore(dir)
	tickets, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tickets) == 0 {
		t.Error("expected tickets from real directory")
	}
	t.Logf("listed %d tickets from real .tickets/", len(tickets))

	// Verify partial resolve works.
	if len(tickets) > 0 {
		first := tickets[0]
		// Use just the hex suffix for partial match.
		parts := filepath.Base(first.ID)
		if len(parts) > 2 {
			_, err := store.Get(parts[2:]) // strip "t-" prefix
			if err != nil {
				t.Logf("partial resolve of %q (from %s): %v (may be ambiguous, OK)", parts[2:], first.ID, err)
			}
		}
	}
}

func TestStampUpdatedBumpedOnUpdate(t *testing.T) {
	store, _ := testStore(t)
	tk := &Ticket{ID: "stamp-0001", Title: "Stamp", Status: StatusBacklog, Type: TypeFeature}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	created, err := store.Get("stamp-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if created.Updated.IsZero() {
		t.Fatal("Updated should be set on Create")
	}

	// Backdate the on-disk timestamps so the bump is observable regardless of the
	// clock's resolution: timestamps serialize at RFC3339 second precision, so a
	// sub-second sleep would round-trip to the same stored value.
	created.Created = created.Created.Add(-2 * time.Hour)
	created.Updated = created.Updated.Add(-2 * time.Hour)
	data, err := Serialize(created)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "stamp-0001.md"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	before, err := store.Get("stamp-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Written back from the read that followed the backdating, not from the one
	// before it: Update compares the version a ticket was read at, so the stale
	// copy would be refused as a conflict — which is what it is.
	wasUpdated := before.Updated
	before.Title = "Stamp 2"
	if err := store.Update(before); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := store.Get("stamp-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !updated.Updated.After(wasUpdated) {
		t.Errorf("Updated not bumped: was %v, now %v", wasUpdated, updated.Updated)
	}
}

func TestStampCompletedSetWhenDone(t *testing.T) {
	store, _ := testStore(t)
	tk := &Ticket{ID: "done-0001", Title: "Done", Status: StatusOpen, Type: TypeFeature}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	open, _ := store.Get("done-0001")
	if !open.Completed.IsZero() {
		t.Error("Completed should be zero while open")
	}

	open.Status = StatusDone
	if err := store.Update(open); err != nil {
		t.Fatalf("Update: %v", err)
	}
	done, _ := store.Get("done-0001")
	if done.Completed.IsZero() {
		t.Error("Completed should be set when status becomes done")
	}
}

func TestStampCompletedClearedWhenReopened(t *testing.T) {
	store, _ := testStore(t)
	tk := &Ticket{ID: "reopen-0001", Title: "Reopen", Status: StatusDone, Type: TypeFeature}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	done, _ := store.Get("reopen-0001")
	if done.Completed.IsZero() {
		t.Fatal("Completed should be set for a done ticket")
	}

	done.Status = StatusOpen
	if err := store.Update(done); err != nil {
		t.Fatalf("Update: %v", err)
	}
	reopened, _ := store.Get("reopen-0001")
	if !reopened.Completed.IsZero() {
		t.Errorf("Completed should be cleared when reopened, got %v", reopened.Completed)
	}
}

// Populating outputs on the done transition lives at the store write choke
// point, so every caller gets it — including the TUI, which mutates a ticket
// and calls store.Update directly.
func TestUpdateToDonePopulatesBranchOutput(t *testing.T) {
	store, _ := testStore(t)
	tk := &Ticket{ID: "land-0001", Title: "Land", Status: StatusOpen, Type: TypeFeature, Branch: "feature-x"}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	open, _ := store.Get("land-0001")
	if len(open.Outputs) != 0 {
		t.Fatalf("Outputs = %v, want empty while open", open.Outputs)
	}

	open.Status = StatusDone
	if err := store.Update(open); err != nil {
		t.Fatalf("Update: %v", err)
	}
	done, _ := store.Get("land-0001")
	if done.Outputs[OutputKeyBranch] != "feature-x" {
		t.Errorf("Outputs[branch] = %q, want feature-x", done.Outputs[OutputKeyBranch])
	}
}

func TestUpdateToDoneKeepsCallerBranchOutput(t *testing.T) {
	store, _ := testStore(t)
	tk := &Ticket{ID: "land-0002", Title: "Land", Status: StatusOpen, Type: TypeFeature, Branch: "feature-x"}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	open, _ := store.Get("land-0002")

	// A value the caller set explicitly wins over the derived one.
	open.Status = StatusDone
	open.Outputs = map[string]string{OutputKeyBranch: "manual-branch"}
	if err := store.Update(open); err != nil {
		t.Fatalf("Update: %v", err)
	}
	done, _ := store.Get("land-0002")
	if done.Outputs[OutputKeyBranch] != "manual-branch" {
		t.Errorf("Outputs[branch] = %q, want manual-branch", done.Outputs[OutputKeyBranch])
	}
}

func TestStampCreatedPreservedAcrossUpdate(t *testing.T) {
	store, _ := testStore(t)
	tk := &Ticket{ID: "create-0001", Title: "Create", Status: StatusBacklog, Type: TypeFeature}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	created, _ := store.Get("create-0001")

	time.Sleep(10 * time.Millisecond)
	created.Title = "Changed"
	if err := store.Update(created); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, _ := store.Get("create-0001")
	if !after.Created.Equal(created.Created) {
		t.Errorf("Created changed: was %v, now %v", created.Created, after.Created)
	}
}

func TestFileStore_FileNamingAnotherProjectIsNotReadAsATicket(t *testing.T) {
	// Ticket files arrive over git, so a project directory can hold one whose
	// id names another project. The directory decides which project a ticket
	// belongs to, so such a file is this project's ticket in no reading of it.
	dir := t.TempDir()
	store := NewProjectFileStore(dir, "proj")
	if err := store.Create(sampleTicket("fn-own-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	foreign := sampleTicket("other/fn-alien-0002")
	foreign.Status = StatusDone
	plantTicketFile(t, dir, "fn-alien-0002.md", foreign)

	tickets, skips, err := store.listStored()
	if err != nil {
		t.Fatalf("listStored: %v", err)
	}
	if len(tickets) != 1 || tickets[0].ID != "fn-own-0001" {
		t.Errorf("listStored returned %v, want this project's ticket alone", ids2(tickets))
	}
	if len(skips) != 1 || skips[0].File != "fn-alien-0002.md" || skips[0].Kind != FileSkipForeignNamespace {
		t.Fatalf("skips = %+v, want the planted file skipped as %s", skips, FileSkipForeignNamespace)
	}
	for _, want := range []string{`"other/fn-alien-0002"`, `"other"`, `"proj"`} {
		if !strings.Contains(skips[0].Error, want) {
			t.Errorf("skip reason = %q, want it to name %s", skips[0].Error, want)
		}
	}

	// Refused by Get as well as the listing: depLookup falls through to the
	// store, so a check only in the listing would still let the file answer a
	// bare dep here.
	var mismatch *ForeignNamespaceError
	if _, err := store.Get("fn-alien-0002"); !errors.As(err, &mismatch) {
		t.Fatalf("Get = %v, want a ForeignNamespaceError", err)
	}
	if mismatch.ID != "other/fn-alien-0002" || mismatch.Named != "other" || mismatch.Project != "proj" {
		t.Errorf("error = %+v, want the stored id and both projects", mismatch)
	}

	warnings := captureWarnings(t)
	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "fn-own-0001" {
		t.Errorf("List returned %v, want this project's ticket alone", ids2(listed))
	}
	if len(*warnings) != 1 {
		t.Fatalf("List emitted %d warnings, want 1: %v", len(*warnings), *warnings)
	}
	if !strings.Contains((*warnings)[0], "proj/fn-alien-0002.md") || !strings.Contains((*warnings)[0], "not shown as a ticket here") {
		t.Errorf("warning = %q, want it to name the file and say it is not shown as a ticket here", (*warnings)[0])
	}
	if strings.Contains((*warnings)[0], "could not be read") {
		t.Errorf("warning = %q, want the wording of the file that read fine", (*warnings)[0])
	}
}

func TestFileStore_OwnProjectPrefixReadsAsTheBareID(t *testing.T) {
	// A prefix agreeing with the directory is redundant: every reader
	// downstream matches bare IDs, and MultiStore adds the namespace back.
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewProjectFileStore(dir, "proj")
	plantTicketFile(t, dir, "np-0001.md", sampleTicket("proj/np-0001"))

	warnings := captureWarnings(t)
	tickets, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tickets) != 1 || tickets[0].ID != "np-0001" {
		t.Fatalf("List returned %v, want the bare ID", ids2(tickets))
	}
	if len(*warnings) != 0 {
		t.Errorf("a store holding only its own tickets warned: %v", *warnings)
	}
	got, err := store.Get("np-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "np-0001" {
		t.Errorf("Get ID = %q, want the bare ID", got.ID)
	}

	all, err := NewMultiStore(root).List()
	if err != nil {
		t.Fatalf("MultiStore.List: %v", err)
	}
	if len(all) != 1 || all[0].ID != "proj/np-0001" {
		t.Errorf("MultiStore.List returned %v, want the namespace added once", ids2(all))
	}
}

func TestEpicsDegradeOnAnUnreadableFileAndNotOnAForeignOne(t *testing.T) {
	// An unreadable file could be any epic's child, so a derivation that cannot
	// see it is partial. A file naming another project is no child of any epic
	// here, so degrading on it would stop every epic in the project reading
	// done over a file that was never theirs.
	dir := t.TempDir()
	store := NewProjectFileStore(dir, "proj")
	for _, tk := range []*Ticket{
		mkEpic("fe-epic-0001", StatusBacklog, ""),
		mkWithParent("fe-child-0002", StatusDone, "fe-epic-0001"),
	} {
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", tk.ID, err)
		}
	}
	plantTicketFile(t, dir, "fe-alien-0003.md", sampleTicket("other/fe-alien-0003"))

	captureWarnings(t)
	epic, err := store.Get("fe-epic-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if epic.Status != StatusDone {
		t.Errorf("epic reads %s, want %s — a file naming another project is no child of it", epic.Status, StatusDone)
	}

	plantUnreadable(t, dir, "fe-broken-0004.md")
	epic, err = store.Get("fe-epic-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if epic.Status != StatusBacklog {
		t.Errorf("epic reads %s beside an unreadable file, want %s", epic.Status, StatusBacklog)
	}
}

func TestFileStore_NamespaceWithNoTicketIDAfterItYieldsNoTicket(t *testing.T) {
	// ParseNamespacedID splits on the first separator only, so stripping a
	// prefix naming this project can leave something that is not a ticket ID at
	// all: "proj/a/b" would list as "a/b", which indexes under the bare half "b"
	// and answers a real reference to b. Such a file claims to be this project's
	// ticket, so it may be a local epic's child and degrades the derivation the
	// way an unparseable file does.
	dir := t.TempDir()
	store := NewProjectFileStore(dir, "proj")
	for _, tk := range []*Ticket{
		mkEpic("un-epic-0001", StatusBacklog, ""),
		mkWithParent("un-child-0002", StatusDone, "un-epic-0001"),
	} {
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", tk.ID, err)
		}
	}
	plantTicketFile(t, dir, "un-nested-0003.md", sampleTicket("proj/a/b"))
	plantTicketFile(t, dir, "un-bare-0004.md", sampleTicket("proj/"))

	tickets, skips, err := store.listStored()
	if err != nil {
		t.Fatalf("listStored: %v", err)
	}
	if len(tickets) != 2 {
		t.Errorf("listStored returned %v, want the epic and its child alone", ids2(tickets))
	}
	if len(skips) != 2 {
		t.Fatalf("skips = %+v, want both planted files skipped", skips)
	}
	for _, skip := range skips {
		if skip.Kind != FileSkipUnreadable {
			t.Errorf("skip %+v carries kind %q, want %q — the file claims this project's own namespace", skip, skip.Kind, FileSkipUnreadable)
		}
	}
	reasons := skips[0].Error + " " + skips[1].Error
	if !strings.Contains(reasons, `"proj/a/b"`) || !strings.Contains(reasons, `"proj/"`) {
		t.Errorf("skips = %+v, want each reason to name the stored id", skips)
	}

	var unusable *UnusableIDError
	if _, err := store.Get("un-nested-0003"); !errors.As(err, &unusable) {
		t.Fatalf("Get = %v, want an UnusableIDError", err)
	}
	if unusable.ID != "proj/a/b" {
		t.Errorf("error = %+v, want the stored id", unusable)
	}

	epic, err := store.Get("un-epic-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if epic.Status != StatusBacklog {
		t.Errorf("epic reads %s, want %s — a file claiming this project's namespace could be its child", epic.Status, StatusBacklog)
	}
}
