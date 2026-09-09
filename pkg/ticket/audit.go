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
// once per ticket, and auditing N tickets would cost N listings. The listing,
// the parent index and the children map are built here instead, once, and
// every ticket is answered off them; Audit is the same per-ticket function
// fanned out over what was listed, not a second path.
//
// What it holds is the store as it stood at NewAuditor time, read once and
// never refreshed, so a caller that writes to the store prepares a new auditor
// rather than asking this one about the state its write produced.
//
// A MultiStore is prepared project by project, against the same per-project
// FileStore the write path validates against. Resolving through MultiStore.Get
// instead would disagree with enforcement three ways — it accepts another
// project's prefix, resolves a bare ID into another project, and turns a bare
// ID that matches in two projects into a false parent-missing — so the audit
// would clear tickets that no write can touch and flag tickets writes accept.
type Auditor struct {
	// multi says the store is a central one, whose report namespaces every ID.
	// A single project's store reports bare IDs even when it has a project name
	// of its own, which is what every other reader of it sees.
	multi bool
	// contexts in listing order, which is the order Report walks; a
	// single-project auditor holds exactly one.
	contexts  []*auditContext
	byProject map[string]*auditContext
	skipped   []ProjectSkip
}

// auditContext is one project's store, read once: the tickets as their files
// hold them, the files that yielded none, the parent index and children map the
// per-ticket checks resolve against, the same listing indexed by ID so a check
// can reach a ticket's stored twin, and whether the listing was partial.
type auditContext struct {
	store      Store
	project    string
	tickets    []*Ticket
	skips      []FileSkip
	parentOf   func(*Ticket) (*Ticket, error)
	storedByID func(string) (*Ticket, bool)
	children   map[string][]*Ticket
	incomplete bool
}

// NewAuditor prepares an audit of a store. Every read the checks need happens
// here, so Ticket reads nothing.
func NewAuditor(store Store) (*Auditor, error) {
	m, ok := store.(*MultiStore)
	if !ok {
		ctx, err := newAuditContext(store)
		if err != nil {
			return nil, err
		}
		return &Auditor{contexts: []*auditContext{ctx}}, nil
	}

	projects, err := m.projects()
	if err != nil {
		return nil, err
	}
	a := &Auditor{multi: true, byProject: make(map[string]*auditContext, len(projects))}
	for _, proj := range projects {
		projStore, err := m.storeFor(proj)
		if err != nil {
			a.skipped = append(a.skipped, ProjectSkip{Project: proj, Error: oneLine(err)})
			continue
		}
		ctx, err := newAuditContext(projStore)
		if err != nil {
			a.skipped = append(a.skipped, ProjectSkip{Project: proj, Error: oneLine(err)})
			continue
		}
		a.contexts = append(a.contexts, ctx)
		a.byProject[proj] = ctx
	}
	return a, nil
}

// newAuditContext reads one project's store and builds everything the checks
// resolve against. One listing serves all three: only stored fields are read —
// a parent's type, an epic's own status and its children's, a body — so no
// derived status is needed anywhere, and listStored is what reports a stored
// epic status at all. It is also read through rather than List because List
// warns about every file it skipped, and the skips are carried in the report
// instead, where they are one entry per file however many times the audit
// consults the listing.
//
// The skipped files are stamped with the project here, where the store that
// produced them is in hand — the ID namespacing Report does cannot reach them:
// a file that did not parse has no ID to namespace, and one whose stored ID
// names another project has an ID that must not be relabelled with the project
// it was found in, which is the whole reason it is a skip. The stamp is the
// directory it sat in, which is what an operator needs to go and look.
func newAuditContext(store Store) (*auditContext, error) {
	tickets, skips, err := listStored(store)
	if err != nil {
		return nil, err
	}
	project := storeProject(store)
	for i := range skips {
		skips[i].Project = project
	}
	return &auditContext{
		store:   store,
		project: project,
		tickets: tickets,
		skips:   skips,
		// Neither the index nor its fallback derives a status, so preparing an
		// audit of N tickets reads the store once.
		parentOf: parentLookup(store, tickets, func(id string) (*Ticket, error) {
			return readStored(store, id)
		}),
		// Indexed off the listing already in hand, so reaching a stored twin
		// costs no read of its own.
		storedByID: ticketsByID(tickets, project),
		children:   childrenByBareParent(tickets, project),
		incomplete: hasUnreadable(skips),
	}, nil
}

// Ticket reports what the audit finds wrong with one ticket. Nothing is listed
// and no lookup is built here: the ticket is routed to the context prepared for
// its project, and the checks answer from what that context already read. The
// one store read a ticket can still cost is parentLookup's fallback, for a
// parent no listed ticket matches — a single-ticket resolution, and the same
// one the store-wide audit makes for that ticket. How the ticket was read does
// not change the answer: the epic-status check takes the stored side from the
// context's listing rather than from the copy it was handed.
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
// not be read and the files inside the ones that could.
func (a *Auditor) Report() AuditReport {
	report := AuditReport{Skipped: a.skipped}
	for _, c := range a.contexts {
		for _, t := range c.tickets {
			f := c.findings(t)
			if f.Parent != nil {
				v := *f.Parent
				v.ID = a.reportID(c, v.ID)
				report.Violations = append(report.Violations, v)
			}
			if f.EpicStatus != nil {
				d := *f.EpicStatus
				d.ID = a.reportID(c, d.ID)
				report.EpicStatus = append(report.EpicStatus, d)
			}
			for _, issue := range f.Content {
				issue.ID = a.reportID(c, issue.ID)
				report.Content = append(report.Content, issue)
			}
		}
		report.SkippedFiles = append(report.SkippedFiles, c.skips...)
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
	for _, s := range a.skipped {
		if s.Project == proj {
			return nil, fmt.Errorf("ticket %s: project %s could not be read by this audit: %s", t.ID, proj, s.Error)
		}
	}
	return nil, fmt.Errorf("ticket %s: project %s is not one this audit prepared", t.ID, proj)
}

// reportID is the ID a finding carries in a store-wide report: namespaced under
// the project holding it across a central store, and bare on a single project's
// store, where every other reader sees the bare IDs its listing yields.
func (a *Auditor) reportID(c *auditContext, id string) string {
	if !a.multi {
		return id
	}
	return FormatNamespacedID(c.project, id)
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

// parentViolation runs the checks ResolveParent runs, plus the cycle class only
// a pre-rule store can hold. The parent resolves against the tickets the
// context already listed, falling back to the store for the partial forms
// stored parent fields can carry; unlike the write path this only reads them —
// nothing here rewrites a parent to what it resolved to. One violation per
// ticket, most specific first — a cycle is reported as a cycle rather than as
// the non-epic parent each of its members also has.
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
	if isCrossProjectParent(c.store, t) {
		return violation(ViolationParentCrossProject, "an epic and its children must live in the same project")
	}
	parent, err := c.parentOf(t)
	if err != nil {
		return violation(ViolationParentMissing, err.Error())
	}
	if chain := parentCycle(c.parentOf, t); chain != "" {
		return violation(ViolationParentCycle, chain)
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
// The stored side comes from the context's own listing rather than from the
// status the caller's copy carries, so the answer does not depend on how the
// ticket was read: every exported read derives (Get, List), and comparing a
// derived status against itself would report every epic clean — a check that
// could not run, indistinguishable from one that ran and found nothing. Only
// the status is taken from the stored twin, because it is the only field a
// derived read rewrites that any check reads; the caller's copy is the subject
// everywhere else. A ticket the listing does not hold is judged by the status
// it carries, which is all there is.
func (c *auditContext) epicStatusDrift(t *Ticket) *EpicStatusDrift {
	if t.Type != TypeEpic {
		return nil
	}
	_, bare := ParseNamespacedID(t.ID)
	derived := derivedEpicStatus(t.Abandoned, c.children[bare], c.incomplete)
	stored := t.Status
	if twin, ok := c.storedByID(t.ID); ok {
		stored = twin.Status
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
	// An epic is a container: its children carry the contract, so it has no
	// criteria to be missing.
	if t.Type != TypeEpic && desc != "" && acceptance == "" {
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
