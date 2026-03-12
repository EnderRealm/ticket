package ticket

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

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
	if t.Skipped == nil {
		t.Skipped = []Stage{}
	}
	if t.Conversations == nil {
		t.Conversations = []string{}
	}

	// Auto-migrate legacy tickets that have status but no stage.
	if t.Status != "" && t.Stage == "" {
		MigrateTicket(&t)
	}

	parseBody(&t, body)
	return &t, nil
}

// Serialize writes a ticket to canonical markdown+YAML format.
// Field order matches the bash implementation for consistency.
func Serialize(t *Ticket) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("---\n")
	writeField(&buf, "id", t.ID)

	// Write stage if present, fall back to status for legacy tickets.
	if t.Stage != "" {
		writeField(&buf, "stage", string(t.Stage))
	}
	if t.Review != ReviewNone {
		writeField(&buf, "review", string(t.Review))
	}
	if t.Risk != "" {
		writeField(&buf, "risk", string(t.Risk))
	}

	writeFlowArray(&buf, "deps", t.Deps)
	writeFlowArray(&buf, "links", t.Links)
	writeField(&buf, "created", t.Created.UTC().Format(time.RFC3339))
	writeField(&buf, "type", string(t.Type))
	writeField(&buf, "priority", fmt.Sprintf("%d", t.Priority))
	if t.Assignee != "" {
		writeField(&buf, "assignee", t.Assignee)
	}
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
	if len(t.Skipped) > 0 {
		strs := make([]string, len(t.Skipped))
		for i, s := range t.Skipped {
			strs[i] = string(s)
		}
		writeFlowArray(&buf, "skipped", strs)
	}
	if len(t.Conversations) > 0 {
		writeFlowArray(&buf, "conversations", t.Conversations)
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

	if len(t.Reviews) > 0 {
		if !strings.Contains(t.Body, "## Review Log") {
			buf.WriteString("\n## Review Log\n")
		}
		for _, r := range t.Reviews {
			buf.WriteString("\n**" + r.Timestamp.UTC().Format(time.RFC3339) + " [" + r.Reviewer + "]**\n")
			verdict := strings.ToUpper(r.Verdict)
			if r.Comment != "" {
				buf.WriteString(verdict + " — " + r.Comment + "\n")
			} else {
				buf.WriteString(verdict + "\n")
			}
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
	// Split off Review Log and Notes sections (both are parsed into struct fields).
	rest := strings.Join(lines[titleIdx+1:], "\n")

	// Extract ## Review Log section.
	reviewIdx := strings.Index(rest, "\n## Review Log\n")
	if reviewIdx == -1 && strings.HasPrefix(rest, "## Review Log\n") {
		reviewIdx = 0
	}

	// Extract ## Notes section.
	notesIdx := strings.Index(rest, "\n## Notes\n")
	if notesIdx == -1 && strings.HasPrefix(rest, "## Notes\n") {
		notesIdx = 0
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

	if reviewIdx >= 0 {
		// Find where the review section ends (at next ## or end of rest).
		sectionStart := reviewIdx + len("\n## Review Log\n")
		if reviewIdx == 0 {
			sectionStart = len("## Review Log\n")
		}
		sectionEnd := len(rest)
		if notesIdx > reviewIdx {
			sectionEnd = notesIdx
		}
		t.Reviews = parseReviewLog(rest[sectionStart:sectionEnd])
	}

	if notesIdx >= 0 {
		sectionStart := notesIdx + len("\n## Notes\n")
		if notesIdx == 0 {
			sectionStart = len("## Notes\n")
		}
		t.Notes = parseNotes(rest[sectionStart:])
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

// parseReviewLog extracts ReviewRecord structs from the Review Log section.
// Format:
//
//	**2026-02-25T12:00:00Z [design-review-agent]**
//	APPROVED — All file paths verified.
func parseReviewLog(section string) []ReviewRecord {
	var reviews []ReviewRecord
	lines := strings.Split(section, "\n")
	var current *ReviewRecord

	for _, line := range lines {
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			// Flush previous record.
			if current != nil {
				reviews = append(reviews, *current)
			}
			inner := strings.Trim(line, "*")
			// Parse "2026-02-25T12:00:00Z [reviewer-name]"
			bracketOpen := strings.Index(inner, "[")
			bracketClose := strings.Index(inner, "]")
			if bracketOpen < 0 || bracketClose < 0 {
				current = nil
				continue
			}
			tsStr := strings.TrimSpace(inner[:bracketOpen])
			reviewer := inner[bracketOpen+1 : bracketClose]
			ts, err := time.Parse(time.RFC3339, tsStr)
			if err != nil {
				current = nil
				continue
			}
			current = &ReviewRecord{
				Timestamp: ts,
				Reviewer:  reviewer,
			}
		} else if current != nil && strings.TrimSpace(line) != "" {
			// Parse "VERDICT — comment" or just "VERDICT"
			parts := strings.SplitN(line, " — ", 2)
			current.Verdict = strings.ToLower(strings.TrimSpace(parts[0]))
			if len(parts) > 1 {
				current.Comment = strings.TrimSpace(parts[1])
			}
			// Infer stage from context — will be set by workflow logic.
		}
	}
	if current != nil {
		reviews = append(reviews, *current)
	}
	return reviews
}

// structuralSections lists the heading prefixes that delimit ticket body sections.
// User content within sections may contain arbitrary ## headings without conflict.
var structuralSections = []string{
	"\n## Design",
	"\n## Acceptance Criteria",
	"\n## Test Results",
	"\n## Notes",
	"\n## Review Log",
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

func writeField(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key + ": " + value + "\n")
}

func writeFlowArray(buf *bytes.Buffer, key string, items []string) {
	if len(items) == 0 {
		buf.WriteString(key + ": []\n")
		return
	}
	buf.WriteString(key + ": [" + strings.Join(items, ", ") + "]\n")
}
