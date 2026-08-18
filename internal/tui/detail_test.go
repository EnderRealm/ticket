package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
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
	out := strings.Join(newDetailModel(tk, 100, 30).lines, "\n")

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
