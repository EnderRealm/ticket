package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Report tickets whose parent breaks the one-level epic hierarchy, and epics whose stored status is no longer read",
	Long: "Report tickets whose parent breaks the one-level epic hierarchy: a parent that is not an epic, " +
		"a parent that does not resolve, a parent in another project, an epic that has a parent, or a parent cycle. " +
		"Each parent is resolved within the project that owns the ticket, so the report matches what a write would accept. " +
		"Also report every epic whose stored status differs from the status it now derives from its children, since " +
		"stored statuses were left in place and are no longer read. Read-only — nothing is rewritten.",
	Args: cobra.NoArgs,
	RunE: runAudit,
}

func init() {
	auditCmd.Flags().String("project", "", "limit to a single project")
	rootCmd.AddCommand(auditCmd)
}

func runAudit(cmd *cobra.Command, args []string) error {
	root, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("audit requires a configured central store: %w", err)
	}
	store := ticket.NewMultiStore(filepath.Join(root, "tickets"))

	audit, err := ticket.Audit(store)
	if err != nil {
		return err
	}

	if proj, _ := cmd.Flags().GetString("project"); proj != "" {
		cfg, err := project.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if _, ok := cfg.Projects[proj]; !ok {
			return fmt.Errorf("project %q not found in config", proj)
		}
		var filtered []ticket.ParentViolation
		for _, v := range audit.Violations {
			if p, _ := ticket.ParseNamespacedID(v.ID); p == proj {
				filtered = append(filtered, v)
			}
		}
		audit.Violations = filtered
		var drift []ticket.EpicStatusDrift
		for _, d := range audit.EpicStatus {
			if p, _ := ticket.ParseNamespacedID(d.ID); p == proj {
				drift = append(drift, d)
			}
		}
		audit.EpicStatus = drift
		var skipped []ticket.ProjectSkip
		for _, s := range audit.Skipped {
			if s.Project == proj {
				skipped = append(skipped, s)
			}
		}
		audit.Skipped = skipped
		var skippedFiles []ticket.FileSkip
		for _, f := range audit.SkippedFiles {
			if f.Project == proj {
				skippedFiles = append(skippedFiles, f)
			}
		}
		audit.SkippedFiles = skippedFiles
	}

	if jsonOutput {
		if audit.Violations == nil {
			audit.Violations = []ticket.ParentViolation{}
		}
		if audit.EpicStatus == nil {
			audit.EpicStatus = []ticket.EpicStatusDrift{}
		}
		data, err := json.MarshalIndent(audit, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(audit.Violations) == 0 {
		fmt.Println("No parent violations.")
	} else {
		for _, v := range audit.Violations {
			fmt.Printf("%s  %s  parent: %s  (%s)\n", v.ID, v.Kind, v.Parent, v.Detail)
		}
		fmt.Printf("\n%d ticket(s) violate the one-level epic hierarchy — clear or repoint each parent\n", len(audit.Violations))
	}
	printEpicStatusDrift(audit.EpicStatus)
	// A project or a file the audit could not read is named rather than dropped:
	// without it, "No parent violations." would speak for a store never fully
	// read. It covers both sections, which are read in the same per-project pass.
	if len(audit.Skipped) > 0 || len(audit.SkippedFiles) > 0 {
		// Only the half that has something to report is named: a run that skipped
		// one project and no file has nothing to say about files.
		var what []string
		if len(audit.Skipped) > 0 {
			what = append(what, fmt.Sprintf("%d project(s)", len(audit.Skipped)))
		}
		if len(audit.SkippedFiles) > 0 {
			what = append(what, fmt.Sprintf("%d file(s)", len(audit.SkippedFiles)))
		}
		fmt.Printf("\nwarning: %s could not be read, so this report is incomplete:\n", strings.Join(what, " and "))
		// Every line in this block is an entry, and both the filename and the
		// reason came off a store another machine writes into — the reason is
		// flattened to one line where the skip is built, and the filename is
		// quoted here so control bytes in it cannot reach the terminal raw.
		for _, s := range audit.Skipped {
			fmt.Printf("  project %s: %s\n", s.Project, s.Error)
		}
		for _, f := range audit.SkippedFiles {
			fmt.Printf("  project %s, file %q: %s\n", f.Project, f.File, f.Error)
		}
		if len(audit.SkippedFiles) > 0 {
			// An unreadable file is a ticket nothing can place, so no epic in its
			// project can claim every child finished. Said here because the epic
			// section above reports the degraded values without explaining them.
			fmt.Println("An unreadable file could be any epic's child, so no epic in its project reads done or closed until the file is fixed or removed.")
		}
	}
	return nil
}

// printEpicStatusDrift reports the epics reading a different status than their
// file stores. Statuses were derived without migrating what was stored, so this
// is the only place the two can still be compared — an operator has no other
// way to find the epics whose displayed status moved.
func printEpicStatusDrift(drift []ticket.EpicStatusDrift) {
	if len(drift) == 0 {
		fmt.Println("\nNo epic reads a different status than its file stores.")
		return
	}
	fmt.Println()
	storedClosed := 0
	for _, d := range drift {
		if d.Kind == ticket.EpicDriftStoredClosed {
			storedClosed++
		}
		fmt.Printf("%s  %s  stored: %s  reads: %s\n", d.ID, d.Kind, storedStatus(d.Stored), d.Derived)
	}
	fmt.Printf("\n%d epic(s) read the status their children imply rather than the one stored — the stored value is ignored, not migrated\n", len(drift))
	// Neither class is bounded to files written before statuses were derived:
	// every write of an epic bakes the status it derived at that moment into the
	// file, so an ordinary edit today produces both. Said plainly, because the
	// remedy below is only a remedy for the older ones.
	fmt.Println("Both classes also arise from ordinary edits made since: any write of an epic stores the status it derived at that moment, which the next change to a child makes stale.")
	if storedClosed > 0 {
		fmt.Printf("%d epic(s) of those store `closed` with no abandon flag: either a hand-close from before statuses were derived, or a write that carried a derived `closed` into the file — the file cannot say which. "+
			"A stored value is evidence of a decision only on a file older than derived statuses; run `tk edit <id> --status closed` on each of those that should stay abandoned, and do it before editing the epic, since the next write of the epic replaces the stored value with the derived one. "+
			"On a file written since, the stored `closed` is an artifact — closing the epic would close a child nobody asked to close\n", storedClosed)
	}
}

// storedStatus renders a status read straight off a ticket file. A status is
// checked only when tk writes one, so a file another machine pushed into the
// central store can carry anything at all — an unrecognised value is quoted
// rather than printed into the terminal as it stands.
func storedStatus(s ticket.Status) string {
	if ticket.ValidateStatus(s) != nil {
		return fmt.Sprintf("%q", string(s))
	}
	return string(s)
}
