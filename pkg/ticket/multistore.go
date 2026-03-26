package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	project, ticketID := ParseNamespacedID(id)
	if project != "" {
		t, err := m.storeFor(project).Get(ticketID)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project, err)
		}
		t.ID = FormatNamespacedID(project, t.ID)
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
		tickets, err := m.storeFor(proj).List()
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
	project, ticketID := ParseNamespacedID(t.ID)
	if project == "" {
		return fmt.Errorf("project is required for MultiStore.Create — use project/ticket-id format")
	}
	t.ID = ticketID
	if err := m.storeFor(project).Create(t); err != nil {
		t.ID = FormatNamespacedID(project, t.ID)
		return fmt.Errorf("project %s: %w", project, err)
	}
	t.ID = FormatNamespacedID(project, t.ID)
	return nil
}

// Update writes a ticket back to disk. Accepts namespaced or bare IDs.
// Bare IDs are resolved across all projects.
func (m *MultiStore) Update(t *Ticket) error {
	project, ticketID := ParseNamespacedID(t.ID)
	if project != "" {
		t.ID = ticketID
		err := m.storeFor(project).Update(t)
		t.ID = FormatNamespacedID(project, t.ID)
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
	t.ID = ticketID
	err = m.storeFor(ownerProject).Update(t)
	t.ID = FormatNamespacedID(ownerProject, t.ID)
	return err
}

// Delete removes a ticket by namespaced or bare ID.
// Bare IDs are resolved across all projects.
func (m *MultiStore) Delete(id string) error {
	project, ticketID := ParseNamespacedID(id)
	if project != "" {
		return m.storeFor(project).Delete(ticketID)
	}

	// Bare ID — find which project owns it.
	matched, err := m.resolveAcrossProjects(ticketID, func(store *FileStore, bareID string) (*Ticket, error) {
		return store.Get(bareID)
	})
	if err != nil {
		return err
	}
	ownerProject, bareID := ParseNamespacedID(matched.ID)
	return m.storeFor(ownerProject).Delete(bareID)
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

// storeFor returns a FileStore for the given project.
func (m *MultiStore) storeFor(project string) *FileStore {
	return NewFileStore(filepath.Join(m.rootDir, project))
}

// resolveAcrossProjects searches all project stores for a bare ticket ID.
// Returns the matched ticket with a namespaced ID if exactly one match is found.
// Returns an error if no matches or multiple matches (ambiguous).
func (m *MultiStore) resolveAcrossProjects(bareID string, getter func(*FileStore, string) (*Ticket, error)) (*Ticket, error) {
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
		t, err := getter(m.storeFor(proj), bareID)
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
