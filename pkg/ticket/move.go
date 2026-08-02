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
// The move is not atomic and nothing is rolled back on failure: the results
// for the tickets that completed (created in dst and closed in src) are
// returned alongside the error, and the error names any ticket already
// written to dst whose source copy is still open, so it can be reconciled.
func MoveTicket(src, dst *FileStore, id string, recursive bool) ([]MoveResult, error) {
	srcDir, err := filepath.Abs(src.Dir)
	if err != nil {
		return nil, err
	}
	dstDir, err := filepath.Abs(dst.Dir)
	if err != nil {
		return nil, err
	}
	srcRepo := filepath.Dir(srcDir)
	dstRepo := filepath.Dir(dstDir)

	// Validate target .tickets/ dir exists.
	if _, err := os.Stat(dst.Dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("target tickets directory %s does not exist", dst.Dir)
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
		idMap[bare] = newID
	}

	now := time.Now().UTC()
	var results []MoveResult

	for _, t := range toMove {
		_, bare := ParseNamespacedID(t.ID)
		newID := idMap[bare]
		result := MoveResult{OldID: t.ID, NewID: newID}

		// Shallow copy all fields, then override what needs to change.
		copied := *t
		newTicket := &copied
		newTicket.ID = newID
		newTicket.Status = StatusBacklog
		newTicket.Tags = copyStrings(t.Tags)
		newTicket.Deps = nil
		newTicket.Links = nil
		newTicket.DepCargo = nil // the shallow copy aliases the source map
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
			Text:      fmt.Sprintf("Moved from %s in %s", t.ID, srcRepo),
		})

		// Create in target.
		if err := dst.Create(newTicket); err != nil {
			return results, fmt.Errorf("creating %s in target: %w", newID, err)
		}

		// Close original with note.
		closeNote := fmt.Sprintf("Moved to %s in %s", newID, dstRepo)
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
		// Closed, not done: the ticket did not complete here, it left. Done
		// would also assert an epic finished with its children still open,
		// which the state guard rejects, and would roll a parent epic staying
		// behind up to done when its last non-terminal child moves away.
		t.Status = StatusClosed
		if err := src.Update(t); err != nil {
			return results, fmt.Errorf("closing %s in source: %w. %s was written to %s but %s is still "+
				"open here — delete the target copy or close it by hand",
				t.ID, err, newID, dstRepo, t.ID)
		}

		results = append(results, result)
	}

	return results, nil
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
	// ticket in this project and move the wrong one. A local .tickets/ store
	// carries no project, so every namespaced parent is foreign to it.
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
