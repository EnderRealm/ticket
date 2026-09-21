package ticket

import "fmt"

// Findings is what an audit says about one ticket: the way its parent breaks
// the one-level hierarchy, the difference between the status its file stores
// and the one it derives, and what its stored body is missing or holds that no
// reader sees. At most one parent violation and at most one drift, which is
// what the store-wide report carries per ticket too — the two readings are the
// same code (auditContext.findings), so they cannot come to disagree the way a
// second scan of a body once did (BodySections) and a second count of bare
// criteria twice did (BareCriteria).
//
// There is deliberately no field a FileSkip or a ProjectSkip can land in.
// Those describe a file that never became a ticket and a project that could
// not be listed at all; a ticket handed to this API loaded, so neither class
// can apply to it. Leaving them out makes that unexpressible rather than merely
// empty — an API that returned them always empty would invite a caller to read
// "none found" as "checked and clean".
type Findings struct {
	Parent     *ParentViolation `json:"parent,omitempty"`
	EpicStatus *EpicStatusDrift `json:"epic_status,omitempty"`
	Content    []ContentIssue   `json:"content,omitempty"`
}

// Empty reports that nothing was found wrong with the ticket. It is not what a
// failed call returns: a ticket the auditor cannot evaluate comes back with an
// error, so "could not check" is never read as "clean".
func (f Findings) Empty() bool {
	return f.Parent == nil && f.EpicStatus == nil && len(f.Content) == 0
}

// Auditor answers what is wrong with a single ticket, prepared once against a
// store and then asked per ticket. Every check needs the whole store — a parent
// resolves against the other tickets, an epic's status derives from its
// children — so a function taking a store and one ticket would read the store
// once per ticket, and auditing N tickets would cost N listings. One snapshot
// is taken here instead, and every ticket is answered off it; Audit is the
// same per-ticket function fanned out over what was listed, not a second path.
//
// What it holds is the store as it stood at NewAuditor time, read once and
// never refreshed, so a caller that writes to the store prepares a new auditor
// rather than asking this one about the state its write produced.
//
// The snapshot resolves every reference the way the write path does — a bare
// parent names its child's own project, a qualified one its own — so the
// audit clears exactly the tickets a write accepts and flags exactly the ones
// it refuses. Resolving through MultiStore.Get instead would disagree with
// enforcement three ways: it accepts another project's prefix, resolves a
// bare ID into another project, and turns a bare ID that matches in two
// projects into a false parent-missing.
type Auditor struct {
	// multi says the store is a central one, whose report namespaces every ID.
	// A single project's store reports bare IDs even when it has a project name
	// of its own, which is what every other reader of it sees.
	multi bool
	snap  *Snapshot
	// contexts in namespace order, which is the order Report walks; a
	// single-project auditor holds exactly one.
	contexts  []*auditContext
	byProject map[string]*auditContext
	skipped   []ProjectSkip
}

// auditContext is one namespace of the snapshot: its tickets with the IDs the
// store reports — bare on a single project's store, qualified across a central
// one — and the snapshot every check resolves against.
type auditContext struct {
	snap    *Snapshot
	project string
	tickets []*Ticket
	// bare says the context reports bare IDs, so a ticket's qualified twin in
	// the snapshot is found by qualifying with project first.
	bare bool
}

// NewAuditor prepares an audit of a store. Every read the checks need happens
// here, so Ticket reads nothing.
//
// A namespace the snapshot could not read is a ProjectSkip, so the report
// never calls a store clean that it never read in full. The files inside the
// namespaces it could read are carried as they are, stamped with their
// project — the ID namespacing Report does cannot reach them: a file that did
// not parse has no ID to namespace, and one whose stored ID names another
// project has an ID that must not be relabelled with the project it was found
// in, which is the whole reason it is a skip.
func NewAuditor(store Store) (*Auditor, error) {
	snap, err := snapshotOf(store)
	if err != nil {
		return nil, err
	}
	return AuditorOver(store, snap), nil
}

// AuditorOver is NewAuditor over a snapshot the caller already holds, for a
// caller that read the store once for its own answer and wants the audit off
// that same reading rather than a second one — the MCP ticket_show handler,
// whose response for an epic or a mis-parented leaf is already derived from a
// snapshot. The store supplies only what the snapshot does not carry: whether
// it is a central store, whose report namespaces every ID, and for a single
// project's store its project name, which is the namespace its bare IDs
// resolve in. Nothing is read from it.
func AuditorOver(store Store, snap *Snapshot) *Auditor {
	_, multi := store.(*MultiStore)
	a := &Auditor{multi: multi, snap: snap, byProject: map[string]*auditContext{}}
	for _, skip := range snap.Skips {
		if skip.Kind == FileSkipNamespace {
			// A catalog that could not be read is a namespace skip with no
			// project; labelled as skipLine and warnSkips label it, rather than
			// reported as a project with no name.
			name := skip.Project
			if name == "" {
				name = "catalog"
			}
			a.skipped = append(a.skipped, ProjectSkip{Project: name, Error: skip.Error})
		}
	}
	if !multi {
		project := storeProject(store)
		a.contexts = []*auditContext{{snap: snap, project: project, tickets: projectView(snap, project), bare: true}}
		return a
	}
	byNS := map[string][]*Ticket{}
	for _, t := range snap.Tickets {
		ns := namespaceOf(t.ID)
		byNS[ns] = append(byNS[ns], t)
	}
	for _, ns := range snap.namespaces {
		ctx := &auditContext{snap: snap, project: ns, tickets: byNS[ns]}
		a.contexts = append(a.contexts, ctx)
		a.byProject[ns] = ctx
	}
	return a
}

// Ticket reports what the audit finds wrong with one ticket. Nothing is listed
// and no lookup is built here: the ticket is routed to the context prepared for
// its project, and the checks answer from the snapshot. How the ticket was
// read does not change the answer: the epic-status check takes the stored side
// from the snapshot rather than from the copy it was handed.
//
// The error is what tells a caller the ticket could not be evaluated, as
// opposed to being clean — a ticket namespaced to a project this audit did not
// prepare, or a bare ID against a central store, where a bare ID names no one
// project's rules to judge it by.
func (a *Auditor) Ticket(t *Ticket) (Findings, error) {
	ctx, err := a.contextFor(t)
	if err != nil {
		return Findings{}, err
	}
	return ctx.findings(t), nil
}

// Report is the whole store's audit: the same per-ticket check over every
// ticket each context listed, in listing order, plus the projects that could
// not be read and the files inside the ones that could. A finding carries the
// ID its context reports — namespaced across a central store, bare on a
// single project's store, where every other reader sees the bare IDs its
// listing yields.
func (a *Auditor) Report() AuditReport {
	report := AuditReport{Skipped: a.skipped}
	for _, c := range a.contexts {
		for _, t := range c.tickets {
			f := c.findings(t)
			if f.Parent != nil {
				report.Violations = append(report.Violations, *f.Parent)
			}
			if f.EpicStatus != nil {
				report.EpicStatus = append(report.EpicStatus, *f.EpicStatus)
			}
			report.Content = append(report.Content, f.Content...)
		}
	}
	for _, skip := range a.snap.Skips {
		if skip.Kind != FileSkipNamespace {
			report.SkippedFiles = append(report.SkippedFiles, skip)
		}
	}
	return report
}

// contextFor routes a ticket to the project context prepared for it, by the
// namespace its own ID carries. A central store's tickets each belong to one
// project's rules, and a bare ID names none of them — the same reason the audit
// resolves per project rather than through MultiStore.Get.
func (a *Auditor) contextFor(t *Ticket) (*auditContext, error) {
	proj, _ := ParseNamespacedID(t.ID)
	if !a.multi {
		single := a.contexts[0]
		if proj != "" && proj != single.project {
			return nil, fmt.Errorf("ticket %s names project %s, which this audit was not prepared against", t.ID, proj)
		}
		return single, nil
	}
	if proj == "" {
		return nil, fmt.Errorf("ticket %s: a central store's audit resolves a ticket by its project — pass a namespaced ID", t.ID)
	}
	if ctx, ok := a.byProject[proj]; ok {
		return ctx, nil
	}
	// Keyed on the snapshot's failed namespaces rather than on the report's
	// ProjectSkips, whose catalog entry carries a label and not a namespace.
	if reason, failed := a.snap.failed[proj]; failed {
		return nil, fmt.Errorf("ticket %s: project %s could not be read by this audit: %s", t.ID, proj, reason)
	}
	return nil, fmt.Errorf("ticket %s: project %s is not one this audit prepared", t.ID, proj)
}

// findings runs the three checks over one ticket, which is the single reading
// of what is wrong with it: `tk show`, the TUI's detail page, the MCP response
// and the store-wide report all come through here rather than each deriving
// their own. The ID is carried verbatim from the ticket it was handed.
func (c *auditContext) findings(t *Ticket) Findings {
	return Findings{
		Parent:     c.parentViolation(t),
		EpicStatus: c.epicStatusDrift(t),
		Content:    contentIssues(t),
	}
}

// qualified is a ticket's ID as the snapshot keys it. A bare context prefixes
// only an ID that carries no namespace: contextFor admits one already naming
// the context's own project, and prefixing that again would key nothing.
func (c *auditContext) qualified(id string) string {
	if c.bare {
		return qualifyRef(c.project, id)
	}
	return id
}

// parentOf resolves a snapshot ticket's parent the snapshot's way — bare means
// the child's own namespace — for the cycle walk.
func (c *auditContext) parentOf(t *Ticket) (*Ticket, error) {
	parent, ok := c.snap.Get(qualifyRef(namespaceOf(t.ID), t.Parent))
	if !ok {
		return nil, fmt.Errorf("parent %s does not resolve", t.Parent)
	}
	return parent, nil
}

// parentViolation runs the checks the write path runs, plus the cycle class
// only a pre-rule store can hold. The parent resolves against the snapshot by
// the owner-relative rule; unlike the write path this only reads — nothing
// here rewrites a parent to what it resolved to, and a parent stored as a
// fragment is reported as missing rather than resolved by substring. One
// violation per ticket, most specific first — a cycle is reported as a cycle
// rather than as the non-epic parent each of its members also has.
func (c *auditContext) parentViolation(t *Ticket) *ParentViolation {
	if t.Parent == "" {
		return nil
	}
	violation := func(kind ParentViolationKind, detail string) *ParentViolation {
		return &ParentViolation{ID: t.ID, Parent: t.Parent, Kind: kind, Detail: detail}
	}
	if t.Type == TypeEpic {
		return violation(ViolationEpicHasParent, "epics are top level and cannot have a parent")
	}
	parentID := qualifyRef(c.project, t.Parent)
	parentNS := namespaceOf(parentID)
	if parentNS != c.project && !c.snap.crossProject {
		return violation(ViolationParentCrossProject, fmt.Sprintf("an epic and its children must live in the same project until the catalog requires %s", FeatureCrossProjectParents))
	}
	parent, ok := c.snap.Get(parentID)
	if !ok {
		if reason, failed := c.snap.failed[parentNS]; failed {
			return violation(ViolationParentMissing, fmt.Sprintf("parent %s is in namespace %q, which could not be read: %s", parentID, parentNS, reason))
		}
		return violation(ViolationParentMissing, fmt.Sprintf("parent %s does not resolve", parentID))
	}
	if twin, ok := c.snap.Get(c.qualified(t.ID)); ok {
		if chain := parentCycle(c.parentOf, twin); chain != "" {
			return violation(ViolationParentCycle, chain)
		}
	}
	if parent.Type != TypeEpic {
		return violation(ViolationParentNotEpic, fmt.Sprintf("parent %s is type %s", parent.ID, parent.Type))
	}
	return nil
}

// epicStatusDrift reports an epic whose file stores a status other than the one
// it now derives. Deriving replaced the stored value rather than migrating it,
// so the difference is invisible to every reader — the derivation happens at
// the read choke point, and nothing else can show what the file holds.
//
// Derived with the same function every reader gets its value from —
// degradation included, or the audit would compare against a status no reader
// is shown and report drift that is not there while missing drift that is.
//
// The stored side comes from the snapshot, which kept it beside the derived
// value, rather than from the status the caller's copy carries, so the answer
// does not depend on how the ticket was read: every exported read derives
// (Get, List), and comparing a derived status against itself would report
// every epic clean — a check that could not run, indistinguishable from one
// that ran and found nothing. A ticket the snapshot does not hold is judged by
// the status it carries, which is all there is.
func (c *auditContext) epicStatusDrift(t *Ticket) *EpicStatusDrift {
	if t.Type != TypeEpic {
		return nil
	}
	id := c.qualified(t.ID)
	derived := derivedEpicStatus(t.Abandoned, c.snap.Children(id), !c.snap.Complete)
	stored := t.Status
	if twin, ok := c.snap.epicStored[id]; ok {
		stored = twin
	}
	if derived == stored {
		return nil
	}
	// A stored closed is the one drift that may have been a decision, and the
	// file cannot say whether it was — it is separated out so the operator sees
	// the candidates rather than a verdict.
	kind := EpicDriftStale
	if stored == StatusClosed && !t.Abandoned {
		kind = EpicDriftStoredClosed
	}
	return &EpicStatusDrift{ID: t.ID, Stored: stored, Derived: derived, Kind: kind}
}

// BodyFindings is the audit of a ticket's own body and nothing else, for a
// caller holding no snapshot that does not want to take one. Of the three
// checks it runs only the one that reads no store. That is the audit's whole
// answer for a non-epic with no parent: the parent check is nil with no
// parent, the drift check is nil for a non-epic. For a non-epic whose parent
// resolved to an epic it omits only the cycle class the parent check can
// reach, which exists only in a store written before the one-level rule. An
// epic, or a leaf whose parent did not resolve to an epic, is not answered in
// full here; audit it over a snapshot.
func BodyFindings(t *Ticket) Findings {
	return Findings{Content: contentIssues(t)}
}

// EmptyAcceptance reports whether the ticket's stored body carries a
// description with no acceptance criteria beside it. It is the one definition
// of that classification: `tk audit` reports it through contentIssues and
// ticket_create warns of it through the same call, so the two cannot disagree
// on a ticket. Read off BodySections rather than any write's arguments, because
// a description that carries its own `## Acceptance Criteria` section is stored
// as criteria. An epic is a container: its children carry the contract, so it
// has no criteria to be missing.
func EmptyAcceptance(t *Ticket) bool {
	desc, _, acceptance, _ := BodySections(t.Body)
	return t.Type != TypeEpic && desc != "" && acceptance == ""
}

// contentIssues reports what a ticket's stored body is missing, in the two
// shapes an MCP write can leave behind: a section that ends in a tool-call
// envelope fragment, which means the text after it was absorbed rather than
// stored, and a description with no acceptance criteria, which is a ticket
// neither /capture nor /work will accept. The check that refuses both now runs
// at the MCP boundary; this is what finds the ones already written. It also
// reports criteria carrying neither a verify command nor an unverifiable claim,
// which the create-time warning only catches on tickets not yet written, and a
// legacy `## Review Log`, which is the opposite case — content that is there
// and is read by nothing. Read-only, like the rest of the audit.
//
// Nothing about the store is consulted, only the ticket's own body, so this
// takes no context.
func contentIssues(t *Ticket) []ContentIssue {
	var issues []ContentIssue
	desc, design, acceptance, testResults := BodySections(t.Body)
	for _, f := range []struct{ field, value string }{
		{"description", desc},
		{"design", design},
		{"acceptance", acceptance},
		{"test_results", testResults},
	} {
		if tail, ok := EnvelopeFragment(f.value); ok {
			issues = append(issues, ContentIssue{ID: t.ID, Kind: ContentEnvelopeFragment, Field: f.field, Detail: tail})
		}
	}
	if EmptyAcceptance(t) {
		issues = append(issues, ContentIssue{ID: t.ID, Kind: ContentEmptyAcceptance})
	}
	// Criteria that were written and cannot be checked, through the same reading
	// BareCriteria gives `tk create` and ticket_create — the warning there only
	// reaches tickets not yet created, and a second scan here is how two
	// definitions of "bare" would come to disagree. No type exemption, unlike the
	// check above: an epic carries no contract of its own, but one that does
	// state criteria and leaves them bare has the same gap. A ticket with no
	// acceptance section parses to no criteria and is never reported.
	if bare := BareCriteria(t.Body); len(bare) > 0 {
		issues = append(issues, ContentIssue{ID: t.ID, Kind: ContentBareAcceptance, Bare: len(bare)})
	}
	// The parse above stripped the section, and the ticket's next write is what
	// removes it from the file. Listing them is what makes the store
	// determinate: the retirement is a decision to take once over a known set,
	// rather than one that executes a ticket at a time whenever something happens
	// to write.
	if t.droppedReviewLog > 0 {
		issues = append(issues, ContentIssue{ID: t.ID, Kind: ContentLegacyReviewLog, Bytes: t.droppedReviewLog})
	}
	return issues
}

// ProjectSkip is a project the audit could not read, and why. The reason is
// flattened to one line by oneLine, like a FileSkip's.
type ProjectSkip struct {
	Project string `json:"project"`
	Error   string `json:"error"`
}

// AuditReport is what Audit found: the parent violations, the epics whose
// stored status no longer matches the one they derive, the tickets whose stored
// body is missing content it was meant to carry, the projects it could not
// read, and the individual files no listing in their project yields — the ones
// it could not read and the ones whose stored ID names another project, told
// apart by FileSkipKind. Every skip is part of the result rather than swallowed
// — a report that silently covered less than the whole store would call a store
// clean that a write can still trip on, which is the wrong way to fail. A
// single unreadable file is the same failure at a finer grain, and it is the
// one the epic-status section cannot work around: the missing ticket may be any
// epic's child. A file naming another project is a ticket the project holding
// it cannot place rather than one nothing could read, so it is reported without
// making the report partial (FileSkipKind.DegradesEpicStatus).
type AuditReport struct {
	Violations   []ParentViolation `json:"violations"`
	EpicStatus   []EpicStatusDrift `json:"epic_status"`
	Content      []ContentIssue    `json:"content"`
	Skipped      []ProjectSkip     `json:"skipped,omitempty"`
	SkippedFiles []FileSkip        `json:"skipped_files,omitempty"`
}

// Audit reports every ticket whose parent breaks the one-level hierarchy, so a
// store predating the rule can be cleaned before a write trips on it, every
// epic reading a different status than its file stores, so a store predating
// derived epic statuses can be reconciled, and every ticket whose stored body
// is missing content — a section that absorbed part of the tool call that wrote
// it, or a description with no acceptance criteria — so the ones already
// written are findable now that the write path refuses them, and every ticket
// still storing a legacy `## Review Log`, so the sections the v7 retirement
// leaves for the next write to drop are a known set rather than an unknown one.
// Strictly read-only: nothing is repaired or rewritten.
//
// It is an Auditor over the whole store: the store-wide question and the
// per-ticket one are answered by the same check, so the two cannot disagree.
func Audit(store Store) (AuditReport, error) {
	auditor, err := NewAuditor(store)
	if err != nil {
		return AuditReport{}, err
	}
	return auditor.Report(), nil
}
