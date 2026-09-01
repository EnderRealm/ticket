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
	Short: "Report invalid parents, epics whose stored status is no longer read, tickets missing body content, files that cannot be read as tickets, and files whose id names another project",
	Long: "Report tickets whose parent breaks the one-level epic hierarchy: a parent that is not an epic, " +
		"a parent that does not resolve, a parent in another project, an epic that has a parent, or a parent cycle. " +
		"Each parent is resolved within the project that owns the ticket, so the report matches what a write would accept. " +
		"Also report every epic whose stored status differs from the status it now derives from its children, since " +
		"stored statuses were left in place and are no longer read, and every ticket whose stored body is missing content: " +
		"a section ending in a tool-call envelope fragment, or a description with no acceptance criteria. " +
		"Also report every file that could not be read as a ticket at all, which exits non-zero: it is a ticket no listing yields, and " +
		"it could be any epic's child, so no epic in its project reads done or closed while it stands. " +
		"Also report every file whose stored id names a project other than the directory holding it: the directory decides a ticket's " +
		"project, so such a file is read as no project's ticket and appears in no listing until it is moved or its id is fixed. " +
		"Read-only — nothing is rewritten.",
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
		var content []ticket.ContentIssue
		for _, c := range audit.Content {
			if p, _ := ticket.ParseNamespacedID(c.ID); p == proj {
				content = append(content, c)
			}
		}
		audit.Content = content
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

	// Skipped files split by whether they left the store read in part, through
	// the same predicate the derivation degrades on — keyed off the kind's own
	// answer rather than a second test here, so the report and the epic statuses
	// it prints cannot disagree about which files made it partial. Only an
	// unreadable file does; a file naming another project was read in full and
	// is reported below because no listing in the project holding it shows it.
	var unreadable, foreign []ticket.FileSkip
	for _, f := range audit.SkippedFiles {
		if f.Kind.DegradesEpicStatus() {
			unreadable = append(unreadable, f)
			continue
		}
		foreign = append(foreign, f)
	}

	if jsonOutput {
		if audit.Violations == nil {
			audit.Violations = []ticket.ParentViolation{}
		}
		if audit.EpicStatus == nil {
			audit.EpicStatus = []ticket.EpicStatusDrift{}
		}
		if audit.Content == nil {
			audit.Content = []ticket.ContentIssue{}
		}
		data, err := json.MarshalIndent(audit, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		// The count line below would not be JSON, so the exit code is all this
		// mode carries — the files themselves are already in skipped_files.
		return unreadableExit(cmd, unreadable)
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
	printContentIssues(audit.Content)
	// A project or a file the audit could not read is named rather than dropped:
	// without it, "No parent violations." would speak for a store never fully
	// read. It covers every section, all of which are read in the same
	// per-project pass.
	if len(audit.Skipped) > 0 || len(unreadable) > 0 {
		// Only the half that has something to report is named: a run that skipped
		// one project and no file has nothing to say about files.
		var what []string
		if len(audit.Skipped) > 0 {
			what = append(what, fmt.Sprintf("%d project(s)", len(audit.Skipped)))
		}
		if len(unreadable) > 0 {
			what = append(what, fmt.Sprintf("%d file(s)", len(unreadable)))
		}
		fmt.Printf("\nwarning: %s could not be read, so this report is incomplete:\n", strings.Join(what, " and "))
		// Every line in this block is an entry, and the project name, the
		// filename and the reason all came off a store another machine writes
		// into — the reason is flattened to one line where the skip is built, and
		// the other two are quoted here so control bytes in them cannot reach the
		// terminal raw. A project name is a directory name in the synced store,
		// bounded against path separators and nothing else, so it needs the quote
		// as much as the filename does.
		for _, s := range audit.Skipped {
			fmt.Printf("  project %q: %s\n", s.Project, s.Error)
		}
		for _, f := range unreadable {
			fmt.Printf("  project %q, file %q: %s\n", f.Project, f.File, f.Error)
		}
		if len(unreadable) > 0 {
			// An unreadable file is a ticket nothing can place, so no epic in its
			// project can claim every child finished. Said here because the epic
			// section above reports the degraded values without explaining them.
			fmt.Println("An unreadable file could be any epic's child, so no epic in its project reads done or closed until the file is fixed or removed.")
			// Counted in the shape the other findings are counted in: it is a
			// violation class of its own, not a caveat on the sections above.
			fmt.Printf("\n%d file(s) could not be read as tickets — repair or remove each file\n", len(unreadable))
		}
	}
	if len(foreign) > 0 {
		// The directory decides a ticket's project, so a file disagreeing with
		// the one holding it is read as nobody's ticket: it answers no reference
		// there and appears in no listing. Reported here because that is the only
		// place it is visible at all — and stated as a repair, since the ticket
		// itself is intact.
		fmt.Printf("\nwarning: %d file(s) store an id naming another project, so they are not read as tickets where they sit:\n", len(foreign))
		// Project and filename quoted for the reason the block above states.
		for _, f := range foreign {
			fmt.Printf("  project %q, file %q: %s\n", f.Project, f.File, f.Error)
		}
		fmt.Println("The audit read each of these in full, so the report above is complete without them. Move the file to the project its id names, or fix the id field — until then it is in no listing, resolves for no reference, and counts as no epic's child, so an epic that was counting it can now read done.")
	}
	return unreadableExit(cmd, unreadable)
}

// unreadableExit is the error a run that found unreadable files ends with, so
// the command exits non-zero. A file no listing yields is a finding, and a
// scripted caller reads the exit code rather than the report — a zero there
// says the store is clean, which is the one thing an unreadable file rules out.
// Only this class exits non-zero: a file naming another project, and every
// other section, keep the exit code they had. Usage is silenced because the
// command ran correctly; what is wrong is the store.
func unreadableExit(cmd *cobra.Command, unreadable []ticket.FileSkip) error {
	if len(unreadable) == 0 {
		return nil
	}
	cmd.SilenceUsage = true
	return fmt.Errorf("audit: %d unreadable ticket file(s)", len(unreadable))
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

// contentEmptyListLimit bounds how many empty-acceptance tickets are named
// before the rest are summarised as a count.
const contentEmptyListLimit = 10

// printContentIssues reports the tickets whose stored body is missing content
// it was meant to carry. Both classes are silent everywhere else: the ticket
// lists and renders, and only reading its text shows that the contract is not
// there.
func printContentIssues(issues []ticket.ContentIssue) {
	if len(issues) == 0 {
		fmt.Println("\nNo ticket is missing body content.")
		return
	}
	fmt.Println()
	fragments, empty := 0, 0
	// An ID carries its project namespace, and a project name is a store
	// directory name or a shared-config key another machine wrote — bounded
	// against path separators and nothing else — so it goes through the same
	// rule every other untrusted string tk prints to an operator does. The
	// parent-violation and epic-drift loops above still print theirs raw; they
	// are left as they are on purpose, for a separate change.
	for _, c := range issues {
		if c.Kind == ticket.ContentEnvelopeFragment {
			fragments++
			// The field is ours; the tail came off the store, so it is quoted
			// the way storedStatus quotes an unrecognised status.
			fmt.Printf("%s  %s  %s: %q\n", ticket.SanitizeControl(c.ID), c.Kind, c.Field, c.Detail)
			continue
		}
		empty++
		// Every fragment is listed — they are rare and each names a ticket to
		// repair — but a description with no criteria is the ordinary state of a
		// backlog stub, so listing them all would bury the sections above on a
		// healthy store. The count below is what the report is actually for.
		if empty <= contentEmptyListLimit {
			fmt.Printf("%s  %s\n", ticket.SanitizeControl(c.ID), c.Kind)
		}
	}
	if empty > contentEmptyListLimit {
		fmt.Printf("... and %d more with a description and no acceptance criteria\n", empty-contentEmptyListLimit)
	}
	fmt.Println()
	if fragments > 0 {
		fmt.Printf("%d section(s) absorbed part of the tool call that wrote them — the text that followed was never stored, so the real content is likely missing; rewrite each from the source\n", fragments)
	}
	if empty > 0 {
		fmt.Printf("%d ticket(s) carry a description with no acceptance criteria — nothing states what done means. "+
			"The count is a census and includes finished tickets and backlog stubs; the open and ready ones are the actionable half, since /capture and /work both gate on that contract\n", empty)
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
