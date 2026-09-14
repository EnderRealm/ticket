package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/charmbracelet/x/ansi"
)

func TestDetailSanitizesStoredContent(t *testing.T) {
	tk := &ticket.Ticket{
		ID:          "test-abcd",
		Title:       "Déjà 日本語 \x1b[2Jclear \u202erepaint",
		Body:        "Description \x1b[31mred\x1b[0m \u2066hidden\u2069\n",
		Status:      ticket.StatusOpen,
		Type:        ticket.TypeFeature,
		Priority:    2,
		Created:     time.Now(),
		ExternalRef: "external\x1b[2J",
		Notes:       []ticket.Note{{Text: "note \u202espoof"}},
	}
	out := strings.Join(newDetailModel(tk, tk.ID, nil, 100, 30).lines, "\n")

	for _, r := range []rune{'\x1b', '\u202e', '\u2066', '\u2069'} {
		if strings.ContainsRune(out, r) {
			t.Errorf("detail contains control or format character %U:\n%q", r, out)
		}
	}
	for _, want := range []string{"Déjà", "日本語", "Description", "note"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%q", want, out)
		}
	}
}

func TestDetailDeps(t *testing.T) {
	t.Run("deep chain fits a narrow terminal", func(t *testing.T) {
		var tickets []*ticket.Ticket
		for i := 0; i < 12; i++ {
			tk := &ticket.Ticket{ID: fmt.Sprintf("chain-%04d", i), Title: "日本語 dependency with a long title", Status: ticket.StatusOpen, Type: ticket.TypeFeature}
			if i < 11 {
				tk.Deps = []string{fmt.Sprintf("chain-%04d", i+1)}
			}
			tickets = append(tickets, tk)
		}
		listed, snap := boardFixture(t, "proj", tickets...)
		for _, width := range []int{36, 20} {
			lines := newDetailModel(boardTicket(t, listed, "chain-0000"), "proj/chain-0000", snap, width, 30).lines
			inDeps := false
			for _, line := range lines {
				if strings.Contains(ansi.Strip(line), "## Dependencies") {
					inDeps = true
					continue
				}
				if inDeps && ansi.StringWidth(line) > width {
					t.Errorf("dependency line exceeds %d columns: %q", width, line)
				}
			}
		}
	})

	t.Run("layered graph has bounded output", func(t *testing.T) {
		subject := &ticket.Ticket{ID: "subject-0001", Title: "Subject", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"a-0000", "b-0000", "middle-0030"}}
		tickets := []*ticket.Ticket{subject}
		for layer := 0; layer < 30; layer++ {
			for _, prefix := range []string{"a", "b"} {
				tk := &ticket.Ticket{ID: fmt.Sprintf("%s-%04d", prefix, layer), Title: "Shared dependency", Status: ticket.StatusOpen, Type: ticket.TypeFeature}
				if layer < 29 {
					tk.Deps = []string{fmt.Sprintf("a-%04d", layer+1), fmt.Sprintf("b-%04d", layer+1)}
				}
				tickets = append(tickets, tk)
			}
		}
		tickets = append(tickets,
			&ticket.Ticket{ID: "middle-0030", Title: "Later branch", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"leaf-0031"}},
			&ticket.Ticket{ID: "leaf-0031", Title: "Unique blocker", Status: ticket.StatusReady, Type: ticket.TypeFeature},
		)
		listed, snap := boardFixture(t, "proj", tickets...)
		out := ansi.Strip(strings.Join(newDetailModel(boardTicket(t, listed, subject.ID), "proj/subject-0001", snap, 100, 30).lines, "\n"))
		nodes := snap.DependencyTree("proj/subject-0001")
		if len(nodes) > 121 {
			t.Errorf("layered graph produced %d nodes, want at most one per edge", len(nodes))
		}
		for _, id := range subject.Deps {
			if !strings.Contains(out, "\n  ✗ "+id+" [open]") {
				t.Errorf("direct dependency %s was lost", id)
			}
		}
		if !strings.Contains(out, "    ✗ leaf-0031 [ready] Unique blocker ← root blocker") {
			t.Error("later independent branch lost its unique root blocker")
		}
		for _, tk := range tickets[1:] {
			if !strings.Contains(out, tk.ID) {
				t.Errorf("dependency %s was lost", tk.ID)
			}
		}
	})

	t.Run("statuses, depth, and root blocker", func(t *testing.T) {
		listed, snap := boardFixture(t, "proj",
			&ticket.Ticket{ID: "subject-0001", Title: "Subject", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"done-0002", "closed-0003", "middle-0004"}},
			&ticket.Ticket{ID: "done-0002", Title: "Done dependency", Status: ticket.StatusDone, Type: ticket.TypeFeature},
			&ticket.Ticket{ID: "closed-0003", Title: "Closed dependency", Status: ticket.StatusClosed, Type: ticket.TypeFeature},
			&ticket.Ticket{ID: "middle-0004", Title: "Middle dependency", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"leaf-0005"}},
			&ticket.Ticket{ID: "leaf-0005", Title: "Leaf dependency", Status: ticket.StatusReady, Type: ticket.TypeFeature},
		)
		subject := boardTicket(t, listed, "subject-0001")
		out := ansi.Strip(strings.Join(newDetailModel(subject, "proj/subject-0001", snap, 100, 30).lines, "\n"))

		for _, want := range []string{
			"## Dependencies",
			"  ✓ done-0002 [done] Done dependency",
			"  ✓ closed-0003 [closed] Closed dependency",
			"  ✗ middle-0004 [open] Middle dependency",
			"    ✗ leaf-0005 [ready] Leaf dependency ← root blocker",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("detail missing %q:\n%s", want, out)
			}
		}

		narrow := ansi.Strip(strings.Join(newDetailModel(subject, "proj/subject-0001", snap, 36, 30).lines, "\n"))
		narrowLines := strings.Split(narrow, "\n")
		for i, line := range narrowLines {
			if !strings.Contains(line, "leaf-0005") || i+1 >= len(narrowLines) {
				continue
			}
			if !strings.HasPrefix(narrowLines[i+1], "    ") {
				t.Errorf("wrapped dependency lost its depth indentation:\n%s", narrow)
			}
		}
	})

	t.Run("missing dependency and cycle", func(t *testing.T) {
		listed, snap := boardFixture(t, "proj",
			&ticket.Ticket{ID: "subject-0001", Title: "Subject", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"missing-9999", "cycle-a-0002"}},
			&ticket.Ticket{ID: "cycle-a-0002", Title: "Cycle A", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"cycle-b-0003"}},
			&ticket.Ticket{ID: "cycle-b-0003", Title: "Cycle B", Status: ticket.StatusReady, Type: ticket.TypeFeature, Deps: []string{"cycle-a-0002"}},
		)
		subject := boardTicket(t, listed, "subject-0001")
		out := ansi.Strip(strings.Join(newDetailModel(subject, "proj/subject-0001", snap, 100, 30).lines, "\n"))

		if want := "  ✗ missing-9999 [unknown] (not found) ← root blocker"; !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
		for _, id := range []string{"cycle-a-0002", "cycle-b-0003"} {
			if got := strings.Count(out, id); got != 1 {
				t.Errorf("detail contains %s %d times, want once:\n%s", id, got, out)
			}
		}
	})

	t.Run("shared dependencies render beneath each parent", func(t *testing.T) {
		listed, snap := boardFixture(t, "proj",
			&ticket.Ticket{ID: "subject-0001", Title: "Subject", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"a-0002", "b-0003"}},
			&ticket.Ticket{ID: "a-0002", Title: "Dependency A", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"b-0003"}},
			&ticket.Ticket{ID: "b-0003", Title: "Dependency B", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"leaf-0004"}},
			&ticket.Ticket{ID: "leaf-0004", Title: "Shared leaf", Status: ticket.StatusReady, Type: ticket.TypeFeature},
		)
		subject := boardTicket(t, listed, "subject-0001")
		out := ansi.Strip(strings.Join(newDetailModel(subject, "proj/subject-0001", snap, 100, 30).lines, "\n"))

		if got := strings.Count(out, "b-0003"); got != 2 {
			t.Errorf("detail contains b-0003 %d times, want once beneath each parent:\n%s", got, out)
		}
		if got := strings.Count(out, "leaf-0004"); got != 1 {
			t.Errorf("shared subtree expanded %d times, want once:\n%s", got, out)
		}
		for _, want := range []string{
			"    ✗ b-0003 [open] Dependency B",
			"      ✗ leaf-0004 [ready] Shared leaf ← root blocker",
			"  ✗ b-0003 [open] Dependency B (dependencies shown above)",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("detail missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("stored status is sanitized", func(t *testing.T) {
		listed, snap := boardFixture(t, "proj",
			&ticket.Ticket{ID: "subject-0001", Title: "Subject", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Deps: []string{"dep-0002"}},
			&ticket.Ticket{ID: "dep-0002", Title: "Dependency", Status: ticket.Status("open\x1b[2J\u202erepaint"), Type: ticket.TypeFeature},
		)
		subject := boardTicket(t, listed, "subject-0001")
		out := strings.Join(newDetailModel(subject, "proj/subject-0001", snap, 100, 30).lines, "\n")

		for _, r := range []rune{'\x1b', '\u202e'} {
			if strings.ContainsRune(out, r) {
				t.Errorf("dependency detail contains control or format character %U:\n%q", r, out)
			}
		}
	})
}
