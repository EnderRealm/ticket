package ticket

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrConflict reports a write refused because the ticket's file changed between
// the read the caller modified and the write it attempted. It is the outcome a
// caller can act on — re-read the ticket and apply the change again — and it
// exists so that a losing writer never silently discards the winner's work.
// Match it with errors.Is; every path that surfaces it wraps it with the ticket
// ID.
var ErrConflict = errors.New("ticket changed on disk since it was read")

// versionOf identifies a ticket file by its bytes. Update compares the version
// a ticket was read at against the file's current one, so a write only lands on
// the state it was computed from.
func versionOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// lockTicket takes the exclusive advisory lock covering one ticket and returns
// the function that releases it. It blocks until the lock is free.
//
// Per ticket, not per store: writers to different tickets touch different files
// and have nothing to serialise, so one lock over the store would put every
// unrelated write behind whichever one holds it.
//
// The scope is one machine. flock coordinates the tk processes on a host — a
// `tk serve` per agent, a CLI run beside them — which is where overlapping
// writes come from. A store shared between machines is exchanged by git
// commits, not by concurrent access to one directory, and flock over a network
// filesystem does not reliably lock at all.
func (s *FileStore) lockTicket(id string) (func(), error) {
	path, err := s.lockFile(id)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", id, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", id, err)
	}
	// Closing releases the lock: an flock belongs to the open file description,
	// and this descriptor is the only one holding it.
	return func() { f.Close() }, nil
}

// lockFile is the path of the lock file guarding one ticket, creating the
// directory that holds it.
//
// The lock cannot be the ticket file itself: writeTicket replaces that file by
// rename, so two writers would end up holding flocks on two different inodes
// and would exclude nothing. A lock file is created once and never removed, so
// every process opens the same inode.
//
// It lives outside the ticket store because `tk sync` stages tickets/ wholesale
// (centralStorePaths in cmd/sync.go): a lock file under the store directory
// would be committed and shipped to every other machine. It lives under the
// user's cache directory rather than in os.TempDir because the property that
// matters — that no other local user can reach it — has to be structural. A
// world-writable /tmp, which is what a Linux host has, lets another user
// pre-create the directory tk would have made: MkdirAll returns nil for a path
// that already exists whoever owns it, and from there they could hold the flock
// that blocks every writer, unlink lock files so two writers land on different
// inodes, or plant a symlink for tk's O_CREATE to follow. A per-user cache
// directory is owned by its user by construction; the 0700 mode below is
// defence in depth, not the guarantee.
//
// Like the journal, it stays under HOME and is not moved by TK_STORE_ROOT: the
// override relocates the store, and locks are about the processes on this
// machine rather than about the store's contents.
//
// One flat directory, with the canonicalized store directory hashed into each
// file's name so two stores on a host do not share a lock. A truncated hash is
// enough: a collision would need two stores agreeing on 48 bits and holding the
// same ticket ID, and it would over-serialise rather than under-serialise. The
// ticket ID stays legible in the name so the directory can be read while
// debugging a stuck writer.
//
// Residuals, bounded rather than handled. Lock files are never removed, so a
// deleted ticket leaves one behind and every throwaway store a test builds adds
// its own — each is an empty file in one directory. And a cleaner unlinking a
// lock file between two processes' opens would leave them on different inodes,
// excluding nothing; a lock is held for microseconds, and nothing sweeps a
// cache directory on that scale.
func (s *FileStore) lockFile(id string) (string, error) {
	if id == "" || id != filepath.Base(id) {
		return "", fmt.Errorf("invalid ticket ID %q in %s: %s", id, s.Dir, bareNameHint)
	}
	dir, err := storeDir(s)
	if err != nil {
		return "", err
	}
	// A hard failure, not a fallback to a shared directory: an environment with
	// neither XDG_CACHE_HOME nor HOME is a misconfiguration the caller can fix,
	// and falling back to os.TempDir would restore exactly the exposure this
	// avoids — silently, on the one platform where it matters.
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate the ticket lock directory: %w", err)
	}
	locks := filepath.Join(cache, "tk", "locks")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(dir))
	return filepath.Join(locks, hex.EncodeToString(key[:6])+"-"+id+".lock"), nil
}

// mutateRetries bounds the fallback path's re-reads. A store outside this
// package cannot lock, so it can only retry; five attempts is the same bound
// Create uses for ID collisions.
const mutateRetries = 5

// Mutate applies fn to a ticket and writes the result, holding the ticket's
// lock across the read, the change and the write. It is the write path for an
// accumulating change — a note, a dep, a link — where the new value is computed
// from the stored one and a conflict error would leave the caller with nothing
// to do but read and apply it again.
//
// fn receives the ticket as Store.Get returns it and must only mutate that
// struct. It must not write the same ticket through the store: that deadlocks,
// because the lock is held on a descriptor this call owns and the nested write
// would block acquiring a second one. Reading is safe — no read path takes the
// lock, and mutate itself reads the ticket and resolves its parent while
// holding it. fn must not change the ticket's ID either: the lock and the file
// are both keyed on it.
//
// A store that cannot lock (an implementation outside this package) falls back
// to Get/fn/Update, retrying on ErrConflict. That loses no data but re-runs fn.
func Mutate(store Store, id string, fn func(*Ticket) error) (*Ticket, error) {
	if m, ok := store.(mutator); ok {
		return m.mutate(id, fn)
	}
	var err error
	for i := 0; i < mutateRetries; i++ {
		var t *Ticket
		if t, err = store.Get(id); err != nil {
			return nil, err
		}
		if err = fn(t); err != nil {
			return nil, err
		}
		if err = store.Update(t); err == nil {
			return t, nil
		}
		if !errors.Is(err, ErrConflict) {
			return nil, err
		}
	}
	return nil, err
}

// mutator is the store side of Mutate. An unexported interface rather than a
// Store method so no implementation outside this package is forced to grow one.
type mutator interface {
	mutate(id string, fn func(*Ticket) error) (*Ticket, error)
}

var (
	_ mutator = (*FileStore)(nil)
	_ mutator = (*MultiStore)(nil)
)

// mutate holds the ticket's lock across the whole read-modify-write, so the
// state fn is handed is the state the write lands on. It writes through
// updateLocked rather than Update: re-entering Update would block forever on the
// lock this call already holds, on a second descriptor of the same lock file.
func (s *FileStore) mutate(id string, fn func(*Ticket) error) (*Ticket, error) {
	path, err := s.Resolve(id)
	if err != nil {
		return nil, err
	}
	// Keyed on the resolved ID, not the caller's: a partial ID names a file, and
	// two callers spelling one ticket differently have to take the same lock.
	resolved := strings.TrimSuffix(filepath.Base(path), ".md")
	release, err := s.lockTicket(resolved)
	if err != nil {
		return nil, err
	}
	defer release()

	t, err := s.Get(resolved)
	if err != nil {
		return nil, err
	}
	if err := fn(t); err != nil {
		return nil, err
	}
	// The lock and the write are both keyed on the file this resolved to, so a
	// ticket that ends up under another ID — a mutation that renamed it, or a
	// file whose stored id disagrees with its own name — is refused rather than
	// written to a file nothing locked.
	if t.ID != resolved {
		return nil, fmt.Errorf("%s reads as %s: a mutation writes the file it was read from, and these do not agree", resolved, t.ID)
	}
	// The checks Update makes before it writes. They run inside the lock here
	// because the read they validate is inside it too.
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	if err := ResolveParent(s, t); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	if err := s.updateLocked(t); err != nil {
		return nil, err
	}
	return t, nil
}

// mutate routes to the project store that owns the ticket, so the lock taken is
// the one that store's own writers take.
func (m *MultiStore) mutate(id string, fn func(*Ticket) error) (*Ticket, error) {
	proj, ticketID := ParseNamespacedID(id)
	if proj == "" {
		matched, err := m.resolveAcrossProjects(ticketID, (*FileStore).getStored)
		if err != nil {
			return nil, err
		}
		proj, ticketID = ParseNamespacedID(matched.ID)
	}
	store, err := m.storeFor(proj)
	if err != nil {
		return nil, err
	}
	// fn sees the namespaced ID Get would have handed it, and the bare half goes
	// back before the write — the same swap m.update makes around Update.
	t, err := store.mutate(ticketID, func(t *Ticket) error {
		t.ID = FormatNamespacedID(proj, t.ID)
		defer func() { _, t.ID = ParseNamespacedID(t.ID) }()
		return fn(t)
	})
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", proj, err)
	}
	t.ID = FormatNamespacedID(proj, t.ID)
	return t, nil
}
