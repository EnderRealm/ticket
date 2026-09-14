package ticket

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// InboundKind says which field of the referring ticket names the target.
type InboundKind string

const (
	InboundParent InboundKind = "parent"
	InboundDep    InboundKind = "dep"
	InboundLink   InboundKind = "link"
)

// InboundRef is one ticket naming another as its parent, a dependency or a
// link. From is the referring ticket's qualified ID.
type InboundRef struct {
	From string      `json:"from"`
	Kind InboundKind `json:"kind"`
}

// Snapshot is the relationship graph of a central store at one instant: every
// ticket in every namespace read once, keyed by qualified ID, with each epic's
// status and completion date already derived from all of its children and each
// leaf stamped with why its parent does not make it a child, when it does not.
//
// It is the one reading of the graph every consumer shares. A project-scoped
// listing is a filter over it after derivation, so an epic reads the same
// status whichever project the caller asked for, and a mutation validates
// against the snapshot the write lock was taken over rather than against the
// one project's directory.
//
// Complete is false whenever the tickets listed may not be the tickets there
// are: a file no parse could structure, two files in one namespace claiming
// one ID, a namespace that could not be listed or is catalogued but has no
// directory, a catalog that could not be read. Skips names each cause. While
// it is false no epic anywhere reads done or closed — the missing ticket could
// be any epic's child — and no operation that has to prove an absence (delete,
// move, epic-to-leaf, abandon) proceeds.
type Snapshot struct {
	Tickets  []*Ticket
	Skips    []FileSkip
	Complete bool

	byID     map[string]*Ticket
	children map[string][]*Ticket
	inbound  map[string][]InboundRef
	// epicStored is each epic's status as its file holds it, kept beside the
	// derived value the ticket carries because the audit compares the two.
	epicStored map[string]Status
	// claims is how many files claim each qualified ID. An ID claimed more
	// than once is absent from byID, and a lookup that misses has to tell
	// that apart from an ID nothing claims: the remedy is different.
	claims map[string]int
	// namespaces is every namespace read in full, in the order they were read.
	namespaces []string
	// failed is the namespaces whose tickets are not in the snapshot at all,
	// each with the reason, so a reference into one is reported as such rather
	// than as a ticket that does not exist.
	failed map[string]string
	// crossProject says the catalog requires cross-project-parents, so a leaf
	// may be the child of an epic in another namespace.
	crossProject bool
}

// Get returns the ticket with exactly this qualified ID. An ID two files claim
// is nobody's: both files are in Tickets, neither answers here.
func (s *Snapshot) Get(id string) (*Ticket, bool) {
	t, ok := s.byID[id]
	return t, ok
}

// Children returns the tickets whose parent validly resolves to this epic,
// across every namespace, in listing order.
func (s *Snapshot) Children(epicID string) []*Ticket {
	return s.children[epicID]
}

// Inbound returns every ticket naming this ID as its parent, a dependency or a
// link, whether or not the reference is valid: a delete has to know who would
// be left pointing at nothing.
func (s *Snapshot) Inbound(id string) []InboundRef {
	return s.inbound[id]
}

// Namespaces is every namespace the snapshot read in full, in the order they
// were read, as a copy. A consumer reporting what a listing covered names
// these; a namespace that could not be read is in Skips instead.
func (s *Snapshot) Namespaces() []string {
	return append([]string(nil), s.namespaces...)
}

// Diagnostics renders one line per skip, for a caller that reports why the
// snapshot is incomplete rather than reasoning about kinds.
func (s *Snapshot) Diagnostics() []string {
	var lines []string
	for _, skip := range s.Skips {
		lines = append(lines, skipLine(skip))
	}
	return lines
}

// Revision is a stable digest of the graph as it was read: every ticket's
// qualified ID with the version of the file bytes it came from, in sorted ID
// order, then every skip line in order, then the policy and the coverage the
// graph was built under — whether cross-project parents were activated, which
// namespaces were read in full and which could not be. Two snapshots of an
// unchanged store carry the same revision; any file written, added or
// removed, any change to what could not be read, and any catalog change that
// moves a child into or out of an epic without touching a ticket file,
// changes it. It is the token a paged consumer hands back so a page served
// off a later snapshot can be told apart from one served off this one,
// rather than mixed into the same membership.
func (s *Snapshot) Revision() string {
	entries := make([]string, 0, len(s.Tickets))
	for _, t := range s.Tickets {
		entries = append(entries, t.ID+"\x00"+t.version)
	}
	sort.Strings(entries)
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e))
		h.Write([]byte{'\n'})
	}
	for _, skip := range s.Skips {
		h.Write([]byte(skipLine(skip)))
		h.Write([]byte{'\n'})
	}
	fmt.Fprintf(h, "cross-project: %t\n", s.crossProject)
	fmt.Fprintf(h, "namespaces: %s\n", strings.Join(s.namespaces, "\x00"))
	failed := make([]string, 0, len(s.failed))
	for ns := range s.failed {
		failed = append(failed, ns)
	}
	sort.Strings(failed)
	for _, ns := range failed {
		fmt.Fprintf(h, "failed %s: %s\n", ns, s.failed[ns])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// EpicProgress is an epic's children counted by derived status, off the same
// snapshot the epic's own status was derived from, so the counts and the
// status cannot disagree. Complete is the snapshot's, and Diagnostics names
// what it could not read when it is not: a total over a partial read is a
// lower bound, not a count.
type EpicProgress struct {
	Total       int      `json:"total"`
	Done        int      `json:"done"`
	Closed      int      `json:"closed"`
	Open        int      `json:"open"`
	Ready       int      `json:"ready"`
	Backlog     int      `json:"backlog"`
	Complete    bool     `json:"complete"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// Progress counts the children of the epic with this qualified ID across
// every namespace. An ID with no children — a leaf, or an epic nothing names
// — counts zero of everything.
func (s *Snapshot) Progress(epicID string) EpicProgress {
	p := EpicProgress{Complete: s.Complete}
	for _, c := range s.children[epicID] {
		p.Total++
		switch c.Status {
		case StatusDone:
			p.Done++
		case StatusClosed:
			p.Closed++
		case StatusOpen:
			p.Open++
		case StatusReady:
			p.Ready++
		case StatusBacklog:
			p.Backlog++
		}
	}
	if !s.Complete {
		p.Diagnostics = s.Diagnostics()
	}
	return p
}

// Resolve turns an ID as a caller typed it into the exact qualified ID the
// snapshot holds. A qualified input has to exist exactly; a bare one names ns
// — the namespace it was typed in — and matches a bare ID there exactly first,
// then as a fragment the way FileStore.Resolve does, ambiguity refused. No
// other namespace is ever searched for a bare input, for the reason qualifyRef
// gives. An ID two files claim is nobody's and is refused as the duplicate it
// is; a reference into a namespace that could not be read is reported as
// such rather than as a ticket that does not exist. With no ns a bare input
// is looked up as it is, which is the single-store case where nothing is
// qualified.
func (s *Snapshot) Resolve(ns, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("id is required")
	}
	inputNS, fragment := ParseNamespacedID(input)
	if inputNS == "" {
		inputNS = ns
	}
	id := qualifyRef(ns, input)
	if _, ok := s.byID[id]; ok {
		return id, nil
	}
	if s.claims[id] > 1 {
		return "", errors.New(duplicateIssue(id))
	}
	var matches []string
	if namespaceOf(input) == "" {
		seen := map[string]bool{}
		for _, m := range s.partialMatches(ns, fragment) {
			if !seen[m.ID] {
				seen[m.ID] = true
				matches = append(matches, m.ID)
			}
		}
	}
	switch len(matches) {
	case 1:
		if s.claims[matches[0]] > 1 {
			return "", errors.New(duplicateIssue(matches[0]))
		}
		return matches[0], nil
	case 0:
		if reason, failed := s.failed[inputNS]; failed && inputNS != "" {
			return "", fmt.Errorf("ticket %s is in namespace %q, which could not be read: %s", id, inputNS, reason)
		}
		return "", fmt.Errorf("ticket %s not found", id)
	default:
		return "", fmt.Errorf("ambiguous ID %q matches: %s", input, strings.Join(matches, ", "))
	}
}

// skipLine is one skip as a single line of text. A namespace skip has no file,
// so it names the namespace alone; the reason is already flattened by oneLine.
func skipLine(skip FileSkip) string {
	switch {
	case skip.Kind == FileSkipNamespace && skip.Project == "":
		return "catalog: " + skip.Error
	case skip.Kind == FileSkipNamespace:
		return fmt.Sprintf("namespace %q: %s", skip.Project, skip.Error)
	case skip.Project != "":
		return fmt.Sprintf("%q (%s): %s", skip.Project+"/"+skip.File, skip.Kind, skip.Error)
	default:
		return fmt.Sprintf("%q (%s): %s", skip.File, skip.Kind, skip.Error)
	}
}

// QualifyRef is qualifyRef for a consumer outside the package that reads a
// stored reference off a ticket it has in hand: ownerNS is the namespace of
// the ticket holding the reference, and the result is the key Snapshot.Get
// answers to. The rule lives in one place; nothing outside reimplements it.
func QualifyRef(ownerNS, ref string) string {
	return qualifyRef(ownerNS, ref)
}

// qualifyRef resolves a stored reference relative to the namespace of the
// ticket that holds it: a bare reference names that namespace, a qualified one
// stays as it is. This is the whole of how a reference is read — no other
// namespace is ever searched to interpret a bare one, because a bare ID that
// happens to exist in two projects would otherwise resolve by luck, and the
// central store holds identical bare epic IDs in different projects. A store
// with no namespace qualifies nothing: qualifyRef("", ref) is ref.
func qualifyRef(ownerNS, ref string) string {
	if ownerNS == "" || ref == "" {
		return ref
	}
	if proj, _ := ParseNamespacedID(ref); proj != "" {
		return ref
	}
	return FormatNamespacedID(ownerNS, ref)
}

// namespaceOf is the namespace half of a qualified ID; "" for a bare one.
func namespaceOf(id string) string {
	ns, _ := ParseNamespacedID(id)
	return ns
}

// namespaceSource is one namespace as it was read for a snapshot: its tickets
// and file skips exactly as listStored yields them, or the reason it yielded
// nothing at all.
type namespaceSource struct {
	name    string
	tickets []*Ticket
	skips   []FileSkip
	// err is why the namespace could not be listed, or why it is expected and
	// has no directory; a namespace that listed fine carries none.
	err string
}

// buildSnapshot assembles the graph from namespaces already read. The reads
// happen under the store lock in central.snapshot; this is pure, so a store
// outside this package's own types can be snapshotted from its listing.
//
// Order of work: qualify every ID and detect duplicates, so every later lookup
// is exact; place each leaf under its parent or stamp why it cannot be; index
// every inbound reference; then derive each epic from its full child set,
// after which the tickets are what every reader is handed.
func buildSnapshot(sources []namespaceSource, crossProject bool) *Snapshot {
	snap := &Snapshot{
		Complete:     true,
		byID:         map[string]*Ticket{},
		children:     map[string][]*Ticket{},
		inbound:      map[string][]InboundRef{},
		epicStored:   map[string]Status{},
		claims:       map[string]int{},
		failed:       map[string]string{},
		crossProject: crossProject,
	}
	for _, src := range sources {
		if src.err != "" {
			snap.failed[src.name] = src.err
			snap.Skips = append(snap.Skips, FileSkip{Project: src.name, Kind: FileSkipNamespace, Error: src.err})
		} else {
			snap.namespaces = append(snap.namespaces, src.name)
		}
		for _, t := range src.tickets {
			t.ID = FormatNamespacedID(src.name, t.ID)
			snap.claims[t.ID]++
			snap.Tickets = append(snap.Tickets, t)
		}
		for _, skip := range src.skips {
			skip.Project = src.name
			snap.Skips = append(snap.Skips, skip)
		}
	}
	for _, t := range snap.Tickets {
		if snap.claims[t.ID] == 1 {
			snap.byID[t.ID] = t
		}
	}
	for _, skip := range snap.Skips {
		if skip.Kind.DegradesEpicStatus() {
			snap.Complete = false
		}
	}

	for _, t := range snap.Tickets {
		ns := namespaceOf(t.ID)
		if t.Parent != "" {
			parentID := qualifyRef(ns, t.Parent)
			snap.inbound[parentID] = append(snap.inbound[parentID], InboundRef{From: t.ID, Kind: InboundParent})
			t.relationshipIssue = snap.parentIssue(ns, t, parentID)
			if t.relationshipIssue == "" {
				snap.children[parentID] = append(snap.children[parentID], t)
			}
		}
		// A claimant of an ID two files hold is stamped as the ambiguous
		// identity it is, over any parent issue, the way leafIssue checks a
		// single read's identity first: the graph answers a reference to the
		// ID with neither file, so neither is offered for automatic execution
		// off the one whose status happens to read ready.
		if snap.claims[t.ID] > 1 {
			t.relationshipIssue = duplicateIssue(t.ID)
		}
		for _, d := range t.Deps {
			depID := qualifyRef(ns, d)
			snap.inbound[depID] = append(snap.inbound[depID], InboundRef{From: t.ID, Kind: InboundDep})
		}
		for _, l := range t.Links {
			linkID := qualifyRef(ns, l)
			snap.inbound[linkID] = append(snap.inbound[linkID], InboundRef{From: t.ID, Kind: InboundLink})
		}
	}

	// Computed against stored fields and assigned afterwards, so a legacy epic
	// under an epic derives the same way whatever order the files were read in.
	type derivation struct {
		epic      *Ticket
		status    Status
		completed time.Time
	}
	var derived []derivation
	for _, t := range snap.Tickets {
		if t.Type != TypeEpic {
			continue
		}
		snap.epicStored[t.ID] = t.Status
		status, completed := deriveEpicFrom(t.Abandoned, snap.children[t.ID], !snap.Complete)
		derived = append(derived, derivation{epic: t, status: status, completed: completed})
	}
	for _, d := range derived {
		d.epic.Status, d.epic.Completed = d.status, d.completed
	}
	return snap
}

// duplicateIssue is the relationship issue every claimant of an ID two files
// hold carries, and the reason claimedStored refuses a reference to it: one
// wording, whichever path read the file.
func duplicateIssue(id string) string {
	return fmt.Sprintf("ticket %s is claimed by more than one file", id)
}

// parentIssue is why t's parent does not make it a child of parentID, or ""
// when it does. The wording is what a consumer shows beside a leaf missing
// from the frontier, so each case names the remedy's target. The checks run
// in the order op.resolveParent, central.leafIssue and the audit run them —
// the cross-project gate before existence — so one file gets one wording
// whichever path read it.
func (s *Snapshot) parentIssue(ns string, t *Ticket, parentID string) string {
	if t.Type == TypeEpic {
		return fmt.Sprintf("epic %s names parent %s, but epics are top level", t.ID, t.Parent)
	}
	if namespaceOf(parentID) != ns && !s.crossProject {
		return fmt.Sprintf("parent %s is in another project, and cross-project parents are not activated (the catalog does not require %s)", parentID, FeatureCrossProjectParents)
	}
	parent, ok := s.byID[parentID]
	if !ok {
		if reason, failed := s.failed[namespaceOf(parentID)]; failed {
			return fmt.Sprintf("parent %s is in namespace %q, which could not be read: %s", parentID, namespaceOf(parentID), reason)
		}
		return fmt.Sprintf("parent %s does not resolve", parentID)
	}
	if parent.Type != TypeEpic {
		return fmt.Sprintf("parent %s is type %s, not an epic", parentID, parent.Type)
	}
	return ""
}

// partialMatches returns the tickets in one namespace whose bare ID contains
// the fragment — the input convenience FileStore.Resolve offers, applied to
// the snapshot so a parent typed as a hash resolves the same way. Never across
// namespaces: a fragment is relative to the namespace it was typed in.
func (s *Snapshot) partialMatches(ns, fragment string) []*Ticket {
	var matches []*Ticket
	for _, t := range s.Tickets {
		tns, bare := ParseNamespacedID(t.ID)
		if tns == ns && strings.Contains(bare, fragment) {
			matches = append(matches, t)
		}
	}
	return matches
}

// groupByProject renders qualified IDs as "project: a, b; project2: c", for a
// refusal that has to say where the affected tickets live.
func groupByProject(ids []string) string {
	groups := map[string][]string{}
	for _, id := range ids {
		ns, bare := ParseNamespacedID(id)
		groups[ns] = append(groups[ns], bare)
	}
	var names []string
	for ns := range groups {
		names = append(names, ns)
	}
	sort.Strings(names)
	var parts []string
	for _, ns := range names {
		sort.Strings(groups[ns])
		label := ns
		if label == "" {
			label = "(no project)"
		}
		parts = append(parts, label+": "+strings.Join(groups[ns], ", "))
	}
	return strings.Join(parts, "; ")
}
