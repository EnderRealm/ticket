package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

var _ Store = (*MultiStore)(nil)

// MultiStore provides multi-project ticket storage by wrapping a FileStore
// per project subdirectory under a shared root. Ticket IDs are namespaced
// as "project/ticket-id" for cross-project disambiguation.
//
// Source attributes this store's writes in the mutation log; it is carried into
// every per-project store storeFor builds, and set through WithSource.
type MultiStore struct {
	rootDir string
	Source  string
}

// NewMultiStore creates a MultiStore rooted at the given directory.
// Each subdirectory of rootDir is treated as a project with its own tickets.
func NewMultiStore(rootDir string) *MultiStore {
	return &MultiStore{rootDir: rootDir}
}

// Get retrieves a ticket by namespaced ("project/id") or bare ID.
// Bare IDs are resolved across all projects; ambiguous matches return an error.
func (m *MultiStore) Get(id string) (*Ticket, error) {
	return m.get(id, (*FileStore).Get)
}

// getStored retrieves a ticket without deriving an epic's status, against the
// project store that owns it.
func (m *MultiStore) getStored(id string) (*Ticket, error) {
	return m.get(id, (*FileStore).getStored)
}

func (m *MultiStore) get(id string, read func(*FileStore, string) (*Ticket, error)) (*Ticket, error) {
	proj, ticketID := ParseNamespacedID(id)
	if proj != "" {
		store, err := m.storeFor(proj)
		if err != nil {
			return nil, err
		}
		t, err := read(store, ticketID)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", proj, err)
		}
		t.ID = FormatNamespacedID(proj, t.ID)
		return t, nil
	}

	return m.resolveAcrossProjects(ticketID, read)
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

// Create writes a new ticket. The ticket ID must be namespaced ("project/id")
// and the project must already exist — as a directory under the root, or as a
// central-store entry in config. FileStore.Create ensures its own store dir,
// which under a shared root would otherwise conjure a project from whatever
// name the ID carries: a mistyped name or a temp directory basename lands in
// the store and replicates to every other machine.
//
// The config half of the check covers a freshly cloned central store. Git
// tracks no empty directories, so a registered project that has never held a
// ticket arrives without one, and creating it there is right — `tk init` makes
// the directory before it registers the project, so the name is already ours.
func (m *MultiStore) Create(t *Ticket) error {
	proj, ticketID := ParseNamespacedID(t.ID)
	if proj == "" {
		return fmt.Errorf("project is required for MultiStore.Create — use project/ticket-id format")
	}
	store, err := m.storeFor(proj)
	if err != nil {
		return err
	}
	// Lstat, not Stat: a symlink here — the central store is a git repo and git
	// tracks symlinks, so one can arrive from another committer — would put the
	// write at its target, outside the store, and projects() does not follow it
	// either, so the tickets would be unlistable.
	info, err := os.Lstat(store.Dir)
	switch {
	case err == nil && !info.IsDir():
		return fmt.Errorf("project %q in %s is not a directory — refusing to write outside the store", proj, m.rootDir)
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf("project %q in %s: %w", proj, m.rootDir, err)
	case err != nil:
		cfg, err := project.Load()
		if err != nil {
			return fmt.Errorf("load ticket config: %w", err)
		}
		if !project.CentralRegistered(cfg, proj) {
			return fmt.Errorf("project %q has no ticket directory in %s — run `tk init` in that project's repo to create it", proj, m.rootDir)
		}
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
	return m.update(t, (*FileStore).Update)
}

// saveEdit writes an edit against the project store that owns the ticket, so an
// abandoned epic cascades into its own project's children and no other's. The
// children it closed come back namespaced, like every other ID this store
// reports.
func (m *MultiStore) saveEdit(t *Ticket, statusSet bool) ([]string, error) {
	var closed []string
	err := m.update(t, func(s *FileStore, t *Ticket) error {
		bare, err := s.saveEdit(t, statusSet)
		for _, id := range bare {
			closed = append(closed, FormatNamespacedID(s.Project, id))
		}
		return err
	})
	return closed, err
}

func (m *MultiStore) update(t *Ticket, write func(*FileStore, *Ticket) error) error {
	proj, ticketID := ParseNamespacedID(t.ID)
	if proj != "" {
		store, err := m.storeFor(proj)
		if err != nil {
			return err
		}
		t.ID = ticketID
		err = write(store, t)
		t.ID = FormatNamespacedID(proj, t.ID)
		return err
	}

	// Bare ID — find which project owns it.
	matched, err := m.resolveAcrossProjects(ticketID, (*FileStore).getStored)
	if err != nil {
		return err
	}
	ownerProject, _ := ParseNamespacedID(matched.ID)
	store, err := m.storeFor(ownerProject)
	if err != nil {
		return err
	}
	t.ID = ticketID
	err = write(store, t)
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
	matched, err := m.resolveAcrossProjects(ticketID, (*FileStore).getStored)
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
	store := NewProjectFileStore(filepath.Join(m.rootDir, proj), proj)
	// The attribution follows the write down to the store that performs it,
	// which is where the mutation log is appended.
	store.Source = m.Source
	return store, nil
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
