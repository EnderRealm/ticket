package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
