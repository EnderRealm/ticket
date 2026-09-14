package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:     "show <id> [id...]",
	Aliases: []string{"get"},
	Short:   "Display ticket details",
	Args:    cobra.MinimumNArgs(1),
	RunE:    runShow,
}

func init() {
	showCmd.Flags().Bool("metadata", false, "show only frontmatter fields and description")
	rootCmd.AddCommand(showCmd)
}

func runShow(cmd *cobra.Command, args []string) error {
	metadataOnly, _ := cmd.Flags().GetBool("metadata")
	store := TicketStore()
	for i, id := range args {
		if i > 0 {
			fmt.Println()
		}
		if err := showTicket(store, id, metadataOnly); err != nil {
			return err
		}
	}
	return nil
}

func showTicket(store *ticket.FileStore, id string, metadataOnly bool) error {
	t, err := store.Get(id)
	if err != nil {
		return err
	}

	if metadataOnly {
		// Serialize only frontmatter + title + description (no notes, relationships).
		meta := *t
		meta.Notes = nil
		data, err := ticket.Serialize(&meta)
		if err != nil {
			return err
		}
		fmt.Print(sanitizeRenderedDocument(string(data)))
		return nil
	}

	// Relationships come off the graph: every reference is resolved relative
	// to the ticket holding it, by exact qualified ID, so a foreign parent gets
	// its title and a done foreign dep is not a blocker — and an ambiguous or
	// foreign reference the graph cannot answer stays unknown rather than
	// being guessed.
	snap, err := store.Snapshot()
	if err != nil {
		return err
	}
	ns := store.Project
	qid := ticket.FormatNamespacedID(ns, t.ID)
	resolve := func(ref string) (*ticket.Ticket, bool) {
		return snap.Get(ticket.QualifyRef(ns, ref))
	}
	// The header comes off the same reading as everything under it: the
	// snapshot's copy carries the derived status and the stamped relationship
	// issue, so a child written between Get and the snapshot cannot make the
	// status disagree with the counts. The ID stays the bare one a project
	// store prints.
	if cur, ok := snap.Get(qid); ok {
		view := *cur
		view.ID = t.ID
		t = &view
	}

	// Serialize the base ticket content.
	data, err := ticket.Serialize(t)
	if err != nil {
		return err
	}

	// Render timestamps in local wall-clock time for human-facing output.
	output := localizeTimestamps(string(data), t)

	// Annotate parent line with title.
	if t.Parent != "" {
		if parent, ok := resolve(t.Parent); ok {
			output = strings.Replace(output,
				"parent: "+t.Parent,
				"parent: "+t.Parent+"  # "+parent.Title,
				1)
		}
	}

	fmt.Print(sanitizeRenderedDocument(output))

	// Outputs: what the ticket produced, for downstream handoff.
	if len(t.Outputs) > 0 {
		keys := make([]string, 0, len(t.Outputs))
		for k := range t.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Print("\n## Outputs\n\n")
		for _, k := range keys {
			fmt.Printf("- %s: %s\n", ticket.SanitizeControl(k), ticket.SanitizeControl(t.Outputs[k]))
		}
	}

	// Dep cargo: what flows across each annotated dependency edge.
	if len(t.DepCargo) > 0 {
		keys := make([]string, 0, len(t.DepCargo))
		for k := range t.DepCargo {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Print("\n## Dep Cargo\n\n")
		for _, k := range keys {
			fmt.Printf("- %s: %s\n", ticket.SanitizeControl(k), ticket.SanitizeControl(t.DepCargo[k]))
		}
	}

	// Blockers: deps not at done status, shown as the ticket spells them.
	var blockers []string
	for _, depID := range t.Deps {
		dep, ok := resolve(depID)
		if !ok || dep.Status != ticket.StatusDone {
			blockers = append(blockers, depID)
		}
	}
	if len(blockers) > 0 {
		fmt.Print("\n## Blockers\n\n")
		for _, id := range blockers {
			if dep, ok := resolve(id); ok {
				fmt.Printf("- %s [%s] %s\n", ticket.SanitizeControl(id), dep.Status, ticket.SanitizeControl(dep.Title))
			} else {
				fmt.Printf("- %s [unknown]\n", ticket.SanitizeControl(id))
			}
		}
	}

	// Blocking: tickets in any namespace that depend on this one and aren't
	// done. Each dep is read relative to the ticket holding it, so a dep on
	// this ID from another namespace counts and a same-suffix dep on another
	// project's ticket does not.
	var blocking []*ticket.Ticket
	for _, tk := range snap.Tickets {
		if tk.Status == ticket.StatusDone {
			continue
		}
		tns, _ := ticket.ParseNamespacedID(tk.ID)
		for _, depID := range tk.Deps {
			if ticket.QualifyRef(tns, depID) == qid {
				blocking = append(blocking, tk)
				break
			}
		}
	}
	if len(blocking) > 0 {
		fmt.Print("\n## Blocking\n\n")
		for _, tk := range blocking {
			fmt.Printf("- %s [%s] %s\n", ticket.SanitizeControl(tk.ID), tk.Status, ticket.SanitizeControl(tk.Title))
		}
	}

	// Children: the tickets the graph places under this epic, from every
	// namespace, under their qualified IDs.
	if children := snap.Children(qid); len(children) > 0 {
		fmt.Print("\n## Children\n\n")
		for _, tk := range children {
			fmt.Printf("- %s [%s] %s\n", ticket.SanitizeControl(tk.ID), tk.Status, ticket.SanitizeControl(tk.Title))
		}
	}

	// Links, shown as the ticket spells them.
	if len(t.Links) > 0 {
		fmt.Print("\n## Linked\n\n")
		for _, id := range t.Links {
			if tk, ok := resolve(id); ok {
				fmt.Printf("- %s [%s] %s\n", ticket.SanitizeControl(id), tk.Status, ticket.SanitizeControl(tk.Title))
			} else {
				fmt.Printf("- %s [unknown]\n", ticket.SanitizeControl(id))
			}
		}
	}

	// Progress: the counts the epic's status was derived from, off the same
	// snapshot, and whether that snapshot saw the whole store.
	if t.Type == ticket.TypeEpic {
		p := snap.Progress(qid)
		fmt.Print("\n## Progress\n\n")
		fmt.Printf("children: %d — done %d, closed %d, open %d, ready %d, backlog %d\n", p.Total, p.Done, p.Closed, p.Open, p.Ready, p.Backlog)
		if p.Complete {
			fmt.Println("complete: yes")
		} else {
			fmt.Println("complete: no")
			for _, d := range p.Diagnostics {
				fmt.Printf("  %s\n", ticket.SanitizeControl(d))
			}
		}
	} else if issue := ticket.RelationshipIssue(t); issue != "" {
		// Why this leaf is no epic's child and never automatically runnable.
		fmt.Print("\n## Relationship\n\n")
		fmt.Println(ticket.SanitizeControl(issue))
	}

	return nil
}

// sanitizeRenderedDocument covers the whole stored document, not only titles:
// body text is equally untrusted. It preserves renderer-owned line breaks and
// leaves the Ticket untouched so JSON paths still carry the stored bytes.
func sanitizeRenderedDocument(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i := range lines {
		lines[i] = ticket.SanitizeControl(lines[i])
	}
	return strings.Join(lines, "\n")
}

// localizeTimestamps rewrites the created/updated/completed frontmatter values
// in serialized ticket output from UTC (as written by Serialize) to local
// wall-clock time with no zone offset, for human-facing CLI display. The
// on-disk file is unaffected.
func localizeTimestamps(serialized string, t *ticket.Ticket) string {
	const localLayout = "2006-01-02T15:04:05"
	fields := []struct {
		key string
		ts  time.Time
	}{
		{"created", t.Created},
		{"updated", t.Updated},
		{"completed", t.Completed},
	}
	for _, f := range fields {
		if f.ts.IsZero() {
			continue
		}
		serialized = strings.Replace(serialized,
			f.key+": "+f.ts.UTC().Format(time.RFC3339),
			f.key+": "+f.ts.Local().Format(localLayout),
			1)
	}
	return serialized
}
