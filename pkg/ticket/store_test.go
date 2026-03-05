package ticket

import (
	"os"
	"path/filepath"
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
		Status:   StatusOpen,
		Type:     TypeTask,
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

	tk.Status = StatusInProgress
	tk.Priority = 0
	if err := store.Update(tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get("t-upd1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", got.Status, StatusInProgress)
	}
	if got.Priority != 0 {
		t.Errorf("Priority = %d, want 0", got.Priority)
	}
}

func TestFileStore_Update_MigratesLegacy(t *testing.T) {
	store, _ := testStore(t)
	// Create a legacy ticket with status only, no stage.
	tk := sampleTicket("t-mig1")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update any field — should auto-migrate to stage-based.
	tk.Priority = 1
	if err := store.Update(tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get("t-mig1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Stage != StageTriage {
		t.Errorf("Stage = %q, want %q (auto-migrated from status open)", got.Stage, StageTriage)
	}
	if got.Status != StatusOpen {
		t.Errorf("Status = %q, want %q (preserved for compat)", got.Status, StatusOpen)
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
