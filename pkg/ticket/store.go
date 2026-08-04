package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store defines the interface for ticket storage backends.
type Store interface {
	Get(id string) (*Ticket, error)
	List() ([]*Ticket, error)
	Create(t *Ticket) error
	Update(t *Ticket) error
	Delete(id string) error
}

var _ Store = (*FileStore)(nil)

// FileStore provides filesystem-backed CRUD operations for tickets.
// Project is the namespace this store's tickets live under; it is empty for
// single-project stores that never see namespaced IDs.
type FileStore struct {
	Dir     string
	Project string
}

// NewFileStore creates a FileStore rooted at the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Dir: dir}
}

// NewProjectFileStore creates a FileStore rooted at the given directory that
// also answers to IDs namespaced under project.
func NewProjectFileStore(dir, project string) *FileStore {
	return &FileStore{Dir: dir, Project: project}
}

// EnsureDir creates the tickets directory if it doesn't exist.
func (s *FileStore) EnsureDir() error {
	return os.MkdirAll(s.Dir, 0o755)
}

// Create writes a new ticket to disk. The ticket must already have an ID.
// If the ID collides with an existing ticket, a new ID is generated and
// the ticket is retried (up to 5 attempts).
func (s *FileStore) Create(t *Ticket) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if err := ValidateStateTransition(s, t); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if err := ResolveParent(s, t); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if err := s.EnsureDir(); err != nil {
		return err
	}

	// Check for existing ticket with the same ID.
	path, err := s.ticketFile(t.ID)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("ticket %s already exists", t.ID)
	}

	// Retry on hash collision (different title, same 4-char hash).
	// Propagation is intentionally NOT triggered on Create — adding a new
	// child does not mean its parent just transitioned; the user can set
	// parent state explicitly. Propagation fires on Update.
	const maxRetries = 5
	for i := 0; i < maxRetries; i++ {
		path, err = s.ticketFile(t.ID)
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return s.writeTicket(t)
		}
		t.ID = GenerateID(t.Title)
	}
	return fmt.Errorf("ticket ID collision after %d attempts", maxRetries)
}

// Get retrieves a ticket by exact or partial ID.
func (s *FileStore) Get(id string) (*Ticket, error) {
	path, err := s.Resolve(id)
	if err != nil {
		return nil, err
	}
	return s.readFile(path)
}

// Update writes a ticket back to disk in canonical format.
// When t.Status has changed, the write is validated (e.g. epics can't be
// marked done while children are still open) and the parent epic chain is
// updated via PropagateStatusUp.
func (s *FileStore) Update(t *Ticket) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	// Checked on every update, not only when parent changes: a ticket that
	// predates the one-level rule must be fixed before any of its fields is
	// written back.
	if err := ResolveParent(s, t); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	// Verify the file exists and read the previous status.
	path, err := s.ticketFile(t.ID)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("ticket %s not found", t.ID)
	}
	prev, err := s.readFile(path)
	statusChanged := err != nil || prev.Status != t.Status

	if statusChanged {
		if err := ValidateStateTransition(s, t); err != nil {
			return fmt.Errorf("update: %w", err)
		}
	}
	if err := s.writeTicket(t); err != nil {
		return err
	}
	if statusChanged {
		return PropagateStatusUp(s, t)
	}
	return nil
}

// Delete removes a ticket file by exact or partial ID.
func (s *FileStore) Delete(id string) error {
	path, err := s.Resolve(id)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// List reads all tickets from the directory.
func (s *FileStore) List() ([]*Ticket, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tickets []*Ticket
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t, err := s.readFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			// Skip unparseable files rather than failing the entire list.
			continue
		}
		tickets = append(tickets, t)
	}
	return tickets, nil
}

// Resolve finds the full file path for an exact or partial ticket ID.
// An ID namespaced under this store's own project resolves against its bare
// remainder — parent, dep, and link fields written through MultiStore carry
// the prefix, and propagation reads them back at every level of the chain.
// Returns an error if the ID is ambiguous (multiple matches) or not found.
func (s *FileStore) Resolve(id string) (string, error) {
	// A prefix naming a different project is rejected, not stripped: stripping
	// it would resolve against a same-suffix ticket in this project and mutate
	// the wrong one. Cross-project references simply do not resolve here.
	if project, bare := ParseNamespacedID(id); project != "" {
		if project != s.Project {
			return "", fmt.Errorf("ticket %s not found", id)
		}
		id = bare
	}

	// Reject empty/whitespace IDs so partial matching does not silently match
	// every ticket (in a single-ticket store this resolves to the lone ticket).
	// Checked after the strip so a prefix-only "project/" is rejected too.
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("id is required")
	}

	// Try exact match first.
	exact, err := s.ticketFile(id)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(exact); err == nil {
		return exact, nil
	}

	// Partial match: find files containing the partial ID.
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return "", fmt.Errorf("ticket %s not found", id)
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		base := strings.TrimSuffix(name, ".md")
		if strings.Contains(base, id) {
			matches = append(matches, filepath.Join(s.Dir, name))
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("ticket %s not found", id)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = strings.TrimSuffix(filepath.Base(m), ".md")
		}
		return "", fmt.Errorf("ambiguous ID %q matches: %s", id, strings.Join(ids, ", "))
	}
}

// bareNameHint is the shared tail of the ID and project path guards. The
// rejected value can come from a ticket file's id field or straight off the
// command line, so it states the constraint without asserting a provenance.
const bareNameHint = "must be a bare name with no path separators — check the id you passed or the id field in the ticket file it came from"

// ticketFile maps a ticket ID to its file path. IDs reach it from ticket files
// that tk did not necessarily write (hand-edited, synced, produced by another
// tool), and filepath.Join cleans traversal segments instead of failing — so an
// ID like "../../evil" would resolve outside s.Dir. Requiring the ID to equal
// its own filepath.Base keeps it a single path element, and ".md" is appended
// before the join so that element can never be "." or ".." either. Every path a
// FileStore builds from a ticket ID goes through here, which bounds the filename
// half; on a MultiStore the directory half comes from an ID's project prefix and
// is bounded in storeFor. The bound is lexical — it constrains the path string,
// not the inode it resolves to, so a symlinked ticket file inside the store
// still redirects the write.
func (s *FileStore) ticketFile(id string) (string, error) {
	if id != filepath.Base(id) {
		return "", fmt.Errorf("invalid ticket ID %q in %s: %s", id, s.Dir, bareNameHint)
	}
	return filepath.Join(s.Dir, id+".md"), nil
}

func (s *FileStore) readFile(path string) (*Ticket, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// stampTimestamps maintains the created/updated/completed fields. It is the
// single write choke point so CLI, MCP, and TUI callers all get consistent
// timestamps. Rules are stateless (no previous status needed):
//   - updated is always set to now.
//   - created is set to now only if unset (a move carries it over).
//   - completed is set to now when the status is done/closed and it is unset;
//     it is cleared when the status is neither done nor closed.
func stampTimestamps(t *Ticket) {
	now := time.Now().UTC()
	t.Updated = now
	if t.Created.IsZero() {
		t.Created = now
	}
	if t.Status == StatusDone || t.Status == StatusClosed {
		if t.Completed.IsZero() {
			t.Completed = now
		}
	} else {
		t.Completed = time.Time{}
	}
}

func (s *FileStore) writeTicket(t *Ticket) error {
	// Resolve the path before the stamping below, which mutates the caller's
	// ticket: a rejected ID must not leave it looking as if it were persisted.
	path, err := s.ticketFile(t.ID)
	if err != nil {
		return err
	}
	stampTimestamps(t)
	// A landed ticket records the branch it landed on. Done at the same write
	// choke point as the timestamps so CLI, MCP, and TUI callers all get it;
	// fill-if-absent leaves any value the caller set explicitly.
	if t.Status == StatusDone {
		PopulateDoneOutputs(t, "", t.Branch)
	}
	data, err := Serialize(t)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// FindTicketsDir walks up from startDir looking for a .tickets/ subdirectory.
// startDir should be an absolute path. Returns the path and true if found,
// or empty string and false.
func FindTicketsDir(startDir string) (string, bool) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, ".tickets")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}
