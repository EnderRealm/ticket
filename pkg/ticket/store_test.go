package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) (*FileStore, func()) {
	t.Helper()
	dir := t.TempDir()
	store := NewFileStore(dir)
	return store, func() {}
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

	created.Title = "Stamp 2"
	if err := store.Update(created); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := store.Get("stamp-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !updated.Updated.After(before.Updated) {
		t.Errorf("Updated not bumped: was %v, now %v", before.Updated, updated.Updated)
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
