package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MoveResult describes a single ticket move operation.
type MoveResult struct {
	OldID string
	NewID string
}

// MoveTicket moves a single ticket from src store to dst store. The ticket is
// closed in src with a note and created in dst with a new ID: copy-and-close,
// not reassignment, so the ID other tickets and commit messages reference is
// not carried over. That is why only an isolated leaf moves. A ticket with a
// parent, a dep or a link, or one another ticket names, would leave behind a
// reference to a closed copy — a blocker that reads satisfied while its
// replacement is unfinished; an epic's children would be orphaned or dragged
// along. Each is refused, before any write, naming the relationship.
// recursive is kept for the callers that pass it and is refused when set.
//
// Both stores are resolved by the caller (ResolveStoreForRepo), which is where
// a destination that resolves to no store is refused: nothing here checks that
// dst.Dir exists, because a registered central project that has never held a
// ticket legitimately has no directory yet and the create makes it. A dst that
// is the same directory as src is refused here, before anything is written,
// as is one in a different central store: the move runs under one hold of the
// source's store lock, which has to cover both ends.
//
// The move is not atomic and nothing is rolled back on failure: the error
// names a ticket already written to dst whose source copy is unchanged, so it
// can be reconciled.
func MoveTicket(src, dst *FileStore, id string, recursive bool) ([]MoveResult, error) {
	srcWhere, err := storeLabel(src)
	if err != nil {
		return nil, err
	}
	dstWhere, err := storeLabel(dst)
	if err != nil {
		return nil, err
	}

	// Refused here rather than in the callers, and before anything is read or
	// written: a destination that resolves to the source store would rename the
	// ticket — new ID, old one closed — for no gain.
	same, err := sameStoreDir(src, dst)
	if err != nil {
		return nil, err
	}
	if same {
		// Says only what the guard checked. It runs ahead of the read, so the
		// requested ID has not been looked up yet and naming it as living here
		// would be an unverified claim on a typo'd ID.
		return nil, fmt.Errorf("destination resolves to %s, the source store — no move performed", srcWhere)
	}
	if recursive {
		return nil, fmt.Errorf("recursive moves are refused: a move copies a ticket under a new ID and closes the original, and an epic's children would be left naming a closed copy. Move isolated leaves one at a time — no move performed")
	}
	central := centralFor(src)
	shared, err := central.sameCentral(centralFor(dst))
	if err != nil {
		return nil, err
	}
	if !shared {
		return nil, fmt.Errorf("%s and %s are not in the same central store — no move performed", srcWhere, dstWhere)
	}

	var results []MoveResult
	err = central.write(src.Project, func(o *op) error {
		// The catalog guard the destination's own write entry points would have
		// made — write guards the source's namespace, and the move writes into
		// both through op.create and op.update, which take the boundary as
		// already guarded: a Root that is not activated or a feature this
		// binary lacks refuses the move as it refuses a create. Under the store
		// lock, so the catalog judged is the one the write lands under.
		if err := dst.guardWrite(); err != nil {
			return fmt.Errorf("destination %s: %w", dstWhere, err)
		}
		path, err := src.Resolve(id)
		if err != nil {
			return err
		}
		bare := strings.TrimSuffix(filepath.Base(path), ".md")
		t, err := src.getStored(bare)
		if err != nil {
			return err
		}
		// References land on the ID the file stores, which is how the snapshot
		// keyed it — a file renamed to `renamed.md` while keeping `id: original`
		// is `original` to every ticket naming it, and checking the filename
		// would find no referrer. The close that records the move is written
		// under the stored ID too, and there is no file at that name to write:
		// the mismatch is refused before anything lands in the destination.
		qualified := FormatNamespacedID(src.Project, t.ID)
		if t.Type == TypeEpic {
			return fmt.Errorf("%s is an epic and cannot move: its children would be left naming a closed copy — no move performed", t.ID)
		}
		var related []string
		if t.Parent != "" {
			related = append(related, "parent "+t.Parent)
		}
		for _, d := range t.Deps {
			related = append(related, "dep "+d)
		}
		for _, l := range t.Links {
			related = append(related, "link "+l)
		}
		for _, in := range o.snap.Inbound(qualified) {
			related = append(related, fmt.Sprintf("%s of %s", in.Kind, in.From))
		}
		if len(related) > 0 {
			return fmt.Errorf("%s cannot move while it is related to other tickets (%s): a move copies it under a new ID and closes the original, which would leave those references pointing at a closed copy. Remove the relationships first — no move performed",
				t.ID, strings.Join(related, ", "))
		}
		if t.ID != bare {
			return fmt.Errorf("%s is stored as %s: a move closes the source under its stored ID, and no file holds that name. Rename the file to %s.md first — no move performed",
				bare, t.ID, t.ID)
		}
		if !o.snap.Complete {
			return o.incompleteError(fmt.Sprintf("moving %s", t.ID))
		}

		newID := GenerateIDFrom(t.Title, time.Now())
		for i := 0; i < 5; i++ {
			if _, err := os.Stat(filepath.Join(dst.Dir, newID+".md")); os.IsNotExist(err) {
				break
			}
			newID = GenerateIDFrom(t.Title, time.Now())
		}
		// The file is written under the bare half of the new ID — a project
		// store's files are named for it, and the namespace is what the
		// destination's readers put back — while the result and the notes
		// carry the destination's prefix, which is how every reader there
		// names it.
		newRef := qualifyForStore(dst, newID)
		now := time.Now().UTC()
		result := MoveResult{OldID: t.ID, NewID: newRef}

		copied := *t
		newTicket := &copied
		newTicket.ID = newID
		newTicket.Status = StatusBacklog
		newTicket.Tags = copyStrings(t.Tags)
		newTicket.Deps = []string{}
		newTicket.Links = []string{}
		newTicket.DepCargo = nil // the shallow copy aliases the source map
		newTicket.version = ""   // the copy is a new file, not the source's
		newTicket.relationshipIssue = ""
		// Same inherited read-state: the body was stripped at parse time, so the
		// new file never holds a Review Log and its write drops nothing. Only the
		// source's close warns.
		newTicket.droppedReviewLog = 0
		// The rows judged commits of the source project's repo, against the ID the
		// ticket had there, so they say nothing about the copy landing in this one
		// and do not travel with it. The closed source ticket keeps the record.
		newTicket.Verdicts = nil
		// Receipts are keyed to the ticket and the project they were recorded
		// in: an action ID names the source ticket, and a finding key is scoped
		// to the source project. Neither answers for the copy.
		newTicket.Actions = nil
		newTicket.Notes = append(copyNotes(t.Notes), Note{
			Timestamp: now,
			Text:      fmt.Sprintf("Moved from %s in %s", t.ID, srcWhere),
		})
		if err := o.create(dst, newTicket); err != nil {
			return fmt.Errorf("creating %s in target: %w", newRef, err)
		}

		// Closed, not done: the ticket did not complete here, it left.
		t.Notes = append(t.Notes, Note{
			Timestamp: now,
			Text:      fmt.Sprintf("Moved to %s in %s", newRef, dstWhere),
		})
		t.Status = StatusClosed
		if err := o.update(src, t); err != nil {
			return fmt.Errorf("recording the move of %s in source: %w. %s was written to %s but %s is "+
				"unchanged here — delete the target copy or record the move by hand",
				t.ID, err, newRef, dstWhere, t.ID)
		}

		// The move as a move, on both projects' logs. The stores logged the
		// create and the edit they each performed, which read as two unrelated
		// writes rather than as one ticket leaving a project for another.
		dst.logMutation(newTicket.ID, MutationMove, nil)
		src.logMutation(bare, MutationMove, nil)

		results = append(results, result)
		return nil
	})
	return results, err
}

// qualifyForStore returns a bare ID in the store's namespace, so a reference
// written into a project store resolves the way every other reference in it
// does. A store with no project never sees a namespaced ID and gets the bare
// form back — a shape no resolution produces now, kept because the type still
// permits it.
func qualifyForStore(store *FileStore, id string) string {
	if store.Project == "" {
		return id
	}
	return FormatNamespacedID(store.Project, id)
}

// storeLabel names a store for the provenance notes a move writes on both
// sides. A project store is named by its project: its directory is a path
// inside the central store, which says nothing about where the work went. A
// store with no project — no resolution produces one now — is named by the
// directory above it.
func storeLabel(store *FileStore) (string, error) {
	if store.Project != "" {
		return "project " + store.Project, nil
	}
	dir, err := filepath.Abs(store.Dir)
	if err != nil {
		return "", err
	}
	return filepath.Dir(dir), nil
}

// sameStoreDir reports whether two stores are the same directory. Directory
// identity, not the project name: a store with no project carries no name to
// compare, so comparing names would read two unrelated ones as a single store.
//
// Identity is the inode where both directories exist, because a canonicalized
// path string still misses spellings that name one directory: EvalSymlinks
// resolves links but does not fold case, and APFS is case-insensitive by
// default, so a repo-owned store reached as ../proj from /Users/me/code/Proj
// compares unequal to itself — as does one directory reached through a bind
// mount or a hard-linked parent. Comparing the canonicalized strings is the
// fallback for a directory that cannot be stat'd, which is the ordinary case
// for a registered central project that has never held a ticket: it has no
// directory until dst.Create makes one.
func sameStoreDir(a, b *FileStore) (bool, error) {
	dirA, err := storeDir(a)
	if err != nil {
		return false, err
	}
	dirB, err := storeDir(b)
	if err != nil {
		return false, err
	}
	fiA, errA := os.Stat(dirA)
	fiB, errB := os.Stat(dirB)
	if errA == nil && errB == nil {
		return os.SameFile(fiA, fiB), nil
	}
	return dirA == dirB, nil
}

// storeDir canonicalizes a store's directory for comparison. Symlinks are
// resolved because the same directory reached by two spellings is one store —
// /tmp and /var are symlinks on macOS — but only on a best effort basis:
// EvalSymlinks fails on a path that does not exist, and a registered central
// project that has never held a ticket legitimately has no directory yet.
// internal/project.canonicalPath applies the same string-level rule to match a
// repo against a registered project path, so a change to what counts as one
// path here has a counterpart there.
func storeDir(store *FileStore) (string, error) {
	abs, err := filepath.Abs(store.Dir)
	if err != nil {
		return "", err
	}
	if eval, err := filepath.EvalSymlinks(abs); err == nil {
		return eval, nil
	}
	return abs, nil
}

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	c := make([]string, len(s))
	copy(c, s)
	return c
}

func copyNotes(notes []Note) []Note {
	if notes == nil {
		return nil
	}
	c := make([]Note, len(notes))
	copy(c, notes)
	return c
}
