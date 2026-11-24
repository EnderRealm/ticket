package ticket

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Landing and testing: what a landed ticket becomes, and the record of what a
// person said of it. Both are written through ApplyAction alone, except that
// the commit watcher lands a ticket by LandingStatus too, so the land action
// and the watcher cannot decide it differently.

// TestResult is what a person said of a landed ticket.
type TestResult string

const (
	TestPassed TestResult = "passed"
	TestFailed TestResult = "failed"
)

// TestCheck is one criterion a test failed: its 1-based index among the
// ticket's `test:` criteria in parse order, and its text as it read at the
// test, so a later edit of the criteria renames nothing the record says.
type TestCheck struct {
	Index int    `yaml:"index" json:"index"`
	Text  string `yaml:"text" json:"text"`
}

// TestRow is one recorded test. The rows are a stack, oldest first: pass and
// fail push one, and retest removes the last.
//
// A failed row carries at least one check and the fail's note; a passed one
// carries neither. By is the declared source, as on ActionReceipt. Commit is
// outputs.commit at the test, empty when the ticket recorded none: it is what
// ties the test to the landing it was of. At is a string for the reason
// VerdictRow.At is.
type TestRow struct {
	Result TestResult  `yaml:"result" json:"result"`
	Checks []TestCheck `yaml:"checks,omitempty" json:"checks,omitempty"`
	Note   string      `yaml:"note,omitempty" json:"note,omitempty"`
	At     string      `yaml:"at" json:"at"`
	By     string      `yaml:"by" json:"by"`
	Commit string      `yaml:"commit,omitempty" json:"commit,omitempty"`
}

// MarshalYAML writes the row with its free text quoted.
func (r TestRow) MarshalYAML() (any, error) {
	return struct {
		Result TestResult  `yaml:"result"`
		Checks []TestCheck `yaml:"checks,omitempty"`
		Note   quoted      `yaml:"note,omitempty"`
		At     string      `yaml:"at"`
		By     quoted      `yaml:"by"`
		Commit quoted      `yaml:"commit,omitempty"`
	}{r.Result, r.Checks, quoted(r.Note), r.At, quoted(r.By), quoted(r.Commit)}, nil
}

// MarshalYAML writes the check with its text quoted.
func (c TestCheck) MarshalYAML() (any, error) {
	return struct {
		Index int    `yaml:"index"`
		Text  quoted `yaml:"text"`
	}{c.Index, quoted(c.Text)}, nil
}

// quoted is text written as a double-quoted scalar on one line. yaml.v3 writes
// text holding a line break as a literal block, where a `---` line is read back
// as the end of the frontmatter, and text opening with a tab or a blank line is
// refused or reads back altered.
type quoted string

func (q quoted) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: string(q)}
	// Bytes that are not UTF-8 are written as base64, which needs the tag left
	// open and reads back as the same bytes.
	if utf8.ValidString(n.Value) {
		n.Tag = "!!str"
	}
	return n, nil
}

// status is what the ticket holds once the row is recorded, or "" for a result
// tk never wrote.
func (r TestRow) status() Status {
	switch r.Result {
	case TestPassed:
		return StatusDone
	case TestFailed:
		return StatusReady
	}
	return ""
}

func testRowsEqual(a, b TestRow) bool {
	return a.Result == b.Result && slices.Equal(a.Checks, b.Checks) && a.Note == b.Note &&
		a.At == b.At && a.By == b.By && a.Commit == b.Commit
}

// LastTest is the last test standing on the ticket, or nil when none does. The
// pointer is into t's own rows.
func LastTest(t *Ticket) *TestRow {
	if len(t.Tests) == 0 {
		return nil
	}
	return &t.Tests[len(t.Tests)-1]
}

// testCriteria is the criteria of body a person checks, in parse order: what a
// fail's checks index.
func testCriteria(body string) []Criterion {
	var criteria []Criterion
	for _, c := range ParseCriteria(AcceptanceCriteria(body)) {
		if c.Kind() == CriterionTest {
			criteria = append(criteria, c)
		}
	}
	return criteria
}

// LandingStatus is the status a ticket takes when its change lands: testing
// while any of its criteria is a person's to check, otherwise done.
func LandingStatus(t *Ticket) Status {
	if len(testCriteria(t.Body)) > 0 {
		return StatusTesting
	}
	return StatusDone
}

// normalizeChecks sorts a fail's checks into criterion order, refusing an empty
// list, an index below 1 and one named twice. The range is judged against the
// ticket, by namedChecks.
func normalizeChecks(checks []int) ([]int, error) {
	if len(checks) == 0 {
		return nil, fmt.Errorf("fail names no failed criterion: at least one check is required, by its index among the ticket's test: criteria")
	}
	sorted := slices.Sorted(slices.Values(checks))
	if sorted[0] < 1 || len(slices.Compact(slices.Clone(sorted))) != len(sorted) {
		return nil, fmt.Errorf("checks %v are not distinct positive indexes", checks)
	}
	return sorted, nil
}

// namedChecks reads normalized checks against the ticket's test: criteria.
func namedChecks(t *Ticket, checks []int) ([]TestCheck, error) {
	criteria := testCriteria(t.Body)
	var named []TestCheck
	for _, n := range checks {
		if n > len(criteria) {
			return nil, fmt.Errorf("check %d is out of range: %s has %d test: criteria", n, t.ID, len(criteria))
		}
		named = append(named, TestCheck{Index: n, Text: criteria[n-1].Text})
	}
	return named, nil
}

// testRefusal is why pass, fail or retest does not apply to t, or nil. The
// status is judged before a fail's checks are read against the criteria.
//
// Retest undoes a test only while the ticket still holds what the test left:
// its status, and the commit the test was of, so that a ticket landed again
// since is not moved by a test of the earlier landing.
func testRefusal(t *Ticket, tr Transition) error {
	if t.Type == TypeEpic {
		return fmt.Errorf("%w: %s on epic %s — an epic's status is derived from its children; test the child that landed", ErrTransitionNotPermitted, tr, t.ID)
	}
	if tr != TransitionRetest {
		if t.Status != StatusTesting {
			return fmt.Errorf("%w: %s on %s, which is %s and not in testing", ErrTransitionNotPermitted, tr, t.ID, t.Status)
		}
		return nil
	}
	last := LastTest(t)
	switch {
	case last == nil:
		return fmt.Errorf("%w: retest on %s, which has no test to undo", ErrTransitionNotPermitted, t.ID)
	case t.Status != last.status():
		return fmt.Errorf("%w: retest on %s, which is %s and no longer holds the status its last test (%s) left", ErrTransitionNotPermitted, t.ID, t.Status, last.Result)
	case t.Outputs[OutputKeyCommit] != last.Commit:
		return fmt.Errorf("%w: retest on %s, whose last test was of commit %q and which now records %q", ErrTransitionNotPermitted, t.ID, last.Commit, t.Outputs[OutputKeyCommit])
	}
	return nil
}
