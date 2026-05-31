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
type FileStore struct {
	Dir string
}

// NewFileStore creates a FileStore rooted at the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Dir: dir}
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
	if err := s.EnsureDir(); err != nil {
		return err
	}

	// Check for existing ticket with the same ID.
	path := s.ticketFile(t.ID)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("ticket %s already exists", t.ID)
	}

	// Retry on hash collision (different title, same 4-char hash).
	// Propagation is intentionally NOT triggered on Create — adding a new
	// child does not mean its parent just transitioned; the user can set
	// parent state explicitly. Propagation fires on Update.
	const maxRetries = 5
	for i := 0; i < maxRetries; i++ {
		path = s.ticketFile(t.ID)
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
	// Verify the file exists and read the previous status.
	path := s.ticketFile(t.ID)
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
// Returns an error if the ID is ambiguous (multiple matches) or not found.
func (s *FileStore) Resolve(id string) (string, error) {
	// Try exact match first.
	exact := s.ticketFile(id)
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

func (s *FileStore) ticketFile(id string) string {
	return filepath.Join(s.Dir, id+".md")
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
	stampTimestamps(t)
	data, err := Serialize(t)
	if err != nil {
		return err
	}
	return os.WriteFile(s.ticketFile(t.ID), data, 0o644)
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
