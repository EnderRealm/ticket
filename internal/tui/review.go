package tui

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

type reviewMode int

const (
	reviewIdle reviewMode = iota
	reviewApproveInput
	reviewRejectInput
	reviewStagePicker
)

type reviewModel struct {
	ticket      *ticket.Ticket
	lines       []string
	offset      int
	width       int
	height      int
	mode        reviewMode
	inputText   string
	stages      []ticket.Stage
	stageCursor int
}

func newReviewModel(t *ticket.Ticket, w, h int) reviewModel {
	m := reviewModel{
		ticket: t,
		width:  w,
		height: h,
	}
	m.lines = m.renderReview()
	return m
}

func (m *reviewModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m reviewModel) inputActive() bool {
	return m.mode != reviewIdle
}

func (m reviewModel) visibleRows() int {
	rows := m.height - 1 // help bar
	if m.mode == reviewStagePicker {
		rows -= len(m.stages) + 1 // label + items
	} else if m.mode != reviewIdle {
		rows-- // input bar
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m reviewModel) update(msg tea.Msg) (reviewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.mode == reviewStagePicker {
			return m.updateStagePicker(msg)
		}
		if m.mode != reviewIdle {
			return m.updateInput(msg)
		}

		maxOffset := max(0, len(m.lines)-m.visibleRows())
		switch msg.String() {
		case "up", "k":
			if m.offset > 0 {
				m.offset--
			}
		case "down", "j":
			if m.offset < maxOffset {
				m.offset++
			}
		case "pgup", "b":
			m.offset -= m.visibleRows()
			if m.offset < 0 {
				m.offset = 0
			}
		case "pgdown", "f", " ":
			m.offset += m.visibleRows()
			if m.offset > maxOffset {
				m.offset = maxOffset
			}
		case "g":
			m.offset = 0
		case "G":
			m.offset = maxOffset
		case "a":
			m.mode = reviewApproveInput
			m.inputText = ""
			return m, nil
		case "r":
			m.mode = reviewRejectInput
			m.inputText = ""
			return m, nil
		}
	}
	return m, nil
}

func (m reviewModel) updateInput(msg tea.KeyMsg) (reviewModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = reviewIdle
		m.inputText = ""
		return m, nil
	case "enter":
		id := m.ticket.ID
		text := m.inputText

		if m.mode == reviewApproveInput {
			m.mode = reviewIdle
			m.inputText = ""
			return m, func() tea.Msg {
				return reviewApproveMsg{id: id, notes: text}
			}
		}

		if m.mode == reviewRejectInput {
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return m, nil // reject requires non-empty feedback
			}
			// Populate stage picker with earlier stages.
			pipeline, err := ticket.PipelineFor(m.ticket.Type, m.ticket.Risk)
			if err != nil {
				m.mode = reviewIdle
				m.inputText = ""
				return m, func() tea.Msg {
					return statusMsg("error: " + err.Error())
				}
			}
			currentIdx := ticket.StageIndexInPipeline(pipeline, m.ticket.Stage)
			var stages []ticket.Stage
			for i := 0; i < currentIdx; i++ {
				stages = append(stages, pipeline[i])
			}
			if len(stages) == 0 {
				m.mode = reviewIdle
				m.inputText = ""
				return m, nil
			}
			m.stages = stages
			m.stageCursor = len(stages) - 1 // default to most recent stage
			m.mode = reviewStagePicker
			return m, nil
		}

		return m, nil
	case "backspace":
		if len(m.inputText) > 0 {
			m.inputText = m.inputText[:len(m.inputText)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.inputText += msg.String()
		}
	}
	return m, nil
}

func (m reviewModel) updateStagePicker(msg tea.KeyMsg) (reviewModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = reviewIdle
		m.inputText = ""
		m.stages = nil
		return m, nil
	case "up", "k":
		if m.stageCursor > 0 {
			m.stageCursor--
		}
	case "down", "j":
		if m.stageCursor < len(m.stages)-1 {
			m.stageCursor++
		}
	case "enter":
		id := m.ticket.ID
		notes := m.inputText
		stage := m.stages[m.stageCursor]
		m.mode = reviewIdle
		m.inputText = ""
		m.stages = nil
		return m, func() tea.Msg {
			return reviewRejectMsg{id: id, notes: notes, stage: stage}
		}
	}
	return m, nil
}

func (m reviewModel) view() string {
	var b strings.Builder

	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.lines) {
		end = len(m.lines)
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.lines[i])
		b.WriteString("\n")
	}

	// Pad remaining.
	for i := end - m.offset; i < visible; i++ {
		b.WriteString("\n")
	}

	// Input bar or stage picker.
	if m.mode == reviewStagePicker {
		b.WriteString(inputLabelStyle.Render("revert to stage:") + "\n")
		for i, stage := range m.stages {
			prefix := "  "
			if i == m.stageCursor {
				prefix = "> "
			}
			b.WriteString(prefix + string(stage) + "\n")
		}
	} else if m.mode == reviewApproveInput {
		b.WriteString(inputLabelStyle.Render("notes (optional): ") + m.inputText + "\u2588\n")
	} else if m.mode == reviewRejectInput {
		b.WriteString(inputLabelStyle.Render("feedback (required): ") + m.inputText + "\u2588\n")
	}

	// Help bar.
	var help string
	switch m.mode {
	case reviewIdle:
		help = "(a)pprove  (r)eject  \u2502  \u2191\u2193/jk scroll  \u2502  esc back  q quit"
	case reviewApproveInput:
		help = "enter confirm  esc cancel"
	case reviewRejectInput:
		help = "enter confirm (non-empty)  esc cancel"
	case reviewStagePicker:
		help = "\u2191\u2193 select  enter confirm  esc cancel"
	}
	b.WriteString(detailHelpStyle.Render(help))

	return b.String()
}

var reviewHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))

func (m reviewModel) renderReview() []string {
	t := m.ticket
	var lines []string
	pad := "  "
	avail := m.width - len(pad)
	if avail < 20 {
		avail = 20
	}

	// 1. Git checkout command.
	if t.Branch != "" {
		lines = append(lines, pad+reviewHeaderStyle.Render("git checkout "+t.Branch))
		lines = append(lines, "")
	}

	// 2. PR URL.
	if t.Branch != "" {
		if url := derivePRURL(t.Branch); url != "" {
			lines = append(lines, pad+reviewHeaderStyle.Render(url))
			lines = append(lines, "")
		}
	}

	// 3. Acceptance criteria.
	ac := extractSection(t.Body, "Acceptance Criteria")
	if ac != "" {
		lines = append(lines, pad+sectionStyle.Render("## Acceptance Criteria"))
		lines = append(lines, "")
		for _, line := range strings.Split(ac, "\n") {
			for _, wl := range wrapText(line, avail) {
				lines = append(lines, pad+wl.text)
			}
		}
		lines = append(lines, "")
	}

	// 4. Separator.
	lines = append(lines, pad+strings.Repeat("\u2500", min(avail, 60)))
	lines = append(lines, "")

	// 5. Full ticket detail (same as detailModel.render).
	lines = append(lines, m.field("Title", t.Title))
	lines = append(lines, m.field("ID", t.ID))
	if t.Stage != "" {
		lines = append(lines, m.field("Stage", string(t.Stage)))
	}
	if t.Review != "" {
		lines = append(lines, m.field("Review", string(t.Review)))
	}
	if t.Risk != "" {
		lines = append(lines, m.field("Risk", string(t.Risk)))
	}
	lines = append(lines, m.field("Type", string(t.Type)))
	lines = append(lines, m.field("Priority", fmt.Sprintf("P%d", t.Priority)))
	if t.Assignee != "" {
		lines = append(lines, m.field("Assignee", t.Assignee))
	}
	if t.Parent != "" {
		lines = append(lines, m.field("Parent", t.Parent))
	}
	if len(t.Deps) > 0 {
		lines = append(lines, m.field("Deps", strings.Join(t.Deps, ", ")))
	}
	if len(t.Links) > 0 {
		lines = append(lines, m.field("Links", strings.Join(t.Links, ", ")))
	}
	if len(t.Tags) > 0 {
		lines = append(lines, m.field("Tags", strings.Join(t.Tags, ", ")))
	}
	if t.ExternalRef != "" {
		lines = append(lines, m.field("External Ref", t.ExternalRef))
	}
	lines = append(lines, m.field("Created", t.Created.Format("2006-01-02 15:04")))

	if len(t.Extra) > 0 {
		keys := make([]string, 0, len(t.Extra))
		for k := range t.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, m.field(k, t.Extra[k]))
		}
	}

	lines = append(lines, "")

	// Body.
	if t.Body != "" {
		for _, line := range strings.Split(t.Body, "\n") {
			if strings.HasPrefix(line, "## ") {
				lines = append(lines, pad+sectionStyle.Render(line))
			} else {
				for _, wl := range wrapText(line, avail) {
					lines = append(lines, pad+wl.text)
				}
			}
		}
	}

	// Review log.
	if len(t.Reviews) > 0 {
		lines = append(lines, pad+sectionStyle.Render("## Review Log"))
		lines = append(lines, "")
		for _, r := range t.Reviews {
			verdict := strings.ToUpper(r.Verdict)
			ts := r.Timestamp.Format("2006-01-02 15:04")
			entry := fmt.Sprintf("[%s] %s %s", r.Reviewer, verdict, ts)
			if r.Comment != "" {
				entry += " \u2014 " + r.Comment
			}
			for _, wl := range wrapText(entry, avail) {
				lines = append(lines, pad+wl.text)
			}
		}
		lines = append(lines, "")
	}

	// Notes.
	if len(t.Notes) > 0 {
		lines = append(lines, pad+sectionStyle.Render("## Notes"))
		lines = append(lines, "")
		for _, n := range t.Notes {
			lines = append(lines, pad+timestampStyle.Render(n.Timestamp.Format("2006-01-02 15:04:05")))
			for _, wl := range wrapText(n.Text, avail) {
				lines = append(lines, pad+wl.text)
			}
			lines = append(lines, "")
		}
	}

	return lines
}

func (m reviewModel) field(key, val string) string {
	return "  " + fieldKeyStyle.Render(key+":") + " " + fieldValStyle.Render(val)
}

// extractSection returns the content of a ## heading section from the body,
// or empty string if not found.
func extractSection(body, heading string) string {
	marker := "## " + heading
	idx := strings.Index(body, marker)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(marker):]
	// Trim leading newlines.
	rest = strings.TrimLeft(rest, "\n")
	// Find end: next structural section or end of string.
	endMarkers := []string{"\n## Design", "\n## Acceptance Criteria", "\n## Test Results", "\n## Notes", "\n## Review Log"}
	endIdx := len(rest)
	for _, em := range endMarkers {
		if i := strings.Index(rest, em); i >= 0 && i < endIdx {
			endIdx = i
		}
	}
	return strings.TrimRight(rest[:endIdx], "\n")
}

// sshRemoteRe matches git@host:owner/repo.git style remotes.
var sshRemoteRe = regexp.MustCompile(`^git@([^:]+):([^/]+)/(.+?)(?:\.git)?$`)

// httpsRemoteRe matches https://host/owner/repo.git style remotes.
var httpsRemoteRe = regexp.MustCompile(`^https?://([^/]+)/([^/]+)/(.+?)(?:\.git)?$`)

// derivePRURL attempts to construct a GitHub PR URL from the branch name
// by reading the git remote origin URL.
func derivePRURL(branch string) string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	remote := strings.TrimSpace(string(out))

	var host, owner, repo string
	if m := sshRemoteRe.FindStringSubmatch(remote); m != nil {
		host, owner, repo = m[1], m[2], m[3]
	} else if m := httpsRemoteRe.FindStringSubmatch(remote); m != nil {
		host, owner, repo = m[1], m[2], m[3]
	} else {
		return ""
	}

	if !strings.Contains(host, "github") {
		return "" // only GitHub supported for now
	}

	return fmt.Sprintf("https://%s/%s/%s/compare/%s", host, owner, repo, branch)
}
