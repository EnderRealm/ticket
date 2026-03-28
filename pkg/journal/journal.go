package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// StatePath returns ~/.ticket/state/<project>/.
func StatePath(project string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ticket", "state", project), nil
}

// JournalPath returns ~/.ticket/state/<project>/commits.jsonl.
func JournalPath(project string) (string, error) {
	dir, err := StatePath(project)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "commits.jsonl"), nil
}

// ReadEntries loads all commit journal entries for the given project.
// Malformed lines are silently skipped. Missing file returns empty slice.
func ReadEntries(project string) ([]Entry, error) {
	if strings.TrimSpace(project) == "" {
		return []Entry{}, nil
	}
	path, err := JournalPath(project)
	if err != nil {
		return nil, err
	}
	return ReadEntriesFromPath(path)
}

// ReadEntriesFromPath loads entries from a specific JSONL file path.
func ReadEntriesFromPath(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		out = append(out, entry)
	}
	return out, scanner.Err()
}

// AppendEntries appends entries to the project's commits.jsonl, creating parent
// directories if needed. Entries whose SHA+Ticket pair already exists in the
// file are skipped.
func AppendEntries(project string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	path, err := JournalPath(project)
	if err != nil {
		return err
	}
	return AppendEntriesToPath(path, entries)
}

// AppendEntriesToPath appends entries to a specific JSONL path with dedup.
func AppendEntriesToPath(path string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	existing, err := loadExistingKeys(path)
	if err != nil {
		return err
	}

	var toWrite []Entry
	for _, e := range entries {
		key := e.SHA + ":" + e.Ticket
		if _, ok := existing[key]; ok {
			continue
		}
		existing[key] = struct{}{}
		toWrite = append(toWrite, e)
	}
	if len(toWrite) == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, e := range toWrite {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// loadExistingKeys reads existing SHA:Ticket pairs from a JSONL file.
func loadExistingKeys(path string) (map[string]struct{}, error) {
	keys := map[string]struct{}{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return keys, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.SHA != "" {
			keys[e.SHA+":"+e.Ticket] = struct{}{}
		}
	}
	return keys, scanner.Err()
}

// FilterByTickets returns entries whose Ticket is in the given set of IDs.
func FilterByTickets(entries []Entry, ids []string) []Entry {
	set := map[string]struct{}{}
	for _, id := range ids {
		set[id] = struct{}{}
	}
	var out []Entry
	for _, e := range entries {
		if _, ok := set[e.Ticket]; ok {
			out = append(out, e)
		}
	}
	return out
}

// CountForTicket counts entries for a specific ticket ID.
func CountForTicket(entries []Entry, ticketID string) int {
	n := 0
	for _, e := range entries {
		if e.Ticket == ticketID {
			n++
		}
	}
	return n
}

// LastN returns the last n entries (or all if fewer).
func LastN(entries []Entry, n int) []Entry {
	if len(entries) <= n {
		return entries
	}
	return entries[len(entries)-n:]
}
