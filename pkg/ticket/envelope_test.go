package ticket

import (
	"strings"
	"testing"
	"unicode"
)

// closeTag and openTag build the envelope markup the tests feed in, from the
// same pieces envelope.go matches with — a test file carrying a terminator
// verbatim corrupts the tool call of any agent that quotes it, which is the
// failure under test.
func closeTag(name string) string { return "</" + name + ">" }

func openTag(name, attrs string) string {
	if attrs == "" {
		return "<" + name + ">"
	}
	return "<" + name + " " + attrs + ">"
}

func TestEnvelopeFragment(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name: "absorbed acceptance and call terminator",
			value: "The real description text.\n" + closeTag("description") + "\n" +
				openTag("parameter", `name="acceptance"`) + "\n- criteria\n" +
				closeTag("parameter") + "\n" + closeTag("invoke"),
			want: true,
		},
		{
			name:  "namespaced terminator",
			value: "The real description text.\n" + closeTag("antml:invoke"),
			want:  true,
		},
		{
			name:  "terminator with trailing whitespace",
			value: "Text.\n" + closeTag("function_calls") + "\n\n  ",
			want:  true,
		},
		{
			name:  "truncated mid envelope",
			value: "Text.\n" + openTag("antml:parameter", `name="acceptance"`),
			want:  true,
		},
		{
			name:  "terminator with non-ASCII trailing space",
			value: "Text.\n" + closeTag("invoke") + "\v\u00a0",
			want:  true,
		},
		{
			name:  "truncated inside an opening tag",
			value: "Text.\n" + "<" + "antml:parameter" + ` name="acceptance"`,
			want:  true,
		},
		{
			name:  "backtick-quoted opening tag mid-sentence",
			value: "Text.\nThe corruption leaves an opening `" + openTag("parameter", `name="acceptance"`) + "` at the end.",
			want:  false,
		},
		{
			name:  "bare opening tag followed by prose",
			value: "Text.\nAn RSS feed's " + openTag("description", "") + " element is not this.",
			want:  false,
		},
		{
			name:  "opening tag closed on the same line",
			value: "Text.\nA balanced " + openTag("parameter", "") + "x" + closeTag("parameter") + " inside prose.",
			want:  false,
		},
		{
			name:  "mentioned mid body",
			value: "The description ended with " + closeTag("invoke") + ", which is the bug.\nThe cause is client-side.",
			want:  false,
		},
		{
			name:  "backtick-quoted trailing mention",
			value: "The tail was an unmistakable `" + closeTag("invoke") + "`",
			want:  false,
		},
		{
			name:  "ordinary prose",
			value: "A description whose tail is an unmistakable truncated tool-call envelope.",
			want:  false,
		},
		{
			name:  "empty",
			value: "",
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tail, ok := EnvelopeFragment(tt.value)
			if ok != tt.want {
				t.Fatalf("EnvelopeFragment(%q) = %v, want %v", tt.value, ok, tt.want)
			}
			if ok && !strings.HasSuffix(strings.TrimRightFunc(tt.value, unicode.IsSpace), tail) {
				t.Errorf("tail %q is not the end of the value", tail)
			}
			if !ok && tail != "" {
				t.Errorf("tail = %q, want empty for a clean value", tail)
			}
		})
	}
}

func TestEnvelopeFragmentTailIsBounded(t *testing.T) {
	value := strings.Repeat("padding text. ", 100) + closeTag("invoke")
	tail, ok := EnvelopeFragment(value)
	if !ok {
		t.Fatal("EnvelopeFragment did not flag a terminator after a long body")
	}
	if len(tail) > envelopeTailBytes {
		t.Errorf("tail is %d bytes, want at most %d", len(tail), envelopeTailBytes)
	}
}
