package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MoveResult describes a single ticket move operation.
type MoveResult struct {
	OldID         string
	NewID         string
	StrippedDeps  []string
	StrippedLinks []string
}

// MoveTicket moves a single ticket from src store to dst store.
// The ticket is closed in src with a note, and created in dst with a new ID.
//
// Both stores are resolved by the caller (ResolveStoreForRepo), which is where
// a destination that resolves to no store is refused: nothing here checks that
// dst.Dir exists, because a registered central project that has never held a
// ticket legitimately has no directory yet and dst.Create makes it. A dst that
// is the same directory as src is refused here, before anything is written.
//
// The move is not atomic and nothing is rolled back on failure: the results
// for the tickets that completed (created in dst and closed in src) are
// returned alongside the error, and the error names any ticket already
// written to dst whose source copy is still open, so it can be reconciled.
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
	// ticket — new ID, old one closed — for no gain, and the move is not atomic,
	// so a recursive run has to be refused as a whole rather than partway.
	same, err := sameStoreDir(src, dst)
	if err != nil {
		return nil, err
	}
	if same {
		// Says only what the guard checked. It runs ahead of src.Get, so the
		// requested ID has not been looked up yet and naming it as living here
		// would be an unverified claim on a typo'd ID.
		return nil, fmt.Errorf("destination resolves to %s, the source store — no move performed", srcWhere)
	}

	root, err := src.Get(id)
	if err != nil {
		return nil, err
	}

	// Collect tickets to move.
	var toMove []*Ticket
	toMove = append(toMove, root)

	if recursive {
		children, err := collectDescendants(src, root.ID)
		if err != nil {
			return nil, err
		}
		toMove = append(toMove, children...)
	}

	// Build old ID → new ID mapping. Keyed on the bare ID, and every lookup
	// below normalizes the same way: a map key can't tolerate the namespace
	// mismatch the way SameTicketID does, and a moving ticket can name a
	// moving parent, dep, or link namespaced. Bare IDs are unique within one
	// store directory, so the bare form is an unambiguous key.
	//
	// The mapped value is the new ID in the destination's namespace: a central
	// project's tickets reference each other namespaced, so a remapped parent,
	// dep or link has to carry the destination's prefix rather than the one it
	// arrived with. The file itself is written under the bare half.
	idMap := map[string]string{}
	for _, t := range toMove {
		newID := GenerateIDFrom(t.Title, time.Now())
		// Ensure no collision in target.
		for i := 0; i < 5; i++ {
			path := filepath.Join(dst.Dir, newID+".md")
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
			newID = GenerateIDFrom(t.Title, time.Now())
		}
		_, bare := ParseNamespacedID(t.ID)
		idMap[bare] = qualifyForStore(dst, newID)
	}

	now := time.Now().UTC()
	var results []MoveResult

	for _, t := range toMove {
		_, bare := ParseNamespacedID(t.ID)
		newID := idMap[bare]
		result := MoveResult{OldID: t.ID, NewID: newID}

		// Shallow copy all fields, then override what needs to change. The file
		// is written under the bare half of the new ID — a project store's files
		// are named for it, and the namespace is what the destination's readers
		// put back — while every reference to it carries the destination's prefix.
		_, bareNew := ParseNamespacedID(newID)
		copied := *t
		newTicket := &copied
		newTicket.ID = bareNew
		newTicket.Status = StatusBacklog
		// The copy starts over, so an abandoned epic does not arrive abandoned:
		// the flag is the record of a decision taken about the work that stayed
		// behind, not about the work that landed here.
		newTicket.Abandoned = false
		newTicket.Tags = copyStrings(t.Tags)
		newTicket.Deps = nil
		newTicket.Links = nil
		newTicket.DepCargo = nil // the shallow copy aliases the source map
		newTicket.version = ""   // the copy is a new file, not the source's
		// The rows judged commits of the source project's repo, against the ID the
		// ticket had there, so they say nothing about the copy landing in this one
		// and do not travel with it. The closed source ticket keeps the record.
		newTicket.Verdicts = nil
		newTicket.Notes = copyNotes(t.Notes)
		newTicket.Parent = ""

		// Remap or strip parent.
		if t.Parent != "" {
			_, bareParent := ParseNamespacedID(t.Parent)
			if newParent, ok := idMap[bareParent]; ok {
				newTicket.Parent = newParent
			}
			// If parent isn't moving, drop it — ticket is moving to new repo.
		}

		// Remap or strip deps. Cargo follows its dep under the new ID; a
		// stripped dep takes its cargo with it.
		for _, d := range t.Deps {
			_, bareDep := ParseNamespacedID(d)
			if newDep, ok := idMap[bareDep]; ok {
				newTicket.Deps = append(newTicket.Deps, newDep)
				if cargo := CargoFor(t, d); cargo != "" {
					if newTicket.DepCargo == nil {
						newTicket.DepCargo = map[string]string{}
					}
					newTicket.DepCargo[newDep] = cargo
				}
			} else {
				result.StrippedDeps = append(result.StrippedDeps, d)
			}
		}
		if newTicket.Deps == nil {
			newTicket.Deps = []string{}
		}

		// Remap or strip links.
		for _, l := range t.Links {
			_, bareLink := ParseNamespacedID(l)
			if newLink, ok := idMap[bareLink]; ok {
				newTicket.Links = append(newTicket.Links, newLink)
			} else {
				result.StrippedLinks = append(result.StrippedLinks, l)
			}
		}
		if newTicket.Links == nil {
			newTicket.Links = []string{}
		}

		// Add provenance note to target ticket.
		newTicket.Notes = append(newTicket.Notes, Note{
			Timestamp: now,
			Text:      fmt.Sprintf("Moved from %s in %s", t.ID, srcWhere),
		})

		// Create in target.
		if err := dst.Create(newTicket); err != nil {
			return results, fmt.Errorf("creating %s in target: %w", newID, err)
		}

		// Close original with note.
		closeNote := fmt.Sprintf("Moved to %s in %s", newID, dstWhere)
		if len(result.StrippedDeps) > 0 {
			closeNote += fmt.Sprintf(". Stripped deps: %v", result.StrippedDeps)
		}
		if len(result.StrippedLinks) > 0 {
			closeNote += fmt.Sprintf(". Stripped links: %v", result.StrippedLinks)
		}
		t.Notes = append(t.Notes, Note{
			Timestamp: now,
			Text:      closeNote,
		})
		// Closed, not done: the ticket did not complete here, it left. It is an
		// epic's children that carry this — an epic derives done only once every
		// child of it is done, so a child that moved away leaves the epic
		// staying behind reading closed rather than finished. On an epic that is
		// itself moving the status written here is inert: an epic's status is
		// derived, and the write goes through Update, which records no abandon
		// intent, so what stays behind reads as whatever children stayed with it.
		t.Status = StatusClosed
		if err := src.Update(t); err != nil {
			return results, fmt.Errorf("closing %s in source: %w. %s was written to %s but %s is still "+
				"open here — delete the target copy or close it by hand",
				t.ID, err, newID, dstWhere, t.ID)
		}

		// The move as a move, on both projects' logs. The stores logged the
		// create and the edit they each performed, which read as two unrelated
		// writes rather than as one ticket leaving a project for another.
		dst.logMutation(bareNew, MutationMove, nil)
		src.logMutation(bare, MutationMove, nil)

		results = append(results, result)
	}

	return results, nil
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

// collectDescendants returns all descendants (children, grandchildren, etc.)
// of the given ticket ID.
func collectDescendants(store *FileStore, parentID string) ([]*Ticket, error) {
	all, err := store.List()
	if err != nil {
		return nil, err
	}

	// Build parent → children index. A map key can't tolerate the namespace
	// mismatch the way SameTicketID does, so every ID entering the walk — the
	// keys, the seed, and the queue — is normalized to its bare form: the
	// central store records children with a namespaced parent, while tickets
	// written before the namespacing rollout record it bare. A parent naming a
	// different project is skipped, not stripped, on the same grounds as
	// FileStore.Resolve: stripping it would index the child under a same-suffix
	// ticket in this project and move the wrong one. A store with no project
	// carries no namespace, so every namespaced parent is foreign to it.
	childMap := map[string][]*Ticket{}
	for _, t := range all {
		if t.Parent == "" {
			continue
		}
		project, parent := ParseNamespacedID(t.Parent)
		if project != "" && project != store.Project {
			continue
		}
		childMap[parent] = append(childMap[parent], t)
	}

	// BFS from parentID. The seed needs no project check — it is the root
	// ticket's own ID, read from this store's files after Resolve rejected any
	// foreign prefix. seen bounds the walk: a parent cycle would otherwise
	// never terminate, and a ticket reachable by two paths would move twice.
	_, seed := ParseNamespacedID(parentID)
	var result []*Ticket
	queue := []string{seed}
	seen := map[string]bool{seed: true}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range childMap[pid] {
			_, childID := ParseNamespacedID(child.ID)
			if seen[childID] {
				continue
			}
			seen[childID] = true
			result = append(result, child)
			queue = append(queue, childID)
		}
	}

	return result, nil
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
