package ticket

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// Criterion is one acceptance criterion, optionally carrying a verify command.
type Criterion struct {
	Text    string
	Command string // empty = unverified
}

// VerifyStatus is the outcome of checking a single criterion.
type VerifyStatus string

const (
	VerifyPass       VerifyStatus = "pass"
	VerifyFail       VerifyStatus = "fail"
	VerifyUnverified VerifyStatus = "unverified"
)

// VerifyResult pairs a criterion with the outcome of running its command.
type VerifyResult struct {
	Criterion Criterion
	Status    VerifyStatus
	ExitCode  int    // meaningful for pass/fail
	Output    string // combined stdout+stderr, trimmed and capped
}

// verifyPrefix marks a criterion's check command on a continuation line.
const verifyPrefix = "verify:"

// maxVerifyOutput caps captured output per criterion so a chatty command can't
// blow up the reported results.
const maxVerifyOutput = 4096

// verifyWaitDelay bounds how long a command's I/O is waited on after its
// context expires. Without it a grandchild holding the output pipe keeps Wait
// blocked and the timeout is only advisory.
const verifyWaitDelay = 5 * time.Second

// verifyTimeout bounds a single criterion command; an overrun is reported as a
// failure. Package-level so tests can shrink it.
var verifyTimeout = 120 * time.Second

// AcceptanceCriteria returns the text of the body's acceptance criteria section.
func AcceptanceCriteria(body string) string {
	var section []string
	in := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.HasPrefix(line, "## Acceptance")
			continue
		}
		if in {
			section = append(section, line)
		}
	}
	return strings.TrimSpace(strings.Join(section, "\n"))
}

// ParseCriteria extracts criteria from an acceptance-criteria section. A
// top-level "- " bullet starts a criterion; a following line indented by at
// least two spaces and reading "verify: <command>" attaches that command to it.
// The first such line wins — later ones under the same bullet are ignored, as
// is a verify line with no preceding bullet. Bullets with no text are skipped.
func ParseCriteria(section string) []Criterion {
	var criteria []Criterion
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "- "):
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if text != "" {
				criteria = append(criteria, Criterion{Text: text})
			}
		case strings.HasPrefix(line, "  ") && strings.HasPrefix(trimmed, verifyPrefix):
			if len(criteria) == 0 || criteria[len(criteria)-1].Command != "" {
				continue
			}
			criteria[len(criteria)-1].Command = strings.TrimSpace(trimmed[len(verifyPrefix):])
		}
	}
	return criteria
}

// RunVerify executes each criterion's command with `sh -c` in dir, sequentially
// and in order, each bounded by verifyTimeout and by ctx. Criteria without
// commands yield unverified results. An unusable dir is a single error rather
// than a failure per criterion.
func RunVerify(ctx context.Context, criteria []Criterion, dir string) ([]VerifyResult, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("verify directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("verify directory %s is not a directory", dir)
	}

	results := make([]VerifyResult, 0, len(criteria))
	for _, c := range criteria {
		if c.Command == "" {
			results = append(results, VerifyResult{Criterion: c, Status: VerifyUnverified})
			continue
		}
		results = append(results, runCriterion(ctx, c, dir))
	}
	return results, nil
}

func runCriterion(ctx context.Context, c Criterion, dir string) VerifyResult {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
	cmd.Dir = dir
	cmd.WaitDelay = verifyWaitDelay
	out, err := cmd.CombinedOutput()

	res := VerifyResult{Criterion: c, Status: VerifyPass, Output: string(out)}
	if err != nil {
		res.Status = VerifyFail
		res.ExitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.Output += "\n" + err.Error()
		}
		if ctx.Err() == context.DeadlineExceeded {
			res.Output += fmt.Sprintf("\ncommand timed out after %s", verifyTimeout)
		}
	}
	res.Output = capOutput(res.Output)
	return res
}

// capOutput trims and caps output, backing the cut off to a rune boundary so a
// multi-byte character can't be split.
func capOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxVerifyOutput {
		return s
	}
	cut := maxVerifyOutput
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n... output truncated"
}

// FormatVerifyRecord renders a verify run as ticket Test Results content.
func FormatVerifyRecord(results []VerifyResult, at time.Time) string {
	counts := countVerify(results)
	var b strings.Builder
	fmt.Fprintf(&b, "verify %s: %d pass, %d fail, %d unverified\n",
		at.UTC().Format(time.RFC3339), counts.Pass, counts.Fail, counts.Unverified)
	for _, r := range results {
		if r.Status == VerifyUnverified {
			fmt.Fprintf(&b, "- UNVERIFIED: %s\n", r.Criterion.Text)
			continue
		}
		fmt.Fprintf(&b, "- %s (exit %d): %s\n", strings.ToUpper(string(r.Status)), r.ExitCode, r.Criterion.Text)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// VerifyReport is the structured result of a verify run, shared by the CLI's
// --json output and the ticket_verify MCP tool.
type VerifyReport struct {
	ID      string             `json:"id"`
	Dir     string             `json:"dir"`
	Summary VerifyCounts       `json:"summary"`
	OK      bool               `json:"ok"`
	Results []VerifyResultJSON `json:"results"`
	// RecordError reports a failure to write the results back to the ticket.
	// The results themselves are still valid.
	RecordError string `json:"record_error,omitempty"`
}

// VerifyCounts tallies results by status.
type VerifyCounts struct {
	Pass       int `json:"pass"`
	Fail       int `json:"fail"`
	Unverified int `json:"unverified"`
}

// VerifyResultJSON is the wire form of a single criterion's outcome.
type VerifyResultJSON struct {
	Criterion string `json:"criterion"`
	Command   string `json:"command,omitempty"`
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output,omitempty"`
}

// NewVerifyReport builds the structured report for a verify run.
func NewVerifyReport(id, dir string, results []VerifyResult) VerifyReport {
	counts := countVerify(results)
	report := VerifyReport{
		ID:      id,
		Dir:     dir,
		Summary: counts,
		OK:      counts.Fail == 0,
		Results: []VerifyResultJSON{},
	}
	for _, r := range results {
		report.Results = append(report.Results, VerifyResultJSON{
			Criterion: r.Criterion.Text,
			Command:   r.Criterion.Command,
			Status:    string(r.Status),
			ExitCode:  r.ExitCode,
			Output:    r.Output,
		})
	}
	return report
}

func countVerify(results []VerifyResult) VerifyCounts {
	var counts VerifyCounts
	for _, r := range results {
		switch r.Status {
		case VerifyPass:
			counts.Pass++
		case VerifyFail:
			counts.Fail++
		default:
			counts.Unverified++
		}
	}
	return counts
}
