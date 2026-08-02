package ticket

import (
	"bufio"
	"bytes"
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

	var t Ticket
	if err := yaml.Unmarshal(front, &t); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	// Second pass: capture unknown keys into Extra, and handle legacy fields.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(front, &raw); err == nil {
		t.Extra = map[string]string{}
		for k, v := range raw {
			if !reservedKeys[k] && !legacyKeys[k] {
				t.Extra[k] = fmt.Sprintf("%v", v)
			}
		}

		// Updated and Completed use yaml:"-" and are parsed manually here.
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

// writeMapBlock emits a nested map under key, encoding it through yaml.v3 so
// values are quoted when they need it. Unlike writeField, this is safe for
// free-form prose containing colons, hashes or quotes. yaml.v3 sorts map keys,
// so the output is deterministic.
func writeMapBlock(buf *bytes.Buffer, key string, m map[string]string) error {
	var block bytes.Buffer
	enc := yaml.NewEncoder(&block)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
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

func writeFlowArray(buf *bytes.Buffer, key string, items []string) {
	if len(items) == 0 {
		buf.WriteString(key + ": []\n")
		return
	}
	buf.WriteString(key + ": [" + strings.Join(items, ", ") + "]\n")
}
