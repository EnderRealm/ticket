package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit [cause]",
	Short: "Summarise the store's findings by cause — invalid parents, epics whose stored status is no longer read, tickets missing body content, tickets whose acceptance criteria nothing can check, tickets storing a legacy Review Log, files that cannot be read as tickets, and files whose id names another project — and list one cause's tickets with `tk audit <cause>`",
	Long: "With no argument, report one line per cause: how many findings it has, and the command that lists them. " +
		"The per-ticket listings and the overview are two different outputs, and printing both left the report unreadable — " +
		"one class alone runs to hundreds of lines on a real store and pushes the classes with the fewest findings, and the most to act on, off the top of the terminal. " +
		"`tk audit <cause>` prints that one cause's listing in full, uncapped, and nothing else. " +
		"`--json` is unaffected by the argument and returns the whole report in either form.\n\n" +
		"The causes: tickets whose parent breaks the one-level epic hierarchy — a parent that is not an epic, " +
		"a parent that does not resolve, a parent in another project, an epic that has a parent, or a parent cycle. " +
		"Each parent is resolved within the project that owns the ticket, so the report matches what a write would accept. " +
		"Every epic whose stored status differs from the status it now derives from its children, since " +
		"stored statuses were left in place and are no longer read, and every ticket whose stored body is missing content: " +
		"a section ending in a tool-call envelope fragment, or a description with no acceptance criteria. " +
		"Every ticket carrying acceptance criteria that have neither a `verify:` command nor an `unverifiable:` reason, " +
		"with how many of its criteria are bare: nothing decides whether they are met, and the warning `tk create` prints only reaches tickets not yet written. " +
		"Every ticket whose file still stores a legacy `## Review Log` section, which nothing has read since v7 retired the review system: " +
		"the section is stripped from the body on read and the ticket's next write drops it from the file, so this is the list of what is still there. " +
		"Every file that could not be read as a ticket at all, which exits non-zero: it is a ticket no listing yields, and " +
		"it could be any epic's child, so no epic in its project reads done or closed while it stands. " +
		"And every file whose stored id names a project other than the directory holding it: the directory decides a ticket's " +
		"project, so such a file is read as no project's ticket and appears in no listing until it is moved or its id is fixed. " +
		"Read-only — nothing is rewritten.",
	Args: cobra.MaximumNArgs(1),
	RunE: runAudit,
}

func init() {
	auditCmd.Flags().String("project", "", "limit to a single project")
	rootCmd.AddCommand(auditCmd)
}

// auditCause is one class of finding: how many of them the report holds, and
// how they are listed. The name is the kind constant the report has always
// printed on each line, so the vocabulary the argument takes is not new — a
// reader of yesterday's output could already type it.
//
// The summary and the drill-in are both derived from this one list, so the set
// of names printed and the set accepted as an argument cannot drift apart. A
// kind added to pkg/ticket has to be registered here to reach the text report;
// until it is, `--json` carries it, which the argument never filters.
type auditCause struct {
	name string
	// count over the already --project-filtered report — what the summary line
	// carries.
	count func(*ticket.AuditReport) int
	// print the cause's own listing and the prose that says what to do about
	// it. runAudit answers the zero case before this is reached, but no printer
	// depends on that — each takes its class as an argument rather than reading
	// it off the first finding.
	print func(*ticket.AuditReport)
}

// auditCauses is the report in the order it was printed in when it was one
// output: the parent violations first, which predate every other class, then
// the epic drift, the content findings, and the files that yielded no ticket.
var auditCauses = []auditCause{
	parentCause(ticket.ViolationEpicHasParent),
	parentCause(ticket.ViolationParentMissing),
	parentCause(ticket.ViolationParentCycle),
	parentCause(ticket.ViolationParentNotEpic),
	parentCause(ticket.ViolationParentCrossProject),
	driftCause(ticket.EpicDriftStoredClosed),
	driftCause(ticket.EpicDriftStale),
	contentCause(ticket.ContentEnvelopeFragment),
	contentCause(ticket.ContentEmptyAcceptance),
	contentCause(ticket.ContentBareAcceptance),
	contentCause(ticket.ContentLegacyReviewLog),
	{
		name:  string(ticket.FileSkipUnreadable),
		count: func(a *ticket.AuditReport) int { u, _ := splitFileSkips(a.SkippedFiles); return len(u) },
		print: func(a *ticket.AuditReport) { u, _ := splitFileSkips(a.SkippedFiles); printUnreadableFiles(u) },
	},
	{
		name:  string(ticket.FileSkipForeignNamespace),
		count: func(a *ticket.AuditReport) int { _, f := splitFileSkips(a.SkippedFiles); return len(f) },
		print: func(a *ticket.AuditReport) { _, f := splitFileSkips(a.SkippedFiles); printForeignFiles(f) },
	},
}

func parentCause(kind ticket.ParentViolationKind) auditCause {
	pick := func(audit *ticket.AuditReport) []ticket.ParentViolation {
		var picked []ticket.ParentViolation
		for _, v := range audit.Violations {
			if v.Kind == kind {
				picked = append(picked, v)
			}
		}
		return picked
	}
	return auditCause{
		name:  string(kind),
		count: func(a *ticket.AuditReport) int { return len(pick(a)) },
		print: func(a *ticket.AuditReport) { printParentViolations(pick(a)) },
	}
}

func driftCause(kind ticket.EpicDriftKind) auditCause {
	pick := func(audit *ticket.AuditReport) []ticket.EpicStatusDrift {
		var picked []ticket.EpicStatusDrift
		for _, d := range audit.EpicStatus {
			if d.Kind == kind {
				picked = append(picked, d)
			}
		}
		return picked
	}
	return auditCause{
		name:  string(kind),
		count: func(a *ticket.AuditReport) int { return len(pick(a)) },
		print: func(a *ticket.AuditReport) { printEpicStatusDrift(kind, pick(a)) },
	}
}

func contentCause(kind ticket.ContentIssueKind) auditCause {
	pick := func(audit *ticket.AuditReport) []ticket.ContentIssue {
		var picked []ticket.ContentIssue
		for _, c := range audit.Content {
			if c.Kind == kind {
				picked = append(picked, c)
			}
		}
		return picked
	}
	return auditCause{
		name:  string(kind),
		count: func(a *ticket.AuditReport) int { return len(pick(a)) },
		print: func(a *ticket.AuditReport) { printContentIssues(kind, pick(a)) },
	}
}

func findAuditCause(name string) *auditCause {
	for i := range auditCauses {
		if auditCauses[i].name == name {
			return &auditCauses[i]
		}
	}
	return nil
}

func auditCauseNames() []string {
	names := make([]string, 0, len(auditCauses))
	for _, c := range auditCauses {
		names = append(names, c.name)
	}
	return names
}

func runAudit(cmd *cobra.Command, args []string) error {
	// Resolved before the store is read, and before the --json branch: a name
	// nothing can list is the caller's error whatever the output mode, and a
	// full report printed under it would answer a question nobody asked. Usage
	// is silenced for the reason unreadableExit silences it — the command is
	// fine, the input is not.
	var cause *auditCause
	if len(args) > 0 {
		if cause = findAuditCause(args[0]); cause == nil {
			cmd.SilenceUsage = true
			return fmt.Errorf("unknown audit cause %q — valid causes are: %s", args[0], strings.Join(auditCauseNames(), ", "))
		}
	}

	root, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("audit requires a configured central store: %w", err)
	}
	store := ticket.NewMultiStore(filepath.Join(root, "tickets"))

	audit, err := ticket.Audit(store)
	if err != nil {
		return err
	}

	proj, _ := cmd.Flags().GetString("project")
	if proj != "" {
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

	unreadable, _ := splitFileSkips(audit.SkippedFiles)

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
		// The whole report, whatever cause was named: this mode is read by
		// scripts, it buries nothing, and filtering it would break a caller that
		// asked for one cause's listing and reads the rest of the object. The
		// count line below would not be JSON, so the exit code is all this mode
		// carries — the files themselves are already in skipped_files.
		return unreadableExit(cmd, unreadable)
	}

	if cause == nil {
		printAuditSummary(cmd, &audit, proj)
	} else if cause.count(&audit) == 0 {
		fmt.Printf("No %s findings.\n", cause.name)
	} else {
		cause.print(&audit)
	}
	// A project or a file the audit could not read is named rather than dropped,
	// in either form: without it, a summary of zeros would speak for a store
	// never fully read. It covers every cause, all of which are read in the same
	// per-project pass.
	printIncompleteReport(audit.Skipped, unreadable)
	return unreadableExit(cmd, unreadable)
}

// splitFileSkips divides the skipped files by whether they left the store read
// in part, through the same predicate the derivation degrades on — keyed off
// the kind's own answer rather than a second test here, so the report and the
// epic statuses it prints cannot disagree about which files made it partial.
// Only an unreadable file does; a file naming another project was read in full
// and is a finding of its own.
func splitFileSkips(skips []ticket.FileSkip) (unreadable, foreign []ticket.FileSkip) {
	for _, f := range skips {
		if f.Kind.DegradesEpicStatus() {
			unreadable = append(unreadable, f)
			continue
		}
		foreign = append(foreign, f)
	}
	return unreadable, foreign
}

// printAuditSummary reports the store one line per cause and lists nothing. The
// listings are what made the report unreadable: when this was split,
// bare-acceptance alone ran to 605 lines of the live store and
// legacy-review-log to 128, which between them pushed the 11 parent violations
// and the 14 drifting epics — the findings most likely to need acting on —
// hundreds of lines off the top of the terminal. A report that is correct and
// cannot be read is wrong. Capping the listings was
// rejected when bare-acceptance landed, because a capped list drops tickets
// nothing can then reach; splitting the two outputs keeps every ticket
// reachable, which is why each line names the command that prints it.
func printAuditSummary(cmd *cobra.Command, audit *ticket.AuditReport, proj string) {
	// The drill-in command carries the scope this run was made under, so the
	// command as printed lists the tickets these counts were taken over.
	scope := ""
	if proj != "" {
		scope = " --project=" + shellQuoteProject(proj)
	}
	counts := make([]int, len(auditCauses))
	nameWidth, countWidth, total := 0, 1, 0
	for i, c := range auditCauses {
		counts[i] = c.count(audit)
		total += counts[i]
		if len(c.name) > nameWidth {
			nameWidth = len(c.name)
		}
		if w := len(strconv.Itoa(counts[i])); w > countWidth {
			countWidth = w
		}
	}
	fmt.Println("Findings by cause. Each line names the command that lists that cause's tickets.")
	fmt.Println()
	for i, c := range auditCauses {
		fmt.Printf("%-*s  %*d  %s%s %s\n", nameWidth, c.name, countWidth, counts[i], cmd.CommandPath(), scope, c.name)
	}
	if total == 0 {
		// Said in words rather than left to a column of zeros: a table reporting
		// nothing reads like a report that ran into something.
		fmt.Println("\nNo findings — every cause above is clear.")
	}
}

// auditSafeToken is the conservative set a project name prints bare in: it
// carries no shell meaning in any position, so quoting it would be noise on the
// ordinary case.
var auditSafeToken = regexp.MustCompile(`^[A-Za-z0-9._@+/-]+$`)

// shellQuoteProject renders a --project value into the drill-in command. Every
// other string this report prints is only read; this one is printed to be
// copied into a shell and run, which is why it alone is held to a shell-safety
// rule: a name carrying a metacharacter, a quote or whitespace is
// single-quoted, exactly, so what runs is the project the counts were taken
// over rather than whatever the name expands to. Control bytes are stripped
// first, for the reason printIncompleteReport quotes the name it prints — a
// project name is a store directory name or a shared-config key another machine
// wrote. The flag is refused unless the local config already holds the name, so
// this is defence in depth rather than a live exploit.
func shellQuoteProject(proj string) string {
	proj = ticket.SanitizeControl(proj)
	if auditSafeToken.MatchString(proj) {
		return proj
	}
	return "'" + strings.ReplaceAll(proj, "'", `'\''`) + "'"
}

// printIncompleteReport warns that the counts above were taken over a store the
// audit could not read whole. It prints in both forms, since it qualifies every
// count in either. The projects are listed here or nowhere — a project that was
// never read is no cause, having nothing in it to classify — while the files are
// the `unreadable` cause and are enumerated by drilling into it, so this block
// carries their count and leaves the listing there.
func printIncompleteReport(skipped []ticket.ProjectSkip, unreadable []ticket.FileSkip) {
	if len(skipped) == 0 && len(unreadable) == 0 {
		return
	}
	// Only the half that has something to report is named: a run that skipped
	// one project and no file has nothing to say about files.
	var what []string
	if len(skipped) > 0 {
		what = append(what, fmt.Sprintf("%d project(s)", len(skipped)))
	}
	if len(unreadable) > 0 {
		what = append(what, fmt.Sprintf("%d file(s)", len(unreadable)))
	}
	// A header with nothing under it ends in a full stop rather than a colon.
	end := ":"
	if len(skipped) == 0 {
		end = "."
	}
	fmt.Printf("\nwarning: %s could not be read, so this report is incomplete%s\n", strings.Join(what, " and "), end)
	// The project name came off a store another machine writes into — the reason
	// is flattened to one line where the skip is built, and the name is quoted
	// here so control bytes in it cannot reach the terminal raw. A project name
	// is a directory name in the synced store, bounded against path separators
	// and nothing else, so it needs the quote as much as a filename does.
	for _, s := range skipped {
		fmt.Printf("  project %q: %s\n", s.Project, s.Error)
	}
}

// unreadableExit is the error a run that found unreadable files ends with, so
// the command exits non-zero. A file no listing yields is a finding, and a
// scripted caller reads the exit code rather than the report — a zero there
// says the store is clean, which is the one thing an unreadable file rules out.
// Only this class exits non-zero: a file naming another project, and every
// other cause, keep the exit code they had. Usage is silenced because the
// command ran correctly; what is wrong is the store.
func unreadableExit(cmd *cobra.Command, unreadable []ticket.FileSkip) error {
	if len(unreadable) == 0 {
		return nil
	}
	cmd.SilenceUsage = true
	return fmt.Errorf("audit: %d unreadable ticket file(s)", len(unreadable))
}

// printParentViolations lists the tickets breaking the one-level hierarchy in
// one of the shapes it breaks in. Each is refused by every write surface, so
// the listing is the only place a store written before the rule shows them.
func printParentViolations(violations []ticket.ParentViolation) {
	for _, v := range violations {
		fmt.Printf("%s  %s  parent: %s  (%s)\n", v.ID, v.Kind, v.Parent, v.Detail)
	}
	fmt.Printf("\n%d ticket(s) violate the one-level epic hierarchy — clear or repoint each parent\n", len(violations))
}

// printEpicStatusDrift reports the epics reading a different status than their
// file stores. Statuses were derived without migrating what was stored, so this
// is the only place the two can still be compared — an operator has no other
// way to find the epics whose displayed status moved.
func printEpicStatusDrift(kind ticket.EpicDriftKind, drift []ticket.EpicStatusDrift) {
	for _, d := range drift {
		fmt.Printf("%s  %s  stored: %s  reads: %s\n", d.ID, d.Kind, storedStatus(d.Stored), d.Derived)
	}
	fmt.Printf("\n%d epic(s) read the status their children imply rather than the one stored — the stored value is ignored, not migrated\n", len(drift))
	// The class is not bounded to files written before statuses were derived:
	// every write of an epic bakes the status it derived at that moment into the
	// file, so an ordinary edit today produces it. Said plainly, because the
	// remedy below is only a remedy for the older ones. Each drill-in lists one
	// kind, so the class named is the kind this was called for rather than one
	// read off a listing that may be empty.
	fmt.Printf("The %s class also arises from ordinary edits made since: any write of an epic stores the status it derived at that moment, which the next change to a child makes stale.\n", kind)
	if kind == ticket.EpicDriftStoredClosed {
		// Every epic listed under this kind stores it, so the count on the line
		// above already covers them and is not repeated here.
		fmt.Print("Each stores `closed` with no abandon flag: either a hand-close from before statuses were derived, or a write that carried a derived `closed` into the file — the file cannot say which. " +
			"A stored value is evidence of a decision only on a file older than derived statuses; run `tk edit <id> --status closed` on each that should stay abandoned, and do it before editing the epic, since the next write of the epic replaces the stored value with the derived one. " +
			"On a file written since, the stored `closed` is an artifact — closing the epic would close a child nobody asked to close\n")
	}
}

// printContentIssues lists one class of ticket whose stored body is missing
// content it was meant to carry, is carrying criteria nothing can check, or is
// still storing a legacy Review Log. Every class is silent everywhere else: the
// ticket lists and renders, and only reading its text shows that the contract is
// not there, or is there and undecidable — or that the file holds a section
// nothing reads. Each prints alone, count and remedy included, because each is
// reached by its own command.
//
// An ID carries its project namespace, and a project name is a store directory
// name or a shared-config key another machine wrote — bounded against path
// separators and nothing else — so it goes through the same rule every other
// untrusted string tk prints to an operator does. The parent-violation and
// epic-drift listings still print theirs raw; they are left as they are on
// purpose, for a separate change.
func printContentIssues(kind ticket.ContentIssueKind, issues []ticket.ContentIssue) {
	switch kind {
	case ticket.ContentEnvelopeFragment:
		for _, c := range issues {
			// The field is ours; the tail came off the store, so it is quoted the
			// way storedStatus quotes an unrecognised status.
			fmt.Printf("%s  %s  %s: %q\n", ticket.SanitizeControl(c.ID), c.Kind, c.Field, c.Detail)
		}
		fmt.Printf("\n%d section(s) absorbed part of the tool call that wrote them — the text that followed was never stored, so the real content is likely missing; rewrite each from the source\n", len(issues))
	case ticket.ContentBareAcceptance:
		bare := 0
		for _, c := range issues {
			bare += c.Bare
			fmt.Printf("%s  %s  %d bare criterion(s)\n", ticket.SanitizeControl(c.ID), c.Kind, c.Bare)
		}
		fmt.Printf("\n%d ticket(s) carry %d acceptance criterion(s) with neither a `verify:` nor an `unverifiable:` line — the criteria state what done means and nothing decides whether they are met. "+
			"Add a `verify: <command>` line under each, or an `unverifiable: <reason>` line saying why no command can exist. "+
			"The count is a census and spans the whole store, done and closed tickets included; the open and ready ones are the actionable half\n", len(issues), bare)
	case ticket.ContentLegacyReviewLog:
		bytes := 0
		for _, c := range issues {
			bytes += c.Bytes
			fmt.Printf("%s  %s  %d bytes\n", ticket.SanitizeControl(c.ID), c.Kind, c.Bytes)
		}
		fmt.Printf("\n%d ticket(s) still store a legacy `## Review Log` section, %d bytes in total — nothing has read it since v7 retired the review system, and each ticket's next write drops its own for good. "+
			"Clear them deliberately, or leave them and expect the warning a write prints; either way the content stays in the store's git history\n", len(issues), bytes)
	default:
		// ContentEmptyAcceptance, and any later kind registered above without a
		// printer of its own: listed here so it is visible in the report rather
		// than dropped, and read as empty-acceptance until it is given a case.
		for _, c := range issues {
			fmt.Printf("%s  %s\n", ticket.SanitizeControl(c.ID), c.Kind)
		}
		fmt.Printf("\n%d ticket(s) carry a description with no acceptance criteria — nothing states what done means. "+
			"The count is a census and includes finished tickets and backlog stubs; the open and ready ones are the actionable half, since /capture and /work both gate on that contract\n", len(issues))
	}
}

// printUnreadableFiles lists the files that yielded no ticket at all. They are
// the one cause that also qualifies the rest of the report, which is why the
// incompleteness warning names their count wherever the run went.
func printUnreadableFiles(files []ticket.FileSkip) {
	// Project and filename quoted for the reason the incompleteness warning
	// quotes the project name it prints: both came off a store another machine
	// writes into, and the reason is already flattened to one line.
	for _, f := range files {
		fmt.Printf("project %q, file %q: %s\n", f.Project, f.File, f.Error)
	}
	// Counted in the shape the other causes are counted in: it is a violation
	// class of its own, not a caveat on them.
	fmt.Printf("\n%d file(s) could not be read as tickets — repair or remove each file\n", len(files))
	// An unreadable file is a ticket nothing can place, so no epic in its project
	// can claim every child finished. Said here because the epic causes report
	// the degraded values without explaining them.
	fmt.Println("An unreadable file could be any epic's child, so no epic in its project reads done or closed until the file is fixed or removed.")
}

// printForeignFiles lists the files whose stored id names another project. The
// directory decides a ticket's project, so a file disagreeing with the one
// holding it is read as nobody's ticket: it answers no reference there and
// appears in no listing. This is the only place it is visible at all — and it is
// stated as a repair, since the ticket itself is intact.
func printForeignFiles(files []ticket.FileSkip) {
	// Project and filename quoted for the reason the block above states.
	for _, f := range files {
		fmt.Printf("project %q, file %q: %s\n", f.Project, f.File, f.Error)
	}
	fmt.Printf("\n%d file(s) store an id naming another project, so they are not read as tickets where they sit. "+
		"The audit read each of these in full, so the rest of the report is complete without them. "+
		"Move the file to the project its id names, or fix the id field — until then it is in no listing, resolves for no reference, and counts as no epic's child, so an epic that was counting it can now read done\n", len(files))
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
