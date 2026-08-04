package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveParent_RejectsNonEpicParentOnCreate(t *testing.T) {
	s := depStore(t, mk("feat-1111", StatusOpen))

	err := s.Create(mkWithParent("child-2222", StatusOpen, "feat-1111"))
	if err == nil {
		t.Fatal("expected a non-epic parent to be rejected on create, got nil")
	}
	if !strings.Contains(err.Error(), "feat-1111") || !strings.Contains(err.Error(), string(TypeFeature)) {
		t.Errorf("error should name the parent and its type, got: %v", err)
	}
}

func TestResolveParent_RejectsNonEpicParentOnUpdate(t *testing.T) {
	s := depStore(t,
		mkEpic("epic-1111", StatusOpen, ""),
		mk("feat-2222", StatusOpen),
		mkWithParent("child-3333", StatusOpen, "epic-1111"),
	)

	child, _ := s.Get("child-3333")
	child.Parent = "feat-2222"
	err := s.Update(child)
	if err == nil {
		t.Fatal("expected a non-epic parent to be rejected on update, got nil")
	}
	if !strings.Contains(err.Error(), "feat-2222") || !strings.Contains(err.Error(), string(TypeFeature)) {
		t.Errorf("error should name the parent and its type, got: %v", err)
	}

	stored, _ := s.Get("child-3333")
	if stored.Parent != "epic-1111" {
		t.Errorf("rejected update still changed the stored parent to %q", stored.Parent)
	}
}

func TestResolveParent_RejectsParentOnEpic(t *testing.T) {
	s := depStore(t, mkEpic("epic-1111", StatusOpen, ""))

	err := s.Create(mkEpic("epic-2222", StatusOpen, "epic-1111"))
	if err == nil {
		t.Fatal("expected an epic with a parent to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "epic-2222") || !strings.Contains(err.Error(), "epic-1111") {
		t.Errorf("error should name the epic and its parent, got: %v", err)
	}
}

func TestResolveParent_RejectsMissingParent(t *testing.T) {
	s := depStore(t)

	err := s.Create(mkWithParent("child-1111", StatusOpen, "gone-9999"))
	if err == nil {
		t.Fatal("expected an unresolvable parent to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "gone-9999") {
		t.Errorf("error should name the parent, got: %v", err)
	}
}

func TestResolveParent_StoresTheResolvedEpicID(t *testing.T) {
	// A parent may be typed in any form tk resolves — full, partial hash, or
	// namespaced under the store's own project — but every reader matches a
	// parent by ID, so what lands on disk has to be the resolved epic.
	s := NewProjectFileStore(t.TempDir(), "proj")
	if err := s.Create(mkEpic("epic-abcd", StatusOpen, "")); err != nil {
		t.Fatal(err)
	}
	forms := map[string]struct{ typed, stored string }{
		"child-1111": {"epic-abcd", "epic-abcd"},
		"child-2222": {"abcd", "epic-abcd"},
		"child-3333": {"proj/epic-abcd", "proj/epic-abcd"},
	}
	for id, form := range forms {
		if err := s.Create(mkWithParent(id, StatusOpen, form.typed)); err != nil {
			t.Errorf("parent %q should resolve to the epic: %v", form.typed, err)
			continue
		}
		stored, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Parent != form.stored {
			t.Errorf("parent typed as %q stored as %q, want the canonical %q", form.typed, stored.Parent, form.stored)
		}
	}
}

func TestResolveParent_LegacyTicketReadsButDoesNotWrite(t *testing.T) {
	// A store written before the rule keeps loading: only writes are refused,
	// and the refusal has to say how to fix the ticket.
	s := depStore(t, mk("feat-1111", StatusOpen))
	writeLegacy(t, s, mkWithParent("child-2222", StatusOpen, "feat-1111"))

	child, err := s.Get("child-2222")
	if err != nil {
		t.Fatalf("a violating ticket must still load: %v", err)
	}
	all, err := s.List()
	if err != nil || len(all) != 2 {
		t.Fatalf("List = %d tickets, %v; want both", len(all), err)
	}

	child.Priority = 0
	err = s.Update(child)
	if err == nil {
		t.Fatal("expected an edit of a violating ticket to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "clear it") {
		t.Errorf("error should tell the user how to fix the parent, got: %v", err)
	}
}

func TestAuditParentsReportsEachViolationClass(t *testing.T) {
	s := NewProjectFileStore(t.TempDir(), "proj")
	if err := s.Create(mkEpic("epic-1111", StatusOpen, "")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(mkWithParent("good-2222", StatusOpen, "epic-1111")); err != nil {
		t.Fatal(err)
	}
	writeLegacy(t, s, mkWithParent("notepic-3333", StatusOpen, "good-2222"))
	writeLegacy(t, s, mkWithParent("missing-4444", StatusOpen, "gone-9999"))
	writeLegacy(t, s, mkEpic("subepic-5555", StatusOpen, "epic-1111"))
	writeLegacy(t, s, mkWithParent("cyc-6666", StatusOpen, "cyc-7777"))
	writeLegacy(t, s, mkWithParent("cyc-7777", StatusOpen, "cyc-6666"))

	audit, err := AuditParents(s)
	if err != nil {
		t.Fatalf("AuditParents: %v", err)
	}

	got := map[string]ParentViolationKind{}
	for _, v := range audit.Violations {
		got[v.ID] = v.Kind
	}
	want := map[string]ParentViolationKind{
		"notepic-3333": ViolationParentNotEpic,
		"missing-4444": ViolationParentMissing,
		"subepic-5555": ViolationEpicHasParent,
		"cyc-6666":     ViolationParentCycle,
		"cyc-7777":     ViolationParentCycle,
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("audit reported %s as %q, want %q", id, got[id], kind)
		}
	}
	if _, ok := got["good-2222"]; ok {
		t.Errorf("audit reported the valid child good-2222")
	}
	if len(audit.Violations) != len(want) {
		t.Errorf("audit reported %d violations, want %d: %v", len(audit.Violations), len(want), got)
	}
}

func TestAuditParentsMatchesEnforcementAcrossProjects(t *testing.T) {
	// Audit resolves each parent inside the project that owns the ticket, the
	// same way the write path does. Resolving through MultiStore.Get instead
	// would clear a cross-project parent no write accepts, clear a bare parent
	// that only exists elsewhere, and flag a bare parent that resolves fine in
	// its own project but is ambiguous across the store.
	ms, root := testMultiStore(t, "alpha", "beta")
	alpha := NewProjectFileStore(filepath.Join(root, "alpha"), "alpha")
	beta := NewProjectFileStore(filepath.Join(root, "beta"), "beta")

	for _, s := range []*FileStore{alpha, beta} {
		if err := s.Create(mkEpic("epic-1111", StatusOpen, "")); err != nil {
			t.Fatal(err)
		}
	}
	if err := beta.Create(mkEpic("epic-7777", StatusOpen, "")); err != nil {
		t.Fatal(err)
	}
	// Resolves through MultiStore.Get, but every write to it is refused.
	writeLegacy(t, alpha, mkWithParent("cross-2222", StatusOpen, "beta/epic-7777"))
	// Bare, and only exists in the other project.
	writeLegacy(t, alpha, mkWithParent("elsewhere-3333", StatusOpen, "epic-7777"))
	// Bare and ambiguous across the store, but unambiguous in alpha — writes
	// accept it, so the audit must not name it.
	if err := alpha.Create(mkWithParent("partial-4444", StatusOpen, "1111")); err != nil {
		t.Fatal(err)
	}

	audit, err := AuditParents(ms)
	if err != nil {
		t.Fatalf("AuditParents: %v", err)
	}

	got := map[string]ParentViolationKind{}
	for _, v := range audit.Violations {
		got[v.ID] = v.Kind
	}
	want := map[string]ParentViolationKind{
		"alpha/cross-2222":     ViolationParentCrossProject,
		"alpha/elsewhere-3333": ViolationParentMissing,
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("audit reported %s as %q, want %q", id, got[id], kind)
		}
	}
	if len(audit.Violations) != len(want) {
		t.Errorf("audit reported %d violations, want %d: %v", len(audit.Violations), len(want), got)
	}
	if len(audit.Skipped) != 0 {
		t.Errorf("audit skipped %v, want every project read", audit.Skipped)
	}

	// The audit's verdict and the write path's verdict have to agree on each.
	for _, tk := range []*Ticket{
		mkWithParent("cross-2222", StatusOpen, "beta/epic-7777"),
		mkWithParent("elsewhere-3333", StatusOpen, "epic-7777"),
	} {
		if err := ResolveParent(alpha, tk); err == nil {
			t.Errorf("audit flagged %s but the write path accepts it", tk.ID)
		}
	}
	if err := ResolveParent(alpha, mkWithParent("partial-4444", StatusOpen, "1111")); err != nil {
		t.Errorf("audit cleared partial-4444 but the write path refuses it: %v", err)
	}
}

func TestAuditParentsDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	s := NewProjectFileStore(dir, "proj")
	if err := s.Create(mk("feat-1111", StatusOpen)); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(mkEpic("epic-abcd", StatusOpen, "")); err != nil {
		t.Fatal(err)
	}
	writeLegacy(t, s, mkWithParent("child-2222", StatusOpen, "feat-1111"))
	// A partial parent the audit clears. The write path would rewrite it to the
	// epic's full ID; the audit reads the same way but rewrites nothing.
	writeLegacy(t, s, mkWithParent("partial-3333", StatusOpen, "abcd"))

	before := map[string]string{}
	for _, name := range []string{"feat-1111.md", "epic-abcd.md", "child-2222.md", "partial-3333.md"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = string(data)
	}

	if _, err := AuditParents(s); err != nil {
		t.Fatalf("AuditParents: %v", err)
	}

	for name, want := range before {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("audit rewrote %s", name)
		}
	}
}

func TestAuditParentsNamesUnreadableProject(t *testing.T) {
	// A project the audit cannot enumerate has to be named in the result. Left
	// out, it would drop silently and the report would call a store clean that
	// it never fully read — the one direction this audit must not fail in.
	ms, root := testMultiStore(t, "alpha", "beta")
	alpha := NewProjectFileStore(filepath.Join(root, "alpha"), "alpha")
	writeLegacy(t, alpha, mkWithParent("bad-1111", StatusOpen, "gone-9999"))

	unreadable := filepath.Join(root, "beta")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o755) })

	audit, err := AuditParents(ms)
	if err != nil {
		t.Fatalf("AuditParents: %v", err)
	}
	if len(audit.Violations) != 1 {
		t.Errorf("violations = %v, want the one in the readable project", audit.Violations)
	}
	if len(audit.Skipped) != 1 || audit.Skipped[0].Project != "beta" {
		t.Fatalf("skipped = %v, want beta reported as unreadable", audit.Skipped)
	}
	if audit.Skipped[0].Error == "" {
		t.Error("a skipped project should carry the reason it was skipped")
	}
}
