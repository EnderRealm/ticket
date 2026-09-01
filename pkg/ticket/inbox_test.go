package ticket

import (
	"testing"
	"time"
)

func TestNextAction_Ready(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Status: StatusReady, Type: TypeFeature, Priority: 1,
		Created: time.Now(),
	}
	item := NextAction(tk)
	if item.Action != ActionWork {
		t.Errorf("NextAction(ready) = %s, want work", item.Action)
	}
}

func TestNextAction_Open(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Status: StatusOpen, Type: TypeFeature, Priority: 1,
		Created: time.Now(),
	}
	item := NextAction(tk)
	if item.Action != ActionWork {
		t.Errorf("NextAction(open) = %s, want work", item.Action)
	}
}

func TestNextAction_Done(t *testing.T) {
	tk := &Ticket{ID: "t-1", Status: StatusDone, Type: TypeFeature, Created: time.Now()}
	item := NextAction(tk)
	if item.Action != ActionReady {
		t.Errorf("NextAction(done) = %s, want ready", item.Action)
	}
}

func TestNextAction_Backlog(t *testing.T) {
	tk := &Ticket{ID: "t-1", Status: StatusBacklog, Type: TypeFeature, Created: time.Now()}
	item := NextAction(tk)
	if item.Action != ActionReady {
		t.Errorf("NextAction(backlog) = %s, want ready", item.Action)
	}
	if item.Detail != "no action needed" {
		t.Errorf("NextAction(backlog) detail = %q, want %q", item.Detail, "no action needed")
	}
}

func TestNextAction_Parked(t *testing.T) {
	// A parked run's question outranks the status: both an open and a ready
	// ticket carrying one need a human before work resumes.
	for _, status := range []Status{StatusOpen, StatusReady} {
		tk := &Ticket{
			ID: "t-1", Status: status, Type: TypeFeature, Priority: 1,
			Created: time.Now(), Extra: map[string]string{QuestionField: "Which store wins?"},
		}
		item := NextAction(tk)
		if item.Action != ActionBlocked {
			t.Errorf("NextAction(%s + question) = %s, want blocked", status, item.Action)
		}
		if item.Detail != "Which store wins?" {
			t.Errorf("NextAction(%s + question) detail = %q, want the question", status, item.Detail)
		}
	}
}

func TestNextAction_EmptyQuestionUnchanged(t *testing.T) {
	// An empty value counts as absent — an extra field is not a park by itself.
	// Whitespace is empty too: parking on a blank question blocks the ticket
	// with nothing for the human to answer.
	for _, q := range []string{"", "   ", "\t\n"} {
		tk := &Ticket{
			ID: "t-1", Status: StatusOpen, Type: TypeFeature, Priority: 1,
			Created: time.Now(), Extra: map[string]string{QuestionField: q, "env": "prod"},
		}
		item := NextAction(tk)
		if item.Action != ActionWork {
			t.Errorf("NextAction(open, question %q) = %s, want work", q, item.Action)
		}
		if item.Detail != "in progress" {
			t.Errorf("NextAction(open, question %q) detail = %q, want %q", q, item.Detail, "in progress")
		}
	}
}

func TestInbox_KeepsParkedTicket(t *testing.T) {
	store := NewFileStore(t.TempDir())

	tk := &Ticket{
		ID: "t-parked", Status: StatusOpen, Type: TypeFeature, Priority: 1,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Parked", Body: "\n",
		Extra: map[string]string{QuestionField: "Which store wins?"},
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	items, err := Inbox(store)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Inbox returned %d items, want 1", len(items))
	}
	if items[0].Action != ActionBlocked {
		t.Errorf("inbox item action = %s, want blocked", items[0].Action)
	}
	if items[0].Detail != "Which store wins?" {
		t.Errorf("inbox item detail = %q, want the question", items[0].Detail)
	}
}

func TestInbox_ExcludesBacklog(t *testing.T) {
	store := NewFileStore(t.TempDir())

	// Backlog ticket — should NOT appear in inbox.
	t1 := &Ticket{
		ID: "t-backlog", Status: StatusBacklog, Type: TypeFeature, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Backlog idea", Body: "\n",
	}
	// Ready ticket — should appear.
	t2 := &Ticket{
		ID: "t-ready", Status: StatusReady, Type: TypeFeature, Priority: 1,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Ready", Body: "\n",
	}

	for _, tk := range []*Ticket{t1, t2} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	items, err := Inbox(store)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Inbox returned %d items, want 1", len(items))
	}
	if items[0].Ticket.ID != "t-ready" {
		t.Errorf("inbox item = %s, want t-ready", items[0].Ticket.ID)
	}
}

func TestInbox_FiltersActionableStatuses(t *testing.T) {
	store := NewFileStore(t.TempDir())

	// Ready ticket — should appear in inbox.
	t1 := &Ticket{
		ID: "t-1", Status: StatusReady, Type: TypeFeature, Priority: 1,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Feature", Body: "\n",
	}
	// Open ticket — should appear.
	t2 := &Ticket{
		ID: "t-2", Status: StatusOpen, Type: TypeBug, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Bug", Body: "\n",
	}
	// Done ticket — should NOT appear.
	t3 := &Ticket{
		ID: "t-3", Status: StatusDone, Type: TypeFeature, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Done", Body: "\n",
	}

	for _, tk := range []*Ticket{t1, t2, t3} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	items, err := Inbox(store)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Inbox returned %d items, want 2", len(items))
	}

	// Should be sorted: P0 open first, then P1 ready.
	if items[0].Ticket.ID != "t-2" {
		t.Errorf("first inbox item = %s, want t-2 (P0)", items[0].Ticket.ID)
	}
	if items[1].Ticket.ID != "t-1" {
		t.Errorf("second inbox item = %s, want t-1 (P1)", items[1].Ticket.ID)
	}
}

func TestProjects(t *testing.T) {
	store := NewFileStore(t.TempDir())

	epic := &Ticket{
		ID: "t-epic", Status: StatusBacklog, Type: TypeEpic, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Epic", Body: "\n",
	}
	child1 := &Ticket{
		ID: "t-c1", Status: StatusDone, Type: TypeFeature, Priority: 1,
		Parent: "t-epic", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "Done child", Body: "\n",
	}
	child2 := &Ticket{
		ID: "t-c2", Status: StatusOpen, Type: TypeFeature, Priority: 1,
		Parent: "t-epic", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "WIP child", Body: "\n",
	}

	for _, tk := range []*Ticket{epic, child1, child2} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := Projects(store)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Projects returned %d, want 1", len(projects))
	}

	p := projects[0]
	if p.Total != 2 {
		t.Errorf("Total = %d, want 2", p.Total)
	}
	if p.CompletionPct != 50 {
		t.Errorf("CompletionPct = %f, want 50", p.CompletionPct)
	}
	if p.StatusBreakdown[StatusDone] != 1 {
		t.Errorf("StatusBreakdown[done] = %d, want 1", p.StatusBreakdown[StatusDone])
	}
}

func TestProjects_KeepsParkedChildInNextActions(t *testing.T) {
	// A parked child is blocked rather than work; dropping it would hide the
	// one action the epic most needs from the human.
	store := NewFileStore(t.TempDir())

	epic := &Ticket{
		ID: "t-epic", Status: StatusBacklog, Type: TypeEpic, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Epic", Body: "\n",
	}
	child := &Ticket{
		ID: "t-c1", Status: StatusOpen, Type: TypeFeature, Priority: 1,
		Parent: "t-epic", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "Parked child", Body: "\n",
		Extra: map[string]string{QuestionField: "Which store wins?"},
	}
	// A question left behind on a finished child is stale: it still counts as
	// done and must not read as an action the epic is waiting on.
	finished := &Ticket{
		ID: "t-c2", Status: StatusDone, Type: TypeFeature, Priority: 1,
		Parent: "t-epic", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "Finished child", Body: "\n",
		Extra: map[string]string{QuestionField: "Which store won?"},
	}

	for _, tk := range []*Ticket{epic, child, finished} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := Projects(store)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Projects returned %d, want 1", len(projects))
	}

	p := projects[0]
	if len(p.NextActions) != 1 {
		t.Fatalf("NextActions returned %d, want 1", len(p.NextActions))
	}
	if p.NextActions[0].Ticket.ID != "t-c1" {
		t.Errorf("NextActions[0] ticket = %s, want t-c1", p.NextActions[0].Ticket.ID)
	}
	if p.NextActions[0].Action != ActionBlocked {
		t.Errorf("NextActions[0] action = %s, want blocked", p.NextActions[0].Action)
	}
	if p.NextActions[0].Detail != "Which store wins?" {
		t.Errorf("NextActions[0] detail = %q, want the question", p.NextActions[0].Detail)
	}
	if p.StatusBreakdown[StatusDone] != 1 {
		t.Errorf("StatusBreakdown[done] = %d, want 1", p.StatusBreakdown[StatusDone])
	}
	if p.CompletionPct != 50 {
		t.Errorf("CompletionPct = %v, want 50", p.CompletionPct)
	}
}

func TestProjects_CountsBareAndNamespacedChildren(t *testing.T) {
	// The central store records a child's parent namespaced; tickets written
	// before the namespacing rollout record it bare. Both roll up to the epic.
	store := NewProjectFileStore(t.TempDir(), "proj")

	epic := &Ticket{
		ID: "ns-epic-0001", Status: StatusBacklog, Type: TypeEpic, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Epic", Body: "\n",
	}
	bare := &Ticket{
		ID: "ns-bare-0002", Status: StatusDone, Type: TypeFeature, Priority: 1,
		Parent: "ns-epic-0001", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "Bare child", Body: "\n",
	}
	namespaced := &Ticket{
		ID: "ns-child-0003", Status: StatusOpen, Type: TypeFeature, Priority: 1,
		Parent: "proj/ns-epic-0001", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "Namespaced child", Body: "\n",
	}

	for _, tk := range []*Ticket{epic, bare, namespaced} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := Projects(store)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Projects returned %d, want 1", len(projects))
	}

	p := projects[0]
	if p.Total != 2 {
		t.Errorf("Total = %d, want 2", p.Total)
	}
	if p.StatusBreakdown[StatusDone] != 1 {
		t.Errorf("StatusBreakdown[done] = %d, want 1", p.StatusBreakdown[StatusDone])
	}
	if p.StatusBreakdown[StatusOpen] != 1 {
		t.Errorf("StatusBreakdown[open] = %d, want 1", p.StatusBreakdown[StatusOpen])
	}
}

func TestProjects_SplitsSameBareIDEpicsButPoolsTheirChildren(t *testing.T) {
	// MultiStore namespaces IDs, so two projects can hold the same bare epic
	// ID. Each keeps its own summary rather than collapsing into one, but the
	// child index is bare-keyed, so both epics pick up both children.
	ms, _ := testMultiStore(t, "alpha", "beta")

	epicA := sampleTicket("alpha/dup-epic-0001")
	epicA.Type = TypeEpic
	epicA.Status = StatusBacklog
	kidA := sampleTicket("alpha/kid-0002")
	kidA.Parent = "alpha/dup-epic-0001"
	epicB := sampleTicket("beta/dup-epic-0001")
	epicB.Type = TypeEpic
	epicB.Status = StatusBacklog
	kidB := sampleTicket("beta/kid-0003")
	kidB.Parent = "beta/dup-epic-0001"

	for _, tk := range []*Ticket{epicA, kidA, epicB, kidB} {
		if err := ms.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := Projects(ms)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("Projects returned %d, want 2 — one summary per epic", len(projects))
	}

	byID := make(map[string]ProjectSummary)
	for _, p := range projects {
		byID[p.Epic.ID] = p
	}

	// Each epic carries its own child. The child index is bare-keyed, so a
	// same-bare-ID sibling's child rolls up here too — namespace-blind, like
	// SameTicketID everywhere else. Total pins that tradeoff: scoping the index
	// by project would drop these back to one child apiece.
	for epicID, kidID := range map[string]string{
		"alpha/dup-epic-0001": "alpha/kid-0002",
		"beta/dup-epic-0001":  "beta/kid-0003",
	} {
		p, ok := byID[epicID]
		if !ok {
			t.Errorf("epic %s missing from summaries", epicID)
			continue
		}
		found := false
		for _, action := range p.NextActions {
			if action.Ticket.ID == kidID {
				found = true
			}
		}
		if !found {
			t.Errorf("epic %s: child %s missing from next actions", epicID, kidID)
		}
		if p.Total != 2 {
			t.Errorf("epic %s: Total = %d, want 2 — both same-bare-ID epics pool both children", epicID, p.Total)
		}
	}
}
