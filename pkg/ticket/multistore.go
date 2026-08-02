package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

var _ Store = (*MultiStore)(nil)

// MultiStore provides multi-project ticket storage by wrapping a FileStore
// per project subdirectory under a shared root. Ticket IDs are namespaced
// as "project/ticket-id" for cross-project disambiguation.
type MultiStore struct {
	rootDir string
}

// NewMultiStore creates a MultiStore rooted at the given directory.
// Each subdirectory of rootDir is treated as a project with its own tickets.
func NewMultiStore(rootDir string) *MultiStore {
	return &MultiStore{rootDir: rootDir}
}

// Get retrieves a ticket by namespaced ("project/id") or bare ID.
// Bare IDs are resolved across all projects; ambiguous matches return an error.
func (m *MultiStore) Get(id string) (*Ticket, error) {
	proj, ticketID := ParseNamespacedID(id)
	if proj != "" {
		store, err := m.storeFor(proj)
		if err != nil {
			return nil, err
		}
		t, err := store.Get(ticketID)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", proj, err)
		}
		t.ID = FormatNamespacedID(proj, t.ID)
		return t, nil
	}

	return m.resolveAcrossProjects(ticketID, func(store *FileStore, bareID string) (*Ticket, error) {
		return store.Get(bareID)
	})
}

// List returns all tickets from all projects with namespaced IDs.
func (m *MultiStore) List() ([]*Ticket, error) {
	projects, err := m.projects()
	if err != nil {
		return nil, err
	}

	var all []*Ticket
	for _, proj := range projects {
		store, err := m.storeFor(proj)
		if err != nil {
			continue
		}
		tickets, err := store.List()
		if err != nil {
			continue
		}
		for _, t := range tickets {
			t.ID = FormatNamespacedID(proj, t.ID)
			all = append(all, t)
		}
	}
	return all, nil
}

// Create writes a new ticket. The ticket ID must be namespaced ("project/id").
func (m *MultiStore) Create(t *Ticket) error {
	proj, ticketID := ParseNamespacedID(t.ID)
	if proj == "" {
		return fmt.Errorf("project is required for MultiStore.Create — use project/ticket-id format")
	}
	store, err := m.storeFor(proj)
	if err != nil {
		return err
	}
	t.ID = ticketID
	if err := store.Create(t); err != nil {
		t.ID = FormatNamespacedID(proj, t.ID)
		return fmt.Errorf("project %s: %w", proj, err)
	}
	t.ID = FormatNamespacedID(proj, t.ID)
	return nil
}

// Update writes a ticket back to disk. Accepts namespaced or bare IDs.
// Bare IDs are resolved across all projects.
func (m *MultiStore) Update(t *Ticket) error {
	proj, ticketID := ParseNamespacedID(t.ID)
	if proj != "" {
		store, err := m.storeFor(proj)
		if err != nil {
			return err
		}
		t.ID = ticketID
		err = store.Update(t)
		t.ID = FormatNamespacedID(proj, t.ID)
		return err
	}

	// Bare ID — find which project owns it.
	matched, err := m.resolveAcrossProjects(ticketID, func(store *FileStore, bareID string) (*Ticket, error) {
		return store.Get(bareID)
	})
	if err != nil {
		return err
	}
	ownerProject, _ := ParseNamespacedID(matched.ID)
	store, err := m.storeFor(ownerProject)
	if err != nil {
		return err
	}
	t.ID = ticketID
	err = store.Update(t)
	t.ID = FormatNamespacedID(ownerProject, t.ID)
	return err
}

// Delete removes a ticket by namespaced or bare ID.
// Bare IDs are resolved across all projects.
func (m *MultiStore) Delete(id string) error {
	proj, ticketID := ParseNamespacedID(id)
	if proj != "" {
		store, err := m.storeFor(proj)
		if err != nil {
			return err
		}
		return store.Delete(ticketID)
	}

	// Bare ID — find which project owns it.
	matched, err := m.resolveAcrossProjects(ticketID, func(store *FileStore, bareID string) (*Ticket, error) {
		return store.Get(bareID)
	})
	if err != nil {
		return err
	}
	ownerProject, bareID := ParseNamespacedID(matched.ID)
	store, err := m.storeFor(ownerProject)
	if err != nil {
		return err
	}
	return store.Delete(bareID)
}

// projects returns the list of project names (subdirectory names under rootDir).
func (m *MultiStore) projects() ([]string, error) {
	entries, err := os.ReadDir(m.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var projects []string
	for _, e := range entries {
		if e.IsDir() {
			projects = append(projects, e.Name())
		}
	}
	return projects, nil
}

// storeFor returns a FileStore for the given project. The project is the
// directory half of every path this store reaches, and it arrives from the
// prefix of a ticket ID that tk did not necessarily write — so a name that
// traverses out of rootDir is rejected here, the same way ticketFile rejects a
// traversing ID for the filename half.
func (m *MultiStore) storeFor(proj string) (*FileStore, error) {
	if !project.ValidName(proj) {
		return nil, fmt.Errorf("invalid project %q in %s: %s", proj, m.rootDir, bareNameHint)
	}
	return NewProjectFileStore(filepath.Join(m.rootDir, proj), proj), nil
}

// resolveAcrossProjects searches all project stores for a bare ticket ID.
// Returns the matched ticket with a namespaced ID if exactly one match is found.
// Returns an error if no matches or multiple matches (ambiguous).
func (m *MultiStore) resolveAcrossProjects(bareID string, getter func(*FileStore, string) (*Ticket, error)) (*Ticket, error) {
	// Reject empty/whitespace IDs before the per-project loop, where each Get
	// would fail and be swallowed by the continue, yielding a misleading error.
	if strings.TrimSpace(bareID) == "" {
		return nil, fmt.Errorf("id is required")
	}

	projects, err := m.projects()
	if err != nil {
		return nil, err
	}

	type match struct {
		project string
		ticket  *Ticket
	}
	var matches []match

	for _, proj := range projects {
		store, err := m.storeFor(proj)
		if err != nil {
			continue
		}
		t, err := getter(store, bareID)
		if err != nil {
			continue
		}
		matches = append(matches, match{project: proj, ticket: t})
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("ticket %s not found in any project", bareID)
	case 1:
		t := matches[0].ticket
		t.ID = FormatNamespacedID(matches[0].project, t.ID)
		return t, nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.project
		}
		return nil, fmt.Errorf("ambiguous ID %q found in projects: %s", bareID, strings.Join(names, ", "))
	}
}
