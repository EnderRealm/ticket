package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// parentStore configures a central-store project holding one feature and one
// epic.
func parentStore(t *testing.T) *ticket.FileStore {
	t.Helper()
	store := centralStore(t, "pa-parent")
	for _, tk := range []*ticket.Ticket{
		{ID: "pa-feat-0001", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: time.Now(), Title: "A feature", Body: "\n"},
		{ID: "pa-epic-0002", Status: ticket.StatusBacklog, Type: ticket.TypeEpic, Created: time.Now(), Title: "An epic", Body: "\n"},
	} {
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", tk.ID, err)
		}
	}
	return store
}

func TestCreateRejectsNonEpicParent(t *testing.T) {
	parentStore(t)

	if err := createCmd.Flags().Set("parent", "pa-feat-0001"); err != nil {
		t.Fatal(err)
	}
	defer createCmd.Flags().Set("parent", "")

	err := runCreate(createCmd, []string{"Child of a feature"})
	if err == nil {
		t.Fatal("expected create with a non-epic parent to fail, got nil")
	}
	if !strings.Contains(err.Error(), "pa-feat-0001") || !strings.Contains(err.Error(), "not an epic") {
		t.Errorf("error should name the parent and its type, got: %v", err)
	}
}

func TestCreateAcceptsEpicParent(t *testing.T) {
	parentStore(t)

	if err := createCmd.Flags().Set("parent", "pa-epic-0002"); err != nil {
		t.Fatal(err)
	}
	defer createCmd.Flags().Set("parent", "")

	if err := runCreate(createCmd, []string{"Child of an epic"}); err != nil {
		t.Fatalf("create under an epic should succeed: %v", err)
	}
}

func TestEditRejectsNonEpicParent(t *testing.T) {
	store := parentStore(t)
	child := &ticket.Ticket{
		ID: "pa-child-0003", Status: ticket.StatusOpen, Type: ticket.TypeFeature,
		Parent: "pa-epic-0002", Created: time.Now(), Title: "A child", Body: "\n",
	}
	if err := store.Create(child); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := runEditWith(t, "pa-child-0003", "parent", "pa-feat-0001")
	if err == nil {
		t.Fatal("expected edit repointing a parent at a feature to fail, got nil")
	}
	if !strings.Contains(err.Error(), "pa-feat-0001") || !strings.Contains(err.Error(), "not an epic") {
		t.Errorf("error should name the parent and its type, got: %v", err)
	}
}

func TestEditClearsParent(t *testing.T) {
	// "Clear it" is half the remedy the validation error names, so an explicit
	// empty --parent has to clear the field.
	store := parentStore(t)
	child := &ticket.Ticket{
		ID: "pa-child-0004", Status: ticket.StatusOpen, Type: ticket.TypeFeature,
		Parent: "pa-epic-0002", Created: time.Now(), Title: "A child", Body: "\n",
	}
	if err := store.Create(child); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := runEditWith(t, "pa-child-0004", "parent", ""); err != nil {
		t.Fatalf("edit clearing the parent should succeed: %v", err)
	}
	stored, err := store.Get("pa-child-0004")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Parent != "" {
		t.Errorf("parent = %q, want cleared", stored.Parent)
	}
}
