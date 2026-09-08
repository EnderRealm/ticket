package ticket

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Criterion is one acceptance criterion, optionally carrying a verify command
// or a claim that no command can exist for it.
//
// Unverifiable and UnverifiableReason are two fields rather than one because a
// bare "unverifiable:" with no reason text still carries the claim: an empty
// reason must stay distinguishable from a criterion carrying no such line at
// all, which a single string field would collapse.
type Criterion struct {
	Text               string
	Command            string // empty = unverified
	Unverifiable       bool   // an "unverifiable:" line was present
	UnverifiableReason string // the prose from that line; may be empty
}

// VerifyStatus is the outcome of checking a single criterion.
type VerifyStatus string

const (
	VerifyPass       VerifyStatus = "pass"
	VerifyFail       VerifyStatus = "fail"
	VerifyRefused    VerifyStatus = "refused"
	VerifyUnverified VerifyStatus = "unverified"
)

// VerifyResult pairs a criterion with the outcome of running its command. A
// refused command never ran: its Output carries the refusal, not command output.
// Criterion holds the sanitized text, command and unverifiable reason —
// RunVerify strips control characters once, so every consumer can print it
// as-is; the raw command is what was tokenized and execed.
type VerifyResult struct {
	Criterion Criterion
	Status    VerifyStatus
	ExitCode  int    // meaningful for pass/fail
	Output    string // combined stdout+stderr, trimmed and capped
}

// verifyPrefix marks a criterion's check command on a continuation line.
const verifyPrefix = "verify:"

// unverifiablePrefix marks, on a continuation line, a criterion for which no
// runnable command can exist, with the reason it cannot. It is carried as data
// only: the reason is prose from the same untrusted body a verify command comes
// from, with no allow-list behind it, so it is never tokenized or execed.
const unverifiablePrefix = "unverifiable:"

// maxVerifyOutput caps captured output per criterion so a chatty command can't
// blow up the reported results.
const maxVerifyOutput = 4096

// verifyWaitDelay bounds how long a command's I/O is waited on after its
// context expires. Without it a grandchild holding the output pipe keeps Wait
// blocked and the timeout is only advisory.
const verifyWaitDelay = 5 * time.Second

// DefaultVerifyTimeout bounds a single criterion command when the project sets
// no verify_timeout; an overrun is reported as a failure. It is the only place
// the default is defined — internal/project reports an unset bound as 0 rather
// than restating the number, because it cannot import this package.
const DefaultVerifyTimeout = 120 * time.Second

// VerifyPolicy carries the machine-local controls a verify run answers to.
// Neither control may come from the ticket, the synced store or a caller's
// argument: both decide what runs as the machine owner, and ticket bodies
// replicate over the shared store's git remote.
type VerifyPolicy struct {
	// Allow is the argv[0] allow-list. A command whose program is not an exact
	// match is refused without running.
	Allow []string
	// AllowErr is the reason that allow-list could not be read, if any: Allow is
	// then empty, everything is refused, and the refusal names the cause rather
	// than claiming the user left the program off a list they never got to write.
	AllowErr error
	// Timeout is the project's per-command bound; non-positive means
	// DefaultVerifyTimeout.
	Timeout time.Duration
	// TimeoutErr is the reason that bound could not be read. When set, every
	// command is refused naming it — a bound that does not parse must not
	// silently restore the default, which is the bound the project changed away
	// from.
	TimeoutErr error
}

// AcceptanceCriteria returns the text of the body's acceptance criteria
// section: the same section BodySections returns and ticket_show reports as
// acceptance_criteria, so the criterion index a consumer takes from there is
// the one `tk verify --criterion <n>` runs. BodySections owns the split; a
// second scan here is how the two came to disagree.
func AcceptanceCriteria(body string) string {
	_, _, acceptance, _ := BodySections(body)
	return acceptance
}

// ParseCriteria extracts criteria from an acceptance-criteria section. A
// top-level "- " bullet starts a criterion; a following line indented by at
// least two spaces and reading "verify: <command>" attaches that command to it,
// and one reading "unverifiable: <reason>" marks it as having no runnable check
// and records the reason. The first line of each kind wins — later ones under
// the same bullet are ignored, as is either line with no preceding bullet. An
// empty verify line is not a command, so it leaves the slot open for a later
// non-empty one; an empty unverifiable line still consumes the slot, because
// the claim is the line's presence rather than the reason. The two are
// independent: a criterion carrying both keeps its command. Bullets with no
// text are skipped.
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
		case strings.HasPrefix(line, "  ") && strings.HasPrefix(trimmed, unverifiablePrefix):
			// Unlike an empty verify line, which leaves the slot open for a later
			// non-empty one because an empty command is not runnable, an empty
			// unverifiable line consumes the slot: the claim is the line's
			// presence, and an empty reason is still the claim.
			if len(criteria) == 0 || criteria[len(criteria)-1].Unverifiable {
				continue
			}
			criteria[len(criteria)-1].Unverifiable = true
			criteria[len(criteria)-1].UnverifiableReason = strings.TrimSpace(trimmed[len(unverifiablePrefix):])
		}
	}
	return criteria
}

// BareCriteria returns the text of every acceptance criterion in body that
// carries neither a verify command nor an unverifiable claim. Such a criterion
// states what done means with nothing that can decide it, and no other surface
// reports the gap at the moment it is written. Text is sanitized because it is
// untrusted markdown both the CLI and the MCP response print.
func BareCriteria(body string) []string {
	var bare []string
	for _, c := range ParseCriteria(AcceptanceCriteria(body)) {
		if c.Command == "" && !c.Unverifiable {
			bare = append(bare, SanitizeControl(c.Text))
		}
	}
	return bare
}

// BareAcceptanceWarning is the one phrasing for criteria BareCriteria found,
// said the same way by `tk create` and ticket_create. Empty bare means no
// warning, so a caller can assign the result unconditionally.
//
// The remedy names ticket_edit and the section itself, and deliberately not
// `tk edit`: that command has no acceptance flag, and its --description writes
// only the description section, so a CLI operator sent there finds nothing that
// writes the criteria. One phrasing reaches both surfaces, so it may name only
// what is true on both.
func BareAcceptanceWarning(id string, bare []string) string {
	if len(bare) == 0 {
		return ""
	}
	quoted := make([]string, len(bare))
	for i, text := range bare {
		quoted[i] = fmt.Sprintf("%q", text)
	}
	return fmt.Sprintf("ticket %s has acceptance criteria with no way to check them: %s. "+
		"Nothing decides whether they are met, and the workflow gates on that. "+
		"Add a `verify: <command>` line under each, or an `unverifiable: <reason>` line saying why no command can exist — "+
		"with ticket_edit on %s and an `acceptance` argument, or by editing that ticket's `## Acceptance Criteria` section.",
		id, strings.Join(quoted, ", "), id)
}

// RunVerify executes each criterion's command in dir, sequentially and in
// order, each bounded by policy's timeout and by ctx. Criteria without commands
// yield unverified results. An unusable dir is a single error rather than a
// failure per criterion.
//
// A verify command is attacker-reachable content — ticket bodies are written by
// agents and replicate across machines over the shared store's git remote — so
// it is never handed to a shell. It is tokenized without expansion and execed
// as argv, and runs only if argv[0] exactly matches an entry in policy.Allow;
// everything else is refused without running. Policy must come from
// machine-local config, never from the ticket, the synced store or a caller's
// argument, and a policy whose controls could not be read refuses everything
// naming the cause.
//
// A criterion's text, command and unverifiable reason are stripped of control
// characters; a command's own captured Output is deliberately left raw, so
// that the coloured output of a test runner survives to the terminal in the
// readable form the field exists for. That output comes from a program the
// machine's owner allow-listed, which is a narrower trust than the ticket
// content around it.
func RunVerify(ctx context.Context, criteria []Criterion, dir string, policy VerifyPolicy) ([]VerifyResult, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("verify directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("verify directory %s is not a directory", dir)
	}

	results := make([]VerifyResult, 0, len(criteria))
	for _, c := range criteria {
		res := VerifyResult{Criterion: c, Status: VerifyUnverified}
		if c.Command != "" {
			res = runCriterion(ctx, c, dir, policy)
		}
		// A criterion's text, command and unverifiable reason are all untrusted
		// markdown that every consumer prints. Sanitizing here, where the result
		// is produced, keeps each print site from having to remember to.
		res.Criterion.Text = SanitizeControl(res.Criterion.Text)
		res.Criterion.Command = SanitizeControl(res.Criterion.Command)
		res.Criterion.UnverifiableReason = SanitizeControl(res.Criterion.UnverifiableReason)
		results = append(results, res)
	}
	return results, nil
}

// tokenizeCommand splits a verify command into argv on whitespace, honouring
// single and double quotes so a quoted argument such as -run 'TestA|TestB'
// survives whole. Nothing is expanded: variables, globs, backslash escapes, ~
// and every shell metacharacter stay literal, because the result is execed
// directly rather than interpreted.
func tokenizeCommand(s string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	var quote rune
	started := false

	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			started = true
		case unicode.IsSpace(r):
			if started {
				argv = append(argv, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if started {
		argv = append(argv, cur.String())
	}
	return argv, nil
}

// allowedCommand reports whether argv0 is permitted. Comparison is exact
// string equality, not basename matching: an entry of "go" must not admit
// /tmp/evil/go.
func allowedCommand(argv0 string, allow []string) bool {
	for _, a := range allow {
		if argv0 == a {
			return true
		}
	}
	return false
}

// refusal explains why a command did not run and how to permit it. The command
// itself comes last: it is a whole markdown line of untrusted content, and
// capOutput must not be able to cut off the remedy to fit it. The config path is
// spelled out rather than threaded in as a parameter; it must track
// internal/project.ConfigPath, which this package deliberately does not import —
// the core library stays free of machine config resolution.
func refusal(command, argv0 string, allowErr error) string {
	cause := fmt.Sprintf("refused: %q is not in verify_allow, so this criterion's command did not run.\n", argv0)
	remedy := fmt.Sprintf("To permit it, add %q to the verify_allow list in ~/.ticket/config.yaml. ", argv0)
	if allowErr != nil {
		cause = fmt.Sprintf("refused: ~/.ticket/config.yaml could not be read (%v), so no command is permitted and this criterion's command did not run.\n", allowErr)
		// Adding an entry to a file that does not parse changes nothing; the
		// cause above names where the repair goes.
		remedy = "To permit any command, repair ~/.ticket/config.yaml so it parses. "
	}
	return capOutput(cause + remedy + fmt.Sprintf("The verify_allow list is read from "+
		"machine-local config only — an entry in the shared central-store config, in the ticket, or in a tool argument is ignored.\n"+
		"command: %s", SanitizeControl(command)))
}

// timeoutRefusal explains that the project's bound could not be read and how to
// repair it. Same shape as refusal, and the same reason for the ordering: the
// command is a whole markdown line of untrusted content and must not be able to
// push the remedy past capOutput's cut.
func timeoutRefusal(command string, timeoutErr error) string {
	cause := fmt.Sprintf("refused: the project's verify_timeout could not be read (%v), so no command is permitted and this criterion's command did not run.\n", timeoutErr)
	remedy := "To permit any command, set verify_timeout under the project in ~/.ticket/config.yaml to a positive Go duration such as 300s or 5m, or remove the key. "
	return capOutput(cause + remedy + fmt.Sprintf("The verify_timeout is read from "+
		"machine-local config only — an entry in the shared central-store config, in the ticket, or in a tool argument is ignored.\n"+
		"command: %s", SanitizeControl(command)))
}

// SanitizeControl replaces C0 and C1 control characters, DEL, and the Unicode
// format characters with U+FFFD. It is the shared rule for any untrusted string
// tk prints to an operator or writes into a line-oriented record. A criterion's
// text and command are the case it was written for: content that a refusal
// echoes to the operator's terminal at the one moment they are asked to judge
// it, where a raw escape sequence could repaint that output — including making a
// REFUSED line read as PASS — and a bidi control such as U+202E RIGHT-TO-LEFT
// OVERRIDE reverses what is rendered without changing what would run. `tk sync`
// applies it to the ticket filenames it names in warnings, which arrive over git
// from other machines.
//
// TAB is exempt: it carries none of that repaint risk and is ordinary inside a
// hand-written markdown bullet. Newline and carriage return are not exempt,
// because FormatVerifyRecord writes one line per criterion and an embedded
// break would forge record lines that every later read replays.
func SanitizeControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.Is(unicode.Cf, r) {
			return '�'
		}
		return r
	}, s)
}

func runCriterion(ctx context.Context, c Criterion, dir string, policy VerifyPolicy) VerifyResult {
	argv, err := tokenizeCommand(c.Command)
	if err != nil {
		return VerifyResult{
			Criterion: c,
			Status:    VerifyRefused,
			Output:    capOutput(fmt.Sprintf("refused: cannot parse this criterion's command: %v\ncommand: %s", err, SanitizeControl(c.Command))),
		}
	}
	// Defensive: RunVerify calls this only for a non-empty Command, but a caller
	// constructing a Criterion directly could supply one that is all whitespace.
	if len(argv) == 0 {
		return VerifyResult{Criterion: c, Status: VerifyUnverified}
	}
	// Checked before the allow-list so a command that would be refused either
	// way names the config defect the user has to repair.
	if policy.TimeoutErr != nil {
		return VerifyResult{Criterion: c, Status: VerifyRefused, Output: timeoutRefusal(c.Command, policy.TimeoutErr)}
	}
	if !allowedCommand(argv[0], policy.Allow) {
		return VerifyResult{Criterion: c, Status: VerifyRefused, Output: refusal(c.Command, argv[0], policy.AllowErr)}
	}

	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = DefaultVerifyTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
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
			res.Output += fmt.Sprintf("\ncommand timed out after %s", timeout)
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

// FormatVerifyRecord renders a verify run as ticket Test Results content. The
// criterion text and command are written as RunVerify produced them, already
// stripped of control characters — the record is replayed by every later read.
func FormatVerifyRecord(results []VerifyResult, at time.Time) string {
	counts := countVerify(results)
	var b strings.Builder
	fmt.Fprintf(&b, "verify %s: %d pass, %d fail, %d refused, %d unverified\n",
		at.UTC().Format(time.RFC3339), counts.Pass, counts.Fail, counts.Refused, counts.Unverified)
	for _, r := range results {
		if r.Status == VerifyUnverified {
			fmt.Fprintf(&b, "- UNVERIFIED: %s\n", r.Criterion.Text)
			continue
		}
		// A refusal has no exit code — the command never ran, and recording it
		// as FAIL would read as a check that ran and disagreed.
		if r.Status == VerifyRefused {
			fmt.Fprintf(&b, "- REFUSED (%s): %s\n", r.Criterion.Command, r.Criterion.Text)
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
	Refused    int `json:"refused"`
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
		// A refusal leaves the criterion unchecked, so the run is not ok.
		OK:      counts.Fail == 0 && counts.Refused == 0,
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
		case VerifyRefused:
			counts.Refused++
		default:
			counts.Unverified++
		}
	}
	return counts
}
