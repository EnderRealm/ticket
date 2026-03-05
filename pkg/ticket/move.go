package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MoveResult describes a single ticket move operation.
type MoveResult struct {
	OldID       string
	NewID       string
	StrippedDeps  []string
	StrippedLinks []string
}

// MoveTicket moves a single ticket from src store to dst store.
// The ticket is closed in src with a note, and created in dst with a new ID.
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

	// Build old ID → new ID mapping.
	idMap := map[string]string{}
	movingSet := map[string]bool{}
	for _, t := range toMove {
		movingSet[t.ID] = true
	}
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
		idMap[t.ID] = newID
	}

	now := time.Now().UTC()
	var results []MoveResult

	for _, t := range toMove {
		newID := idMap[t.ID]
		result := MoveResult{OldID: t.ID, NewID: newID}

		// Shallow copy all fields, then override what needs to change.
		copied := *t
		newTicket := &copied
		newTicket.ID = newID
		newTicket.Status = StatusOpen
		newTicket.Stage = StageTriage
		newTicket.Review = ReviewNone
		newTicket.Tags = copyStrings(t.Tags)
		newTicket.Deps = nil
		newTicket.Links = nil
		newTicket.Notes = copyNotes(t.Notes)
		newTicket.Reviews = copyReviews(t.Reviews)
		newTicket.Skipped = nil
		newTicket.Conversations = copyStrings(t.Conversations)
		newTicket.Parent = ""

		// Remap or strip parent.
		if t.Parent != "" {
			if newParent, ok := idMap[t.Parent]; ok {
				newTicket.Parent = newParent
			}
			// If parent isn't moving, drop it — ticket is moving to new repo.
		}

		// Remap or strip deps.
		for _, d := range t.Deps {
			if newDep, ok := idMap[d]; ok {
				newTicket.Deps = append(newTicket.Deps, newDep)
			} else {
				result.StrippedDeps = append(result.StrippedDeps, d)
			}
		}
		if newTicket.Deps == nil {
			newTicket.Deps = []string{}
		}

		// Remap or strip links.
		for _, l := range t.Links {
			if newLink, ok := idMap[l]; ok {
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
			return nil, fmt.Errorf("creating %s in target: %w", newID, err)
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
		t.Status = StatusClosed
		t.Stage = StageDone
		if err := src.Update(t); err != nil {
			return nil, fmt.Errorf("closing %s in source: %w", t.ID, err)
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

	// Build parent → children index.
	childMap := map[string][]*Ticket{}
	for _, t := range all {
		if t.Parent != "" {
			childMap[t.Parent] = append(childMap[t.Parent], t)
		}
	}

	// BFS from parentID.
	var result []*Ticket
	queue := []string{parentID}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range childMap[pid] {
			result = append(result, child)
			queue = append(queue, child.ID)
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

func copyReviews(reviews []ReviewRecord) []ReviewRecord {
	if reviews == nil {
		return nil
	}
	c := make([]ReviewRecord, len(reviews))
	copy(c, reviews)
	return c
}

