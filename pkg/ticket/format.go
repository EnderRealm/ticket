package ticket

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// legacyStageToStatus maps old pipeline stage values to the new status model.
var legacyStageToStatus = map[string]Status{
	"backlog":       StatusBacklog,
	"triage":        StatusReady,
	"spec":          StatusReady,
	"design":        StatusReady,
	"design-review": StatusReady,
	"implement":     StatusOpen,
	"code-review":   StatusOpen,
	"test":          StatusOpen,
	"verify":        StatusOpen,
	"done":          StatusDone,
}

// legacyStatusToStatus maps old status field values to the new model.
var legacyStatusToStatus = map[string]Status{
	"open":          StatusOpen,
	"in_progress":   StatusOpen,
	"needs_testing": StatusOpen,
	"closed":        StatusDone,
}

// Parse reads a ticket from markdown with YAML frontmatter.
func Parse(r io.Reader) (*Ticket, error) {
	front, body, err := splitFrontmatter(r)
	if err != nil {
		return nil, err
	}

	// A type mismatch on a typed field is not fatal, as long as what it cost stays
	// inside this one ticket. yaml.v3 collects such mismatches into a TypeError and
	// decodes every other field regardless, so `abandoned: maybe` or
	// `priority: high` costs that one field rather than the whole ticket — which is
	// what the store needs, because a ticket that fails to parse is a ticket absent
	// from every listing and absent from its epic's derivation. The central store
	// is a git repo other machines push into, so a value tk would never have
	// written can arrive at any time; losing a local field to it is recoverable and
	// losing the ticket is not. Everything else — broken YAML, frontmatter that
	// never closes — still fails: there is no partial reading of a document the
	// parser could not structure.
	//
	// That split is the whole rule, and the line is where a lost field stops being
	// this ticket's own business. Two ways a tolerated TypeError would cross it:
	//
	//   - The ID is what says the decode produced a ticket at all. A TypeError is
	//     not exclusively the per-field kind: yaml.v3 reports a repeated top-level
	//     key as one too, and finds it in the uniqueKeys scan of the mapping before
	//     decoding any field, so nothing decodes — the struct comes back entirely
	//     zero and the raw-map pass below fails the same check. A hand-resolved
	//     merge conflict in the synced store is exactly how a doubled `status:`
	//     arrives, and tolerating it would yield a blank ticket.
	//   - A load-bearing field — `parent`, `deps`, `type` — is other tickets'
	//     business, or the derivation's. `parent: [epic-1111]` decodes to no parent
	//     at all, which silently removes the ticket from its epic's children and
	//     lets the epic read done over it; `deps: notalist` silently empties the
	//     dependencies, which shows the ticket as unblocked in `tk ready` and the
	//     frontier; `type: [epic]` on an epic's own file leaves it typeless, so
	//     deriveEpics passes over it and it renders the stale status its file
	//     stores — often a done a previous write baked in — with the demotion
	//     bypassed entirely. All three are the failure this leniency exists to
	//     remove, reintroduced through the door it opened, and none is visible
	//     anywhere: the file parses.
	//
	// Either way the file is refused, which makes it a reported skip and degrades
	// the epics around it, instead of a ticket quietly missing a field nothing can
	// see. The load-bearing check needs the raw map to tell a value that failed to
	// decode from one that was legitimately empty, so it runs with the second pass
	// and the original error is held until then — and if that pass cannot decode
	// either, the tolerance is taken back rather than granted unvetted. A struct
	// decode that succeeded outright is unaffected: the second pass has always been
	// best-effort for the fields only it reads.
	var t Ticket
	var typeErr *yaml.TypeError
	structErr := yaml.Unmarshal(front, &t)
	if structErr != nil {
		if !errors.As(structErr, &typeErr) || t.ID == "" {
			return nil, fmt.Errorf("parsing frontmatter: %w", structErr)
		}
	}

	// Second pass: capture unknown keys into Extra, and handle legacy fields.
	var raw map[string]interface{}
	rawErr := yaml.Unmarshal(front, &raw)
	if structErr != nil && (rawErr != nil || lostLoadBearing(&t, raw)) {
		return nil, fmt.Errorf("parsing frontmatter: %w", structErr)
	}
	if rawErr == nil {
		t.Extra = map[string]string{}
		for k, v := range raw {
			if !reservedKeys[k] && !legacyKeys[k] {
				t.Extra[k] = fmt.Sprintf("%v", v)
			}
		}

		// The dates use yaml:"-" and are parsed manually here.
		if v, ok := raw["created"]; ok {
			t.Created = parseTimeValue(v)
		}
		if v, ok := raw["updated"]; ok {
			t.Updated = parseTimeValue(v)
		}
		if v, ok := raw["completed"]; ok {
			t.Completed = parseTimeValue(v)
		}

		// Migrate legacy stage → status if status is not already set.
		if t.Status == "" {
			if stage, ok := raw["stage"]; ok {
				if s, ok := stage.(string); ok {
					if mapped, ok := legacyStageToStatus[s]; ok {
						t.Status = mapped
					}
				}
			}
		}

		// Migrate legacy old-style status values.
		if t.Status != "" && !validStatuses[t.Status] {
			if mapped, ok := legacyStatusToStatus[string(t.Status)]; ok {
				t.Status = mapped
			}
		}
	}

	// Ensure nil slices become empty slices for consistent handling.
	if t.Deps == nil {
		t.Deps = []string{}
	}
	if t.Links == nil {
		t.Links = []string{}
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.Extra == nil {
		t.Extra = map[string]string{}
	}

	// Migrate legacy types.
	if t.Type == "task" || t.Type == "chore" {
		t.Type = TypeFeature
	}

	parseBody(&t, body)
	return &t, nil
}

// lostLoadBearing reports whether a tolerated type error cost a field that
// something outside this ticket depends on: the parent an epic gathers its
// children by, the deps that decide whether the ticket is ready to pick up, or
// the type that decides whether the ticket is an epic and therefore derives a
// status at all. The frontmatter carried the key, the decode produced nothing
// for it, and the raw value is not itself empty — so the value was there and was
// dropped, rather than never having been written. `parent:`, `parent: ""`,
// `deps: []` and `type: ""` are all a writer saying "none": readable here, and
// judged by Validate on the way in like any other missing value.
//
// Local fields are deliberately not checked. An unreadable priority or a
// mistyped abandon flag is wrong only about the ticket it is on, and that is
// visible on the ticket; a lost parent, dep or type is wrong about a ticket
// somewhere else, and nothing about the ticket shows it.
func lostLoadBearing(t *Ticket, raw map[string]interface{}) bool {
	if v, ok := raw["parent"]; ok && t.Parent == "" && !emptyYAMLValue(v) {
		return true
	}
	if v, ok := raw["deps"]; ok && len(t.Deps) == 0 && !emptyYAMLValue(v) {
		return true
	}
	if v, ok := raw["type"]; ok && t.Type == "" && !emptyYAMLValue(v) {
		return true
	}
	// The verdict ledger breaks the "local fields are not checked" rule above for
	// a different reason than parent, deps and type do. It is append-only, so a
	// tolerated decode loss would be replayed by the ticket's next write-back as
	// the deletion or the rewriting of the rows it held — through a path that
	// never meant to touch them, and past the append-only check in updateLocked,
	// which compares the damaged prior against the damaged next and finds them
	// equal. Refusing the file keeps the record.
	//
	// The loss is partial as often as it is total, so all three shapes are
	// refused: yaml.v3 records a type error per element and decodes the rest, so
	// a malformed element drops out of the slice while its neighbours survive —
	// caught by the element count — and an element with one unreadable field
	// yields a row missing that field, caught by the emptiness of a field
	// ValidateVerdictRow requires, which is what makes an empty one on this path
	// decode damage rather than something a writer wrote. Every field of a
	// VerdictRow is required on write, so the scan below covers all of them.
	if v, ok := raw["verdicts"]; ok && !emptyYAMLValue(v) {
		if len(t.Verdicts) == 0 {
			return true
		}
		if rows, ok := v.([]interface{}); ok && len(rows) != len(t.Verdicts) {
			return true
		}
		for _, r := range t.Verdicts {
			if r.Ticket == "" || r.SHA == "" || r.Class == "" || r.Role == "" || r.Evidence == "" || r.By == "" || r.At == "" {
				return true
			}
		}
	}
	return false
}

// emptyYAMLValue reports whether a frontmatter value carries nothing — the
// forms a writer uses to say a field has no value. Anything else is content the
// decode was meant to produce something from.
func emptyYAMLValue(v interface{}) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return val == ""
	case []interface{}:
		return len(val) == 0
	case map[string]interface{}:
		return len(val) == 0
	}
	return false
}

// legacyKeys are YAML keys from the old pipeline model that should be
// silently dropped on read rather than captured as Extra fields.
var legacyKeys = map[string]bool{
	"stage": true, "review": true, "risk": true, "skipped": true,
	"assignee": true, "conversations": true,
}

// Serialize writes a ticket to canonical markdown+YAML format.
func Serialize(t *Ticket) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("---\n")
	writeField(&buf, "id", t.ID)
	writeField(&buf, "status", string(t.Status))
	if t.Abandoned {
		writeField(&buf, "abandoned", "true")
	}
	writeFlowArray(&buf, "deps", t.Deps)
	writeFlowArray(&buf, "links", t.Links)
	writeField(&buf, "created", t.Created.UTC().Format(time.RFC3339))
	if !t.Updated.IsZero() {
		writeField(&buf, "updated", t.Updated.UTC().Format(time.RFC3339))
	}
	if !t.Completed.IsZero() {
		writeField(&buf, "completed", t.Completed.UTC().Format(time.RFC3339))
	}
	writeField(&buf, "type", string(t.Type))
	writeField(&buf, "priority", fmt.Sprintf("%d", t.Priority))
	if t.ExternalRef != "" {
		writeField(&buf, "external-ref", t.ExternalRef)
	}
	if t.Branch != "" {
		writeField(&buf, "branch", t.Branch)
	}
	if t.Parent != "" {
		writeField(&buf, "parent", t.Parent)
	}
	if len(t.Tags) > 0 {
		writeFlowArray(&buf, "tags", t.Tags)
	}
	if len(t.Extra) > 0 {
		keys := make([]string, 0, len(t.Extra))
		for k := range t.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeField(&buf, k, t.Extra[k])
		}
	}
	if len(t.Outputs) > 0 {
		keys := make([]string, 0, len(t.Outputs))
		for k := range t.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteString("outputs:\n")
		for _, k := range keys {
			buf.WriteString("  " + k + ": " + t.Outputs[k] + "\n")
		}
	}
	if len(t.DepCargo) > 0 {
		if err := writeMapBlock(&buf, "dep-cargo", t.DepCargo); err != nil {
			return nil, err
		}
	}
	if len(t.Verdicts) > 0 {
		// An evidence pointer or an identity is free-form text that needs quoting
		// when it carries punctuation. yaml.v3 preserves struct field order, so
		// the output is deterministic.
		if err := writeYAMLBlock(&buf, "verdicts", t.Verdicts); err != nil {
			return nil, err
		}
	}
	buf.WriteString("---\n")

	buf.WriteString("# " + t.Title + "\n")

	if t.Body != "" {
		buf.WriteString("\n")
		buf.WriteString(t.Body)
		if !strings.HasSuffix(t.Body, "\n") {
			buf.WriteString("\n")
		}
	}

	if len(t.Notes) > 0 {
		if !strings.Contains(t.Body, "## Notes") {
			buf.WriteString("\n## Notes\n")
		}
		for _, n := range t.Notes {
			buf.WriteString("\n**" + n.Timestamp.UTC().Format(time.RFC3339) + "**\n\n")
			buf.WriteString(n.Text + "\n")
		}
	}

	return buf.Bytes(), nil
}

// splitFrontmatter separates YAML frontmatter from the markdown body.
// Expects the file to start with "---\n".
func splitFrontmatter(r io.Reader) (front []byte, body string, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var state int // 0=before opening ---, 1=in frontmatter, 2=body
	var frontBuf bytes.Buffer
	var bodyBuf bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()
		switch state {
		case 0:
			if strings.TrimSpace(line) == "---" {
				state = 1
			}
		case 1:
			if strings.TrimSpace(line) == "---" {
				state = 2
			} else {
				frontBuf.WriteString(line + "\n")
			}
		case 2:
			bodyBuf.WriteString(line + "\n")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	if state < 2 {
		return nil, "", fmt.Errorf("missing or incomplete YAML frontmatter")
	}
	return frontBuf.Bytes(), bodyBuf.String(), nil
}

// parseBody extracts title, body text, and notes from the markdown content.
func parseBody(t *Ticket, body string) {
	lines := strings.Split(body, "\n")

	// Find the title (first # heading).
	var titleIdx int = -1
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			t.Title = strings.TrimPrefix(line, "# ")
			titleIdx = i
			break
		}
	}

	if titleIdx < 0 {
		t.Body = body
		return
	}

	// Everything after the title line is the body.
	// Split off Notes section (parsed into struct field).
	rest := strings.Join(lines[titleIdx+1:], "\n")

	// Extract ## Notes section.
	notesIdx := strings.Index(rest, "\n## Notes\n")
	if notesIdx == -1 && strings.HasPrefix(rest, "## Notes\n") {
		notesIdx = 0
	}

	// Strip legacy ## Review Log section from body (no longer parsed).
	reviewIdx := strings.Index(rest, "\n## Review Log\n")
	if reviewIdx == -1 && strings.HasPrefix(rest, "## Review Log\n") {
		reviewIdx = 0
	}

	// Determine body end: the earliest of review log or notes.
	bodyEnd := len(rest)
	if reviewIdx >= 0 && reviewIdx < bodyEnd {
		bodyEnd = reviewIdx
	}
	if notesIdx >= 0 && notesIdx < bodyEnd {
		bodyEnd = notesIdx
	}
	t.Body = rest[:bodyEnd]

	if notesIdx >= 0 {
		sectionStart := notesIdx + len("\n## Notes\n")
		if notesIdx == 0 {
			sectionStart = len("## Notes\n")
		}
		// Notes section ends at the next structural section or end.
		sectionEnd := len(rest)
		if reviewIdx > notesIdx {
			sectionEnd = reviewIdx
		}
		t.Notes = parseNotes(rest[sectionStart:sectionEnd])
	}

	t.Body = strings.TrimSpace(t.Body) + "\n"
}

// parseNotes extracts timestamped notes from the notes section.
// Format:
//
//	**2026-02-22T01:15:50Z**
//
//	Note text here
func parseNotes(section string) []Note {
	var notes []Note
	lines := strings.Split(section, "\n")
	var current *Note

	for _, line := range lines {
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			tsStr := strings.Trim(line, "*")
			ts, err := time.Parse(time.RFC3339, tsStr)
			if err != nil {
				// Not a timestamp — treat as body text.
				if current != nil {
					current.Text += line + "\n"
				}
				continue
			}
			// Valid timestamp: flush previous note and start a new one.
			if current != nil {
				current.Text = strings.TrimSpace(current.Text)
				notes = append(notes, *current)
			}
			current = &Note{Timestamp: ts}
		} else if current != nil {
			current.Text += line + "\n"
		}
	}
	if current != nil {
		current.Text = strings.TrimSpace(current.Text)
		notes = append(notes, *current)
	}
	return notes
}

// structuralSections lists the heading prefixes that delimit ticket body sections.
var structuralSections = []string{
	"\n## Design",
	"\n## Acceptance Criteria",
	"\n## Test Results",
	"\n## Notes",
}

// nextStructuralSection returns the index of the next known structural section
// marker in s, or -1 if none found.
func nextStructuralSection(s string) int {
	best := -1
	for _, marker := range structuralSections {
		idx := strings.Index(s, marker)
		if idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

// UpdateSection replaces or inserts a markdown section in the body.
// If heading is empty, replaces the description (text before first structural heading).
func UpdateSection(body, heading, content string) string {
	if heading == "" {
		idx := nextStructuralSection(body)
		if idx >= 0 {
			return "\n" + content + "\n" + body[idx:]
		}
		return "\n" + content + "\n"
	}

	marker := "## " + heading
	idx := strings.Index(body, marker)
	if idx >= 0 {
		rest := body[idx+len(marker):]
		nextIdx := nextStructuralSection(rest)
		var after string
		if nextIdx >= 0 {
			after = rest[nextIdx:]
		}
		return body[:idx] + marker + "\n\n" + content + "\n" + after
	}

	// Section doesn't exist — append before Notes if present, else at end.
	notesIdx := strings.Index(body, "\n## Notes")
	if notesIdx >= 0 {
		return body[:notesIdx] + "\n" + marker + "\n\n" + content + "\n" + body[notesIdx:]
	}
	return body + "\n" + marker + "\n\n" + content + "\n"
}

// BodySections splits a ticket body into the free-text sections the structural
// headings delimit. The counterpart to UpdateSection, which writes them: the
// MCP response fields are read out with it, and so is the audit that inspects
// stored content, so both see the same split.
//
// The headings match on prefix, so `## Acceptance Notes` written by hand reads
// as the acceptance section — intentional, and the same looseness UpdateSection
// writes through, which is why a section written by one is found by the other.
// structuralSections above is a separate list serving a different job (it
// bounds a section being replaced, and includes `## Notes`); the two are not
// derived from one another.
func BodySections(body string) (desc, design, acceptance, testResults string) {
	lines := strings.Split(body, "\n")
	var current *string
	var buf []string

	flush := func() {
		if current != nil {
			*current = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}

	desc = ""
	current = &desc

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "## Design"):
			flush()
			current = &design
		case strings.HasPrefix(line, "## Acceptance"):
			flush()
			current = &acceptance
		case strings.HasPrefix(line, "## Test Results"):
			flush()
			current = &testResults
		default:
			buf = append(buf, line)
		}
	}
	flush()
	return
}

// parseTimeValue extracts a time.Time from a YAML-decoded frontmatter value,
// which may already be a time.Time or an RFC3339 string. Returns the zero time
// if the value cannot be parsed.
func parseTimeValue(v interface{}) time.Time {
	switch val := v.(type) {
	case time.Time:
		return val
	case string:
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			return t
		}
	}
	return time.Time{}
}

func writeField(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key + ": " + value + "\n")
}

// writeYAMLBlock emits a nested block under key, encoding the value through
// yaml.v3 so scalars are quoted when they need it. Unlike writeField, this is
// safe for free-form prose containing colons, hashes or quotes. Whether the
// output is deterministic depends on what is encoded, so each caller states it.
func writeYAMLBlock(buf *bytes.Buffer, key string, v any) error {
	var block bytes.Buffer
	enc := yaml.NewEncoder(&block)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	buf.WriteString(key + ":\n")
	for _, line := range strings.Split(strings.TrimRight(block.String(), "\n"), "\n") {
		buf.WriteString("  " + line + "\n")
	}
	return nil
}

// writeMapBlock emits a nested map under key. yaml.v3 sorts map keys, so the
// output is deterministic.
func writeMapBlock(buf *bytes.Buffer, key string, m map[string]string) error {
	return writeYAMLBlock(buf, key, m)
}

func writeFlowArray(buf *bytes.Buffer, key string, items []string) {
	if len(items) == 0 {
		buf.WriteString(key + ": []\n")
		return
	}
	buf.WriteString(key + ": [" + strings.Join(items, ", ") + "]\n")
}
