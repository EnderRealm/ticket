package ticket

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// envelopeElements names the tool-call envelope elements a corrupted argument
// value can carry. The literal tag strings are built from these at match time
// rather than written out: a terminator spelled in full in tk's own source
// corrupts the tool call of any agent that quotes the file, which is the very
// failure this check exists to catch. Keeping the element list in one place is
// the second reason — the bare and namespaced spellings expand from it.
var envelopeElements = []string{"invoke", "parameter", "description", "function_calls"}

// envelopeNamespace is the prefix real harnesses emit on these elements. Both
// spellings are recognised, since the bare form is what a ticket describing the
// corruption tends to quote.
const envelopeNamespace = "antml:"

// envelopeTags is envelopeElements expanded to both spellings, once.
var envelopeTags = func() []string {
	tags := make([]string, 0, 2*len(envelopeElements))
	for _, e := range envelopeElements {
		tags = append(tags, e, envelopeNamespace+e)
	}
	return tags
}()

// envelopeTailBytes bounds the excerpt returned for an error message.
const envelopeTailBytes = 200

// EnvelopeFragment reports whether s ends in the remains of a tool-call
// envelope — the shape a free-text argument takes when the caller closed its
// parameter with the wrong tag and the tool-call parser went on consuming to
// the end of the call, absorbing every field after it into this one value. tail
// is the last bytes of s, for an error message that lets the caller see what to
// resend.
//
// The match is tail-anchored, not a containment check. A ticket may legitimately
// discuss these tokens in its body — the ticket that asked for this check does —
// and containment would refuse it. The corruption always leaves the envelope's
// terminator at the end of the value, because the parser consumed to the end of
// the call, so anchoring at the tail separates the two cases without a
// heuristic. Prose quoting a tag ends with the quoting, typically a backtick,
// and passes.
//
// Known gap, and the deliberate cost of that: a truncation that stops partway
// through the absorbed block — an opening tag followed by half a line of the
// criteria it swallowed — leaves prose after the tag and reads as a mention.
// Nothing distinguishes the two at the tail, and the false-positive rule
// decides which way the tie goes. Every observed case carried the call
// terminator, so this is not exhaustive detection and is not meant as one.
func EnvelopeFragment(s string) (tail string, ok bool) {
	// Every space rune, not the four ASCII ones: the audit sees values that
	// BodySections already ran through TrimSpace while the MCP path sees the raw
	// argument, so a terminator followed by a vertical tab or an NBSP would
	// otherwise pass on write and be reported on read of the same stored text.
	trimmed := strings.TrimRightFunc(s, unicode.IsSpace)
	if trimmed == "" {
		return "", false
	}
	for _, tag := range envelopeTags {
		if strings.HasSuffix(trimmed, "</"+tag+">") {
			return envelopeTail(trimmed), true
		}
	}
	// The truncated-mid-envelope variant: the value was cut inside the absorbed
	// call, so an opening tag is what stands at its end. Anchored the same way —
	// the tag has to be the last one the value opens and has to run to the end of
	// it, either cut before its own '>' or closing on the final byte. A tag that
	// opens and is followed by more text is prose mentioning one, which is how a
	// ticket documenting this failure writes it.
	open := strings.LastIndex(trimmed, "<")
	if open < 0 {
		return "", false
	}
	rest := trimmed[open+1:]
	for _, tag := range envelopeTags {
		if !strings.HasPrefix(rest, tag) {
			continue
		}
		// A tag name ends at '>' or at its attributes; anything else is a longer
		// word that merely starts with one of ours.
		after := rest[len(tag):]
		if after != "" && !strings.HasPrefix(after, ">") && !strings.HasPrefix(after, " ") {
			continue
		}
		if idx := strings.Index(after, ">"); idx >= 0 && idx != len(after)-1 {
			continue
		}
		return envelopeTail(trimmed), true
	}
	return "", false
}

// envelopeTail returns the last envelopeTailBytes of s, cut forward to a rune
// boundary so the excerpt is valid UTF-8 when it is quoted back at the caller.
func envelopeTail(s string) string {
	if len(s) <= envelopeTailBytes {
		return s
	}
	tail := s[len(s)-envelopeTailBytes:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return tail
}
