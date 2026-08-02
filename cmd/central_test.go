package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// centralStore registers the test process's working directory as a
// central-store project and returns a project-scoped store over its central
// ticket dir. Commands resolve the same dir through TicketsDir().
func centralStore(t *testing.T, name string) *ticket.FileStore {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TICKETS_DIR", "")

	centralRoot := t.TempDir()
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			name: {Path: mustGetwd(), Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	dir := filepath.Join(centralRoot, "tickets", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return ticket.NewProjectFileStore(dir, name)
}

// mkCentral creates a ticket in the given store.
func mkCentral(t *testing.T, store *ticket.FileStore, id string, typ ticket.TicketType, status ticket.Status, parent string) {
	t.Helper()
	tk := &ticket.Ticket{
		ID:      id,
		Status:  status,
		Type:    typ,
		Parent:  parent,
		Created: time.Now(),
		Title:   "Ticket " + id,
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
}

func TestEditPropagatesToNamespacedParent(t *testing.T) {
	store := centralStore(t, "cs-prop")
	mkCentral(t, store, "epic-1111", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, store, "child-2222", ticket.TypeFeature, ticket.StatusOpen, "cs-prop/epic-1111")

	if err := runEditWith(t, "child-2222", "status", "done"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	epic, err := store.Get("epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != ticket.StatusDone {
		t.Errorf("epic status = %q, want %q (CLI write must propagate to a namespaced parent)", epic.Status, ticket.StatusDone)
	}
}

func TestEditEpicDoneGuardWithNamespacedChild(t *testing.T) {
	store := centralStore(t, "cs-guard")
	mkCentral(t, store, "epic-3333", ticket.TypeEpic, ticket.StatusOpen, "")
	mkCentral(t, store, "child-4444", ticket.TypeFeature, ticket.StatusOpen, "cs-guard/epic-3333")

	err := runEditWith(t, "epic-3333", "status", "done")
	if err == nil {
		t.Fatal("expected error marking epic done with an open namespaced child, got nil")
	}
	if !contains(err.Error(), "child-4444") {
		t.Errorf("error should name child-4444, got: %v", err)
	}
}

func TestTicketStoreCentralProject(t *testing.T) {
	store := centralStore(t, "cs-scope")

	got := TicketStore()
	if got.Project != "cs-scope" {
		t.Errorf("Project = %q, want %q", got.Project, "cs-scope")
	}
	if got.Dir != store.Dir {
		t.Errorf("Dir = %q, want %q", got.Dir, store.Dir)
	}
}

func TestTicketStoreEnvDirHasNoProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TICKETS_DIR", dir)

	got := TicketStore()
	if got.Project != "" {
		t.Errorf("Project = %q, want empty (TICKETS_DIR is a single-project store)", got.Project)
	}
	if got.Dir != dir {
		t.Errorf("Dir = %q, want %q", got.Dir, dir)
	}
}

func TestTicketStoreLocalRepoHasNoProject(t *testing.T) {
	// A repo with its own .tickets/ never sees namespaced IDs, so accepting
	// them would let a "someproject/foo-abcd" reference resolve locally.
	repo := t.TempDir()
	ticketsDir := filepath.Join(repo, ".tickets")
	if err := os.MkdirAll(ticketsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	repoFlag = repo
	defer func() { repoFlag = "" }()

	got := TicketStore()
	if got.Project != "" {
		t.Errorf("Project = %q, want empty (local .tickets is a single-project store)", got.Project)
	}
	if got.Dir != ticketsDir {
		t.Errorf("Dir = %q, want %q", got.Dir, ticketsDir)
	}
}
