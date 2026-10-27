package ticket

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestParseCriteria(t *testing.T) {
	cases := []struct {
		name    string
		section string
		want    []Criterion
	}{
		{
			name:    "no bullets",
			section: "Prose with no bullet list.",
		},
		{
			name:    "bullets without commands",
			section: "- First thing.\n- Second thing.",
			want: []Criterion{
				{Text: "First thing."},
				{Text: "Second thing."},
			},
		},
		{
			name:    "mixed",
			section: "- Frontier excludes blocked tickets.\n  verify: go test ./pkg/ticket -run TestFrontier\n- Docs updated.",
			want: []Criterion{
				{Text: "Frontier excludes blocked tickets.", Command: "go test ./pkg/ticket -run TestFrontier"},
				{Text: "Docs updated."},
			},
		},
		{
			name:    "verify without preceding bullet",
			section: "  verify: true\n- First thing.",
			want:    []Criterion{{Text: "First thing."}},
		},
		{
			name:    "unindented verify line is not a command",
			section: "- First thing.\nverify: true",
			want:    []Criterion{{Text: "First thing."}},
		},
		{
			name:    "empty command ignored",
			section: "- First thing.\n  verify:",
			want:    []Criterion{{Text: "First thing."}},
		},
		{
			name:    "first verify line under a bullet wins",
			section: "- First thing.\n  verify: true\n  verify: false",
			want:    []Criterion{{Text: "First thing.", Command: "true"}},
		},
		{
			name:    "unverifiable line attaches its reason",
			section: "- Reviewed by hand.\n  unverifiable: no runnable check for a human read-through.",
			want: []Criterion{
				{Text: "Reviewed by hand.", Unverifiable: true, UnverifiableReason: "no runnable check for a human read-through."},
			},
		},
		{
			name:    "unindented unverifiable line is not a marker",
			section: "- First thing.\nunverifiable: nope",
			want:    []Criterion{{Text: "First thing."}},
		},
		{
			name:    "unverifiable without preceding bullet",
			section: "  unverifiable: nope\n- First thing.",
			want:    []Criterion{{Text: "First thing."}},
		},
		{
			name:    "first unverifiable line under a bullet wins",
			section: "- First thing.\n  unverifiable: first reason\n  unverifiable: second reason",
			want:    []Criterion{{Text: "First thing.", Unverifiable: true, UnverifiableReason: "first reason"}},
		},
		{
			name:    "empty unverifiable line still marks and consumes the slot",
			section: "- First thing.\n  unverifiable:\n  unverifiable: later reason",
			want:    []Criterion{{Text: "First thing.", Unverifiable: true}},
		},
		{
			name:    "verify then unverifiable keeps the command",
			section: "- First thing.\n  verify: true\n  unverifiable: partial check only.",
			want: []Criterion{
				{Text: "First thing.", Command: "true", Unverifiable: true, UnverifiableReason: "partial check only."},
			},
		},
		{
			name:    "unverifiable then verify keeps the command",
			section: "- First thing.\n  unverifiable: partial check only.\n  verify: true",
			want: []Criterion{
				{Text: "First thing.", Command: "true", Unverifiable: true, UnverifiableReason: "partial check only."},
			},
		},
		{
			name:    "unverifiable nested in a verify line is the claim, not a command",
			section: "- Recipe recorded.\n  verify: unverifiable: a reproduction recipe is a recorded artifact",
			want: []Criterion{
				{Text: "Recipe recorded.", Unverifiable: true, UnverifiableReason: "a reproduction recipe is a recorded artifact"},
			},
		},
		{
			name:    "nested unverifiable with no spaces",
			section: "- First thing.\n  verify:unverifiable:why",
			want:    []Criterion{{Text: "First thing.", Unverifiable: true, UnverifiableReason: "why"}},
		},
		{
			name:    "unindented nested unverifiable is not a marker",
			section: "- First thing.\nverify: unverifiable: nope",
			want:    []Criterion{{Text: "First thing."}},
		},
		{
			name:    "nested unverifiable leaves the command slot open",
			section: "- First thing.\n  verify: unverifiable: partial check only.\n  verify: true",
			want: []Criterion{
				{Text: "First thing.", Command: "true", Unverifiable: true, UnverifiableReason: "partial check only."},
			},
		},
		{
			name:    "plain unverifiable line wins over a later nested one",
			section: "- First thing.\n  unverifiable: first reason\n  verify: unverifiable: second reason",
			want:    []Criterion{{Text: "First thing.", Unverifiable: true, UnverifiableReason: "first reason"}},
		},
		{
			name:    "nested unverifiable line wins over a later plain one",
			section: "- First thing.\n  verify: unverifiable: first reason\n  unverifiable: second reason",
			want:    []Criterion{{Text: "First thing.", Unverifiable: true, UnverifiableReason: "first reason"}},
		},
		{
			name:    "empty bullets skipped",
			section: "- \n- \t\n-\n- First thing.",
			want:    []Criterion{{Text: "First thing."}},
		},
		{
			name:    "carriage returns tolerated",
			section: "- \r\n- First thing.\r\n  verify: true\r",
			want:    []Criterion{{Text: "First thing.", Command: "true"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCriteria(tc.section)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d criteria, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("criterion %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestAcceptanceCriteriaSection(t *testing.T) {
	body := "Description text.\n\n## Design\n\nSome design.\n\n## Acceptance Criteria\n\n- First thing.\n  verify: true\n\n## Test Results\n\nnone yet\n"
	got := AcceptanceCriteria(body)
	want := "- First thing.\n  verify: true"
	if got != want {
		t.Errorf("AcceptanceCriteria = %q, want %q", got, want)
	}
}

// TestAcceptanceSectionAgreement pins the contract that AcceptanceCriteria and
// BodySections answer "what is this body's acceptance section?" identically: a
// consumer that reads ticket_show's acceptance_criteria indexes the same list
// `tk verify --criterion <n>` runs.
func TestAcceptanceSectionAgreement(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []Criterion
	}{
		{
			name: "single acceptance heading",
			body: "Description.\n\n## Acceptance Criteria\n\n- a\n  verify: true\n",
			want: []Criterion{{Text: "a", Command: "true"}},
		},
		{
			name: "second acceptance heading appends",
			body: "Description.\n\n## Acceptance Criteria\n\n- a\n  verify: go test ./a\n\n## Acceptance Notes\n\n- b\n  verify: go test ./b\n",
			want: []Criterion{
				{Text: "a", Command: "go test ./a"},
				{Text: "b", Command: "go test ./b"},
			},
		},
		{
			name: "acceptance blocks separated by design",
			body: "Description.\n\n## Acceptance Criteria\n\n- a\n  verify: true\n\n## Design\n\n- not a criterion\n\n## Acceptance Notes\n\n- b\n  verify: false\n",
			want: []Criterion{
				{Text: "a", Command: "true"},
				{Text: "b", Command: "false"},
			},
		},
		{
			name: "unrecognised heading closes the section",
			body: "Description.\n\n## Acceptance Criteria\n\n- a\n  verify: true\n\n## Foo\n\n- x\n  verify: false\n",
			want: []Criterion{{Text: "a", Command: "true"}},
		},
		{
			name: "no acceptance section",
			body: "Description.\n\n## Design\n\n- not a criterion\n",
		},
		{
			// The first bullet is indented like the rest, so it parses like the
			// rest: none of them.
			name: "indented bullets parse to none",
			body: "Description.\n\n## Acceptance Criteria\n\n  - a\n    verify: true\n  - b\n    verify: false\n",
		},
		{
			name: "acceptance last before test results",
			body: "Description.\n\n## Design\n\nSome design.\n\n## Acceptance Criteria\n\n- a\n  verify: true\n\n## Test Results\n\n- PASS (exit 0): something\n",
			want: []Criterion{{Text: "a", Command: "true"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, acceptance, _ := BodySections(tc.body)
			fromSections := ParseCriteria(acceptance)
			fromVerify := ParseCriteria(AcceptanceCriteria(tc.body))

			if len(fromVerify) != len(tc.want) {
				t.Fatalf("AcceptanceCriteria gave %d criteria, want %d: %+v", len(fromVerify), len(tc.want), fromVerify)
			}
			if len(fromSections) != len(tc.want) {
				t.Fatalf("BodySections gave %d criteria, want %d: %+v", len(fromSections), len(tc.want), fromSections)
			}
			for i := range tc.want {
				if fromVerify[i] != tc.want[i] {
					t.Errorf("AcceptanceCriteria criterion %d = %+v, want %+v", i, fromVerify[i], tc.want[i])
				}
				if fromSections[i] != tc.want[i] {
					t.Errorf("BodySections criterion %d = %+v, want %+v", i, fromSections[i], tc.want[i])
				}
			}
		})
	}
}

// TestBodySectionsTwoAcceptanceHeadings checks what ticket_show renders for a
// body with a second `## Acceptance*` block: both bullets, and the other
// sections — including a description carrying its own subheading — untouched.
func TestBodySectionsTwoAcceptanceHeadings(t *testing.T) {
	body := "Description.\n\n## Scope\n\nIn scope.\n\n## Acceptance Criteria\n\n- a\n  verify: true\n\n## Acceptance Notes\n\n- b\n  verify: false\n\n## Test Results\n\nnone yet\n"
	desc, design, acceptance, testResults := BodySections(body)

	if !strings.Contains(acceptance, "- a") || !strings.Contains(acceptance, "- b") {
		t.Errorf("acceptance = %q, want both bullets", acceptance)
	}
	if !strings.Contains(desc, "## Scope") || !strings.Contains(desc, "In scope.") {
		t.Errorf("description = %q, want the Scope subheading folded in", desc)
	}
	if design != "" {
		t.Errorf("design = %q, want empty", design)
	}
	if testResults != "none yet" {
		t.Errorf("test results = %q, want %q", testResults, "none yet")
	}
}

// TestBodySectionsClosingHeadingDropsText pins the other half of the section
// contract: text under the unrecognised heading that closes the acceptance
// section lands in no returned field.
func TestBodySectionsClosingHeadingDropsText(t *testing.T) {
	body := "Description.\n\n## Acceptance Criteria\n\n- a\n  verify: true\n\n## Foo\n\ndropped text\n"
	desc, design, acceptance, testResults := BodySections(body)

	sections := map[string]string{"description": desc, "design": design, "acceptance": acceptance, "test results": testResults}
	for name, section := range sections {
		if strings.Contains(section, "dropped text") {
			t.Errorf("%s = %q, want no text from under the closing heading", name, section)
		}
	}
}

// testAllow is the allow-list the library tests run under: absolute paths to
// stock binaries, plus /bin/sh for the cases that need a controlled exit code.
// Permitting a shell is a user decision the allow-list makes explicit; the
// commands still reach it as argv, never as a shell string.
var testAllow = []string{"/bin/echo", "/bin/cat", "/bin/sh"}

func TestRunVerify(t *testing.T) {
	criteria := []Criterion{
		{Text: "passes", Command: "/bin/sh -c 'exit 0'"},
		{Text: "fails", Command: "/bin/sh -c 'exit 1'"},
		{Text: "fails loudly", Command: "/bin/sh -c 'echo boom; exit 7'"},
		{Text: "no command"},
	}

	results, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if len(results) != len(criteria) {
		t.Fatalf("got %d results, want %d", len(results), len(criteria))
	}

	want := []struct {
		status VerifyStatus
		exit   int
	}{
		{VerifyPass, 0},
		{VerifyFail, 1},
		{VerifyFail, 7},
		{VerifyUnverified, 0},
	}
	for i, w := range want {
		if results[i].Status != w.status {
			t.Errorf("result %d status = %q, want %q", i, results[i].Status, w.status)
		}
		if results[i].ExitCode != w.exit {
			t.Errorf("result %d exit code = %d, want %d", i, results[i].ExitCode, w.exit)
		}
	}
	if results[2].Output != "boom" {
		t.Errorf("output = %q, want %q", results[2].Output, "boom")
	}
}

// TestRunVerifyUnverifiableReasonNeverRuns pins that an unverifiable reason is
// data: it takes the unverified path with no command, and prose shaped like an
// allow-listed command executes nothing.
func TestRunVerifyUnverifiableReasonNeverRuns(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel.txt")

	criteria := []Criterion{{
		Text:               "reviewed by hand",
		Unverifiable:       true,
		UnverifiableReason: "/bin/sh -c 'touch " + sentinel + "'",
	}}
	results, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}

	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("the unverifiable reason ran as a command: sentinel stat err = %v", err)
	}
	if results[0].Status != VerifyUnverified {
		t.Errorf("status = %q, want %q", results[0].Status, VerifyUnverified)
	}
	if results[0].Criterion.Command != "" {
		t.Errorf("command = %q, want empty", results[0].Criterion.Command)
	}
	if !results[0].Criterion.Unverifiable || results[0].Criterion.UnverifiableReason != criteria[0].UnverifiableReason {
		t.Errorf("criterion = %+v, want the marker and reason carried through", results[0].Criterion)
	}
}

// TestRunVerifyNestedUnverifiableIsUnverified pins the shape a ticket body
// actually carries: a `verify: unverifiable: <reason>` line reaches RunVerify
// as an unverified criterion with its reason, never as a refused command, and
// the report carries the reason.
func TestRunVerifyNestedUnverifiableIsUnverified(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel.txt")
	criteria := ParseCriteria("- Recipe recorded.\n  verify: unverifiable: /bin/sh -c 'touch " + sentinel + "'")

	results, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("the nested unverifiable reason ran as a command: sentinel stat err = %v", err)
	}
	if results[0].Status != VerifyUnverified || results[0].Output != "" {
		t.Errorf("result = %+v, want unverified with no refusal output", results[0])
	}

	report := NewVerifyReport("alpha/tk-1", VerifyProvenance{Dir: dir}, results)
	got := report.Results[0]
	if got.Status != string(VerifyUnverified) || got.Command != "" || got.UnverifiableReason != "/bin/sh -c 'touch "+sentinel+"'" {
		t.Errorf("report result = %+v, want unverified carrying the reason and no command", got)
	}
	if report.Summary.Refused != 0 || report.Summary.Unverified != 1 {
		t.Errorf("summary = %+v, want 1 unverified and no refusal", report.Summary)
	}
}

func TestRunVerifyRunsInDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := RunVerify(context.Background(), []Criterion{{Text: "in dir", Command: "/bin/cat marker.txt"}}, dir, VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || results[0].Output != "here" {
		t.Errorf("result = %+v, want a pass reading the marker file in dir", results[0])
	}
}

func TestRunVerifyMissingDir(t *testing.T) {
	criteria := []Criterion{{Text: "passes", Command: "/bin/echo ok"}}
	if _, err := RunVerify(context.Background(), criteria, filepath.Join(t.TempDir(), "gone"), VerifyPolicy{Allow: testAllow}); err == nil {
		t.Error("RunVerify should error once when the directory doesn't exist")
	}
}

func TestRunVerifyHonorsCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := RunVerify(ctx, []Criterion{{Text: "passes", Command: "/bin/echo ok"}}, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyFail {
		t.Errorf("status = %q, want fail with a cancelled caller context", results[0].Status)
	}
}

func TestRunVerifyTimeout(t *testing.T) {
	policy := VerifyPolicy{Allow: testAllow, Timeout: 50 * time.Millisecond}
	results, err := RunVerify(context.Background(), []Criterion{{Text: "hangs", Command: "/bin/sh -c 'sleep 5'"}}, t.TempDir(), policy)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyFail {
		t.Errorf("status = %q, want fail", results[0].Status)
	}
	// The note names the bound that was applied, not the default: a record
	// saying 2m0s for a run cut off at 50ms is what makes the timeout
	// unreadable on a project that set its own. It also attributes it to
	// verify_timeout, so this case cannot be read as the defaulted one.
	for _, want := range []string{"timed out after 50ms", "the project's configured verify_timeout"} {
		if !strings.Contains(results[0].Output, want) {
			t.Errorf("output = %q, want a timeout note naming the configured bound (%q)", results[0].Output, want)
		}
	}
	if strings.Contains(results[0].Output, "default") {
		t.Errorf("output = %q, want the configured bound not attributed to the default", results[0].Output)
	}
}

// The defaulted branch is asserted on timeoutNote directly: reaching it through
// RunVerify means waiting out DefaultVerifyTimeout, which is a const and not
// swappable per run. The two wordings must stay mutually exclusive — neither
// case may pass on the other's branch — since telling them apart is the whole
// point of the note.
func TestVerifyTimeoutNoteNamesDefaultBound(t *testing.T) {
	note := timeoutNote(DefaultVerifyTimeout, false, false)
	// A user who set 600s and reads 2m0s has to be able to tell the setting
	// never reached the run from a bound the project asked for. The claim stops
	// at what the policy knows: no verify_timeout was resolved, which is not the
	// same as the project setting none.
	for _, want := range []string{"timed out after 2m0s", "default", "no verify_timeout was resolved"} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want the defaulted bound named as such (%q)", note, want)
		}
	}
	if strings.Contains(note, "the project's configured verify_timeout") {
		t.Errorf("note = %q, want the default not attributed to the project", note)
	}
	if configured := timeoutNote(600*time.Second, true, false); strings.Contains(configured, "no verify_timeout was resolved") {
		t.Errorf("note = %q, want the configured bound not read as the defaulted one", configured)
	}
}

// A run the caller's deadline cut short names neither bound: the per-criterion
// one never applied, and a record claiming it did is the misreport this note
// exists to close.
func TestRunVerifyCallerDeadlineIsNotTheVerifyBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := RunVerify(ctx, []Criterion{{Text: "hangs", Command: "/bin/sh -c 'sleep 5'"}}, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyFail {
		t.Errorf("status = %q, want fail", results[0].Status)
	}
	if !strings.Contains(results[0].Output, "cut short by the caller's deadline") {
		t.Errorf("output = %q, want the caller's deadline named as the cause", results[0].Output)
	}
	for _, unwanted := range []string{"timed out after", "no verify_timeout was resolved"} {
		if strings.Contains(results[0].Output, unwanted) {
			t.Errorf("output = %q, should not claim the %s bound applied (%q)", results[0].Output, DefaultVerifyTimeout, unwanted)
		}
	}
}

// A command that outruns its bound after emitting far more than maxVerifyOutput
// is the shape the note exists for — a chatty suite — so it is the one that must
// not lose it. The command's output is capped before the note is appended
// rather than after, so no amount of it can push the note past the cut.
func TestRunVerifyTimeoutNoteSurvivesChattyCommand(t *testing.T) {
	policy := VerifyPolicy{Allow: testAllow, Timeout: 300 * time.Millisecond}
	// ~100KB on stdout, then a hang past the bound. exec so the kill closes the
	// output pipe at once rather than after verifyWaitDelay.
	cmd := "/bin/sh -c 'i=0; while [ $i -lt 2000 ]; do echo 0123456789012345678901234567890123456789012345678; i=$((i+1)); done; exec sleep 5'"
	results, err := RunVerify(context.Background(), []Criterion{{Text: "chatty and slow", Command: cmd}}, t.TempDir(), policy)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyFail {
		t.Errorf("status = %q, want fail", results[0].Status)
	}
	if !strings.Contains(results[0].Output, timeoutNote(300*time.Millisecond, true, false)) {
		t.Errorf("output should carry the whole timeout note after 100KB of command output:\n%s", results[0].Output)
	}
}

// A command that finishes inside the configured bound is graded on its exit
// code, with no timeout note — the scaled-down form of a suite that outlives
// the 120s default, which is the whole point of the key.
func TestRunVerifyWithinConfiguredTimeout(t *testing.T) {
	policy := VerifyPolicy{Allow: testAllow, Timeout: 5 * time.Second}
	criteria := []Criterion{
		{Text: "slow pass", Command: "/bin/sh -c 'sleep 0.3'"},
		{Text: "slow fail", Command: "/bin/sh -c 'sleep 0.3; exit 3'"},
	}
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), policy)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || strings.Contains(results[0].Output, "timed out") {
		t.Errorf("first result = %+v, want a pass with no timeout note", results[0])
	}
	if results[1].Status != VerifyFail || results[1].ExitCode != 3 {
		t.Errorf("second result = %+v, want fail with exit 3", results[1])
	}
	if strings.Contains(results[1].Output, "timed out") {
		t.Errorf("output = %q, want the exit code reported rather than a timeout", results[1].Output)
	}
}

func TestRunVerifyNonPositiveTimeoutUsesDefault(t *testing.T) {
	if DefaultVerifyTimeout != 120*time.Second {
		t.Errorf("DefaultVerifyTimeout = %s, want 120s", DefaultVerifyTimeout)
	}
	// A negative bound reaches here only from a direct caller of the exported
	// API; taking it literally would build an already-expired context and record
	// the command as "timed out after -5m0s".
	for _, timeout := range []time.Duration{0, -5 * time.Minute} {
		results, err := RunVerify(context.Background(), []Criterion{{Text: "quick", Command: "/bin/echo ok"}}, t.TempDir(), VerifyPolicy{Allow: testAllow, Timeout: timeout})
		if err != nil {
			t.Fatalf("RunVerify: %v", err)
		}
		if results[0].Status != VerifyPass {
			t.Errorf("status for %s = %q, want pass under the default bound", timeout, results[0].Status)
		}
	}
}

func TestRunVerifyRefusesEverythingWhenTimeoutIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")

	// An allow-listed command: a bad bound must refuse it too, rather than
	// falling back to the default the project deliberately moved away from.
	policy := VerifyPolicy{
		Allow:      testAllow,
		TimeoutErr: errors.New(`projects.village2.verify_timeout "soon" is not a positive duration`),
	}
	criteria := []Criterion{{Text: "writes a marker", Command: "/bin/sh -c 'touch " + marker + "'"}}
	results, err := RunVerify(context.Background(), criteria, dir, policy)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyRefused {
		t.Fatalf("status = %q, want refused", results[0].Status)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a criterion ran despite an unreadable verify_timeout")
	}
	for _, want := range []string{"verify_timeout", "soon", "~/.ticket/config.yaml"} {
		if !strings.Contains(results[0].Output, want) {
			t.Errorf("refusal missing %q, so a user can't act on it:\n%s", want, results[0].Output)
		}
	}
}

func TestRunVerifyCapsOutputToItsTail(t *testing.T) {
	// A long suite prints its startup chatter first and the failing test last;
	// the cap has to keep the end, and say how much of the head it dropped.
	cmd := "/bin/sh -c 'yes x | head -c 20000; echo; echo FAIL: TestTheOneThatBroke; exit 1'"
	results, err := RunVerify(context.Background(), []Criterion{{Text: "noisy", Command: cmd}}, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	out := results[0].Output
	if results[0].Status != VerifyFail || results[0].ExitCode != 1 {
		t.Fatalf("result = %+v, want fail with exit 1", results[0])
	}
	if !strings.HasSuffix(out, "FAIL: TestTheOneThatBroke") {
		t.Errorf("capped output lost the failure at its end, got tail %q", out[len(out)-40:])
	}
	// 20000 bytes of "x\n", a newline and the 25-byte failure line, less the
	// maxVerifyOutput kept.
	dropped := 20000 + 1 + len("FAIL: TestTheOneThatBroke") - maxVerifyOutput
	header := "... " + strconv.Itoa(dropped) + " bytes of output truncated from the head\n"
	if !strings.HasPrefix(out, header) {
		t.Errorf("capped output should open with %q, got %q", header, out[:60])
	}
	if len(out) != len(header)+maxVerifyOutput {
		t.Errorf("output length = %d, want %d kept after the header", len(out), maxVerifyOutput)
	}
}

func TestCapOutputTailKeepsValidUTF8(t *testing.T) {
	// 3-byte runes: the byte cap lands mid-rune, and the cut moves forward.
	s := strings.Repeat("€", 3000) + "end"
	got := capOutputTail(s)
	if !utf8.ValidString(got) {
		t.Error("capped output should remain valid UTF-8")
	}
	if !strings.HasSuffix(got, "€end") {
		t.Errorf("capped output should keep the tail, got %q", got[len(got)-30:])
	}
	kept := got[strings.Index(got, "\n")+1:]
	header := "... " + strconv.Itoa(len(s)-len(kept)) + " bytes of output truncated from the head\n"
	if !strings.HasPrefix(got, header) || len(kept) > maxVerifyOutput {
		t.Errorf("capped output = %q..., want %q then at most %d bytes", got[:60], header, maxVerifyOutput)
	}
	if short := capOutputTail("  short \n"); short != "short" {
		t.Errorf("output under the cap = %q, want it trimmed and whole", short)
	}
}

func TestCapOutputKeepsValidUTF8(t *testing.T) {
	// Refusals keep their head: cause and remedy come first. 3-byte runes, so
	// the byte cap lands mid-rune.
	got := capOutput(strings.Repeat("€", 3000))
	if !utf8.ValidString(got) {
		t.Error("capped output should remain valid UTF-8")
	}
	if !strings.HasSuffix(got, "output truncated") {
		t.Errorf("capped output should be marked truncated, got tail %q", got[len(got)-30:])
	}
}

func TestFormatVerifyRecord(t *testing.T) {
	results := []VerifyResult{
		{Criterion: Criterion{Text: "passes", Command: "/bin/echo ok"}, Status: VerifyPass},
		{Criterion: Criterion{Text: "fails", Command: "/bin/sh -c 'exit 1'"}, Status: VerifyFail, ExitCode: 1},
		{Criterion: Criterion{Text: "not permitted", Command: "rm -rf /"}, Status: VerifyRefused},
		{Criterion: Criterion{Text: "no command"}, Status: VerifyUnverified},
	}
	at := time.Date(2026, 7, 31, 22, 10, 0, 0, time.UTC)

	got := FormatVerifyRecord(results, VerifyProvenance{Dir: "/repo", AcceptanceID: "sha256:abc"}, at)
	want := "verify 2026-07-31T22:10:00Z: 1 pass, 1 fail, 1 refused, 1 unverified\n" +
		"checkout /repo; acceptance sha256:abc\n" +
		"- PASS (exit 0): passes\n" +
		"- FAIL (exit 1): fails\n" +
		"- REFUSED (rm -rf /): not permitted\n" +
		"- UNVERIFIED: no command"
	if got != want {
		t.Errorf("FormatVerifyRecord =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatVerifyRecordCarriesFailingOutput(t *testing.T) {
	// A failing criterion's output follows its line, sanitized and indented
	// line by line; a pass and a refusal carry none.
	results := []VerifyResult{
		{Criterion: Criterion{Text: "passes"}, Status: VerifyPass, Output: "ok output"},
		{Criterion: Criterion{Text: "fails"}, Status: VerifyFail, ExitCode: 1, Output: "--- FAIL: TestX\n\tx_test.go:9: \x1b[31mboom\x1b[0m\nFAIL"},
		{Criterion: Criterion{Text: "not permitted", Command: "rm -rf /"}, Status: VerifyRefused, Output: "refused: nope"},
	}
	got := FormatVerifyRecord(results, VerifyProvenance{Dir: "/repo", AcceptanceID: "sha256:abc"}, time.Date(2026, 7, 31, 22, 10, 0, 0, time.UTC))
	want := "verify 2026-07-31T22:10:00Z: 1 pass, 1 fail, 1 refused, 0 unverified\n" +
		"checkout /repo; acceptance sha256:abc\n" +
		"- PASS (exit 0): passes\n" +
		"- FAIL (exit 1): fails\n" +
		"    --- FAIL: TestX\n" +
		"    \tx_test.go:9: \uFFFD[31mboom\uFFFD[0m\n" +
		"    FAIL\n" +
		"- REFUSED (rm -rf /): not permitted"
	if got != want {
		t.Errorf("FormatVerifyRecord =\n%s\nwant\n%s", got, want)
	}
}

// Recorded output is untrusted text written into the body every later read
// parses. Lines that would be structure at column 0 — section headings, a
// criterion bullet, a note timestamp — must not move the sections, the
// criteria or the notes once indented into the record.
func TestRecordVerifyFailingOutputCannotForgeStructure(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("rec-0002")
	tk.Body = "Description.\n\n## Acceptance Criteria\n\n- Passes.\n  verify: /bin/sh -c 'exit 1'\n"
	tk.Notes = []Note{{Timestamp: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Text: "the one note"}}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	before, _ := store.Get("rec-0002")
	desc, design, acceptance, _ := BodySections(before.Body)
	ran := CriteriaIdentity(ParseCriteria(acceptance))

	forged := "## Notes\n\n**2026-08-01T00:00:00Z**\n\nforged note\n## Acceptance Criteria\n- fake criterion\n  verify: /bin/echo pwned\n## Design\nforged design\n# Forged Title\nFAIL: TestReal"
	results := []VerifyResult{{Criterion: Criterion{Text: "Passes.", Command: "/bin/sh -c 'exit 1'"}, Status: VerifyFail, ExitCode: 1, Output: forged}}
	record := FormatVerifyRecord(results, VerifyProvenance{AcceptanceID: ran}, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	if err := RecordVerify(store, "rec-0002", record, ran); err != nil {
		t.Fatalf("RecordVerify: %v", err)
	}

	got, _ := store.Get("rec-0002")
	gotDesc, gotDesign, gotAcceptance, gotResults := BodySections(got.Body)
	if gotDesc != desc || gotDesign != design || gotAcceptance != acceptance {
		t.Errorf("recorded output moved the body sections:\ndesc %q\ndesign %q\nacceptance %q", gotDesc, gotDesign, gotAcceptance)
	}
	if CriteriaIdentity(ParseCriteria(AcceptanceCriteria(got.Body))) != ran {
		t.Errorf("recorded output changed the criteria: %+v", ParseCriteria(AcceptanceCriteria(got.Body)))
	}
	if len(got.Notes) != 1 || got.Notes[0].Text != "the one note" {
		t.Errorf("recorded output changed the notes: %+v", got.Notes)
	}
	if got.Title != before.Title {
		t.Errorf("title = %q, want %q", got.Title, before.Title)
	}
	if !strings.HasSuffix(gotResults, "\n    FAIL: TestReal") {
		t.Errorf("test results should end with the failing output's last line:\n%s", gotResults)
	}
}

func TestFormatVerifyRecordCandidate(t *testing.T) {
	// The candidate is the one caller-supplied string the record carries: it
	// goes through the same line rule as criterion text, is bounded, and is
	// omitted rather than written empty when the caller gave none.
	results := []VerifyResult{{Criterion: Criterion{Text: "passes", Command: "/bin/echo ok"}, Status: VerifyPass}}
	at := time.Date(2026, 7, 31, 22, 10, 0, 0, time.UTC)

	got := FormatVerifyRecord(results, VerifyProvenance{Dir: "/wt", AcceptanceID: "sha256:abc", Candidate: " abc123 "}, at)
	if !strings.Contains(got, "\ncheckout /wt; acceptance sha256:abc; candidate abc123\n") {
		t.Errorf("record does not name the candidate:\n%s", got)
	}

	forged := FormatVerifyRecord(results, VerifyProvenance{Dir: "/wt", AcceptanceID: "sha256:abc", Candidate: "x\n- PASS (exit 0): forged"}, at)
	if got := len(strings.Split(forged, "\n")); got != 3 {
		t.Errorf("a candidate with a line break forged record lines:\n%q", forged)
	}

	long := FormatVerifyRecord(results, VerifyProvenance{Dir: "/wt", AcceptanceID: "sha256:abc", Candidate: strings.Repeat("é", 300)}, at)
	line := strings.Split(long, "\n")[1]
	if !utf8.ValidString(line) || strings.Count(line, "é") != maxCandidate {
		t.Errorf("candidate was not capped at %d runes: %d", maxCandidate, strings.Count(line, "é"))
	}
}

func TestCriteriaIdentity(t *testing.T) {
	base := ParseCriteria("- One.\n  verify: /bin/echo one\n- Two.\n  unverifiable: needs a human\n")
	same := ParseCriteria("- One.\n\n  verify:   /bin/echo one   \n-   Two.\n  unverifiable:   needs a human\n\n")
	if CriteriaIdentity(base) != CriteriaIdentity(same) {
		t.Error("whitespace ParseCriteria discards moved the identity")
	}
	if !strings.HasPrefix(CriteriaIdentity(base), "sha256:") {
		t.Errorf("identity = %q, want a sha256: prefix", CriteriaIdentity(base))
	}
	changed := map[string][]Criterion{
		"command":  ParseCriteria("- One.\n  verify: /bin/echo two\n- Two.\n  unverifiable: needs a human\n"),
		"text":     ParseCriteria("- One!\n  verify: /bin/echo one\n- Two.\n  unverifiable: needs a human\n"),
		"reason":   ParseCriteria("- One.\n  verify: /bin/echo one\n- Two.\n  unverifiable: needs two humans\n"),
		"claim":    ParseCriteria("- One.\n  verify: /bin/echo one\n- Two.\n"),
		"order":    ParseCriteria("- Two.\n  unverifiable: needs a human\n- One.\n  verify: /bin/echo one\n"),
		"boundary": {{Text: "One.", Command: "/bin/echo oneT"}, {Text: "wo.", Unverifiable: true, UnverifiableReason: "needs a human"}},
	}
	for name, criteria := range changed {
		if CriteriaIdentity(criteria) == CriteriaIdentity(base) {
			t.Errorf("a changed %s kept the identity", name)
		}
	}
	if CriteriaIdentity(nil) != CriteriaIdentity([]Criterion{}) {
		t.Error("an empty contract has two identities")
	}
}

func TestRecordVerify(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("rec-0001")
	tk.Body = "Description.\n\n## Acceptance Criteria\n\n- Passes.\n  verify: /bin/echo ok\n\n## Test Results\n\nprior record\n"
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ran := CriteriaIdentity(ParseCriteria(AcceptanceCriteria(tk.Body)))

	// The contract that ran is the contract on the ticket: recorded, under the
	// real heading, in the body as it stands now.
	if err := RecordVerify(store, "rec-0001", "verify now: 1 pass", ran); err != nil {
		t.Fatalf("RecordVerify: %v", err)
	}
	got, _ := store.Get("rec-0001")
	if AcceptanceCriteria(got.Body) == "" || !strings.Contains(got.Body, "## Test Results\n\nverify now: 1 pass") || strings.Contains(got.Body, "prior record") {
		t.Errorf("record not written in place of the prior one:\n%s", got.Body)
	}

	// The criteria moved after the run: nothing is written, and the error names
	// the contract that ran and the one now on the ticket.
	got.Body = strings.Replace(got.Body, "/bin/echo ok", "/bin/echo changed", 1)
	if err := store.Update(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	now := CriteriaIdentity(ParseCriteria(AcceptanceCriteria(got.Body)))
	err := RecordVerify(store, "rec-0001", "verify later: 1 pass", ran)
	if err == nil {
		t.Fatal("RecordVerify recorded a run under a changed contract")
	}
	for _, want := range []string{"changed during verification", ran, now, "not recorded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	after, _ := store.Get("rec-0001")
	if after.Body != got.Body {
		t.Errorf("a refused record changed the ticket:\n%s", after.Body)
	}
}

func TestNewVerifyReport(t *testing.T) {
	results := []VerifyResult{
		{Criterion: Criterion{Text: "passes", Command: "/bin/echo ok"}, Status: VerifyPass},
		{Criterion: Criterion{Text: "no command"}, Status: VerifyUnverified},
		{Criterion: Criterion{Text: "hand-checked", Unverifiable: true, UnverifiableReason: "needs a human"}, Status: VerifyUnverified},
	}

	report := NewVerifyReport("alpha/tk-1", VerifyProvenance{Dir: "/repo", AcceptanceID: "sha256:abc", Candidate: "c1\x1b[2J"}, results)
	if !report.OK {
		t.Error("report with no failures should be ok")
	}
	if report.Dir != "/repo" || report.AcceptanceID != "sha256:abc" || report.Candidate != "c1\ufffd[2J" {
		t.Errorf("provenance = %q %q %q, want the dir, identity and sanitized candidate", report.Dir, report.AcceptanceID, report.Candidate)
	}
	if report.Summary.Pass != 1 || report.Summary.Unverified != 2 || report.Summary.Fail != 0 {
		t.Errorf("summary = %+v, want 1 pass, 0 fail, 2 unverified", report.Summary)
	}
	if report.Results[0].Command != "/bin/echo ok" || report.Results[1].Command != "" {
		t.Errorf("results = %+v, want the command only on the first criterion", report.Results)
	}
	if report.Results[1].UnverifiableReason != "" || report.Results[2].UnverifiableReason != "needs a human" {
		t.Errorf("results = %+v, want the unverifiable reason only on the criterion declaring one", report.Results)
	}
}

func TestNewVerifyReportRefusalIsNotOK(t *testing.T) {
	results := []VerifyResult{
		{Criterion: Criterion{Text: "passes", Command: "/bin/echo ok"}, Status: VerifyPass},
		{Criterion: Criterion{Text: "not permitted", Command: "rm -rf /"}, Status: VerifyRefused},
	}

	report := NewVerifyReport("alpha/tk-1", VerifyProvenance{Dir: "/repo"}, results)
	if report.OK {
		t.Error("ok = true, want false when a criterion was refused")
	}
	if report.Summary.Refused != 1 || report.Summary.Fail != 0 {
		t.Errorf("summary = %+v, want the refusal counted as refused, not failed", report.Summary)
	}
}

func TestRunVerifyRefusesCommandOutsideAllowList(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}

	criteria := []Criterion{{Text: "hostile", Command: "rm -rf " + sentinel}}
	results, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: []string{"go"}})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}

	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("refused command still deleted the sentinel: %v", err)
	}
	if results[0].Status != VerifyRefused {
		t.Errorf("status = %q, want refused", results[0].Status)
	}
	for _, want := range []string{"rm", "verify_allow", "~/.ticket/config.yaml"} {
		if !strings.Contains(results[0].Output, want) {
			t.Errorf("refusal missing %q, so a user can't act on it:\n%s", want, results[0].Output)
		}
	}
}

// A first token ending in ":" is a label, not a program: adding it to
// verify_allow would have tk try to exec it, so the refusal must not say to,
// and names the unverifiable line as the shape the author meant.
func TestRunVerifyRefusalOfLabelDoesNotAdviseAllowList(t *testing.T) {
	results, err := RunVerify(context.Background(), []Criterion{{Text: "labelled", Command: "manual: read the diff"}}, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	out := results[0].Output
	if results[0].Status != VerifyRefused {
		t.Fatalf("status = %q, want refused", results[0].Status)
	}
	for _, unwanted := range []string{"add \"manual:\"", "To permit it", "verify_allow"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("refusal advises allow-listing a label (%q):\n%s", unwanted, out)
		}
	}
	for _, want := range []string{"\"manual:\" is not a program", "unverifiable: <reason>", "command: manual: read the diff"} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal missing %q:\n%s", want, out)
		}
	}
}

func TestRunVerifyRefusesEverythingWhenAllowListIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}

	// An unreadable local config yields an empty list, not the defaults: `go`
	// must be refused, and the refusal must say why rather than claiming the
	// user left it off a list they never got to write.
	criteria := []Criterion{{Text: "hostile", Command: "go run example.com/x@latest"}}
	results, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: []string{}, AllowErr: errors.New("parsing config.yaml: mapping values are not allowed")})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyRefused {
		t.Fatalf("status = %q, want refused", results[0].Status)
	}
	if !strings.Contains(results[0].Output, "could not be read") || !strings.Contains(results[0].Output, "mapping values") {
		t.Errorf("refusal should name the unreadable config as the cause:\n%s", results[0].Output)
	}
	// Editing verify_allow in a file that does not parse leaves the user stuck.
	if !strings.Contains(results[0].Output, "repair") {
		t.Errorf("refusal should tell the user to repair the config:\n%s", results[0].Output)
	}
	if strings.Contains(results[0].Output, "add \"go\"") {
		t.Errorf("refusal advises adding to a list it just said cannot be read:\n%s", results[0].Output)
	}
}

// TestVerifyCheckSharesRefusalWithRun holds CheckVerify to RunVerify over every
// refusal path: where a run refuses or leaves a criterion unverified, the check
// reports the identical result, and where a run executes the command — pass or
// fail — the check reports runnable.
func TestVerifyCheckSharesRefusalWithRun(t *testing.T) {
	allowed := VerifyPolicy{Allow: testAllow}
	for _, tc := range []struct {
		name     string
		command  string
		policy   VerifyPolicy
		runnable bool
	}{
		{"passes", "/bin/sh -c 'exit 0'", allowed, true},
		{"fails", "/bin/sh -c 'exit 3'", allowed, true},
		{"not allowed", "/usr/bin/true", allowed, false},
		{"label", "manual: read the diff", allowed, false},
		{"untokenizable", "/bin/sh -c 'exit 0", allowed, false},
		{"whitespace", "   ", allowed, false},
		{"no command", "", allowed, false},
		{"bad timeout", "/bin/sh -c 'exit 0'", VerifyPolicy{Allow: testAllow, TimeoutErr: errors.New(`verify_timeout "soon" is not a duration`)}, false},
		{"unreadable allow-list", "/bin/sh -c 'exit 0'", VerifyPolicy{Allow: []string{}, AllowErr: errors.New("parsing config.yaml: mapping values are not allowed")}, false},
		{"control characters", "/usr/bin/true \x1b[2J", allowed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			criteria := []Criterion{{Text: "criterion \x1b[31m" + tc.name, Command: tc.command}}
			run, err := RunVerify(context.Background(), criteria, t.TempDir(), tc.policy)
			if err != nil {
				t.Fatalf("RunVerify: %v", err)
			}
			check := CheckVerify(criteria, tc.policy)
			if len(check) != 1 {
				t.Fatalf("CheckVerify returned %d results, want 1", len(check))
			}
			if tc.runnable {
				if run[0].Status != VerifyPass && run[0].Status != VerifyFail {
					t.Fatalf("run status = %q, want the command to have run", run[0].Status)
				}
				want := VerifyResult{Criterion: run[0].Criterion, Status: VerifyRunnable}
				if check[0] != want {
					t.Errorf("check = %+v, want %+v", check[0], want)
				}
				return
			}
			if run[0].Status != VerifyRefused && run[0].Status != VerifyUnverified {
				t.Fatalf("run status = %q, want refused or unverified", run[0].Status)
			}
			if check[0] != run[0] {
				t.Errorf("check = %+v\nrun   = %+v", check[0], run[0])
			}
		})
	}
}

func TestVerifyStripsControlCharactersFromEchoedCommand(t *testing.T) {
	// ParseCriteria rules out a newline, but an ANSI escape survives it and
	// would let a command repaint the operator's terminal at the moment they
	// are asked to judge the refusal.
	command := "\x1b[2J\x1b[1;32mPASS\x1b[0m"
	results, err := RunVerify(context.Background(), []Criterion{{Text: "spoofs", Command: command}}, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if strings.ContainsRune(results[0].Output, 0x1b) {
		t.Errorf("refusal echoed a raw escape character:\n%q", results[0].Output)
	}
	if strings.ContainsRune(results[0].Criterion.Command, 0x1b) {
		t.Errorf("result carried a raw escape in the command:\n%q", results[0].Criterion.Command)
	}

	record := FormatVerifyRecord(results, VerifyProvenance{}, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	if strings.ContainsRune(record, 0x1b) {
		t.Errorf("recorded results echoed a raw escape character:\n%q", record)
	}
}

func TestVerifyStripsControlCharactersFromCriterionText(t *testing.T) {
	// The bullet text is as attacker-writable as the verify line and prints on
	// the same line, so an escape planted there repaints the very output the
	// operator is judging — and the record replays it on every later read. An
	// unverifiable reason is written the same way and prints on that same line.
	criteria := []Criterion{
		{Text: "\x1b[1A\x1b[2KPASS (exit 0) all good", Command: "/bin/echo ok"},
		{Text: "refused \x1b[2Jspoof", Command: "rm -rf /"},
		{Text: "unverified ‮special"},
		{Text: "hand-checked", Unverifiable: true, UnverifiableReason: "\x1b[2Jspoof"},
	}
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	for i, r := range results {
		if strings.ContainsRune(r.Criterion.Text, 0x1b) || strings.ContainsRune(r.Criterion.Text, 0x202e) {
			t.Errorf("result %d carried a raw escape in the criterion text:\n%q", i, r.Criterion.Text)
		}
		if strings.ContainsRune(r.Criterion.UnverifiableReason, 0x1b) || strings.ContainsRune(r.Criterion.UnverifiableReason, 0x202e) {
			t.Errorf("result %d carried a raw escape in the unverifiable reason:\n%q", i, r.Criterion.UnverifiableReason)
		}
	}

	record := FormatVerifyRecord(results, VerifyProvenance{}, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	if strings.ContainsRune(record, 0x1b) || strings.ContainsRune(record, 0x202e) {
		t.Errorf("recorded results replay a raw escape from the criterion text:\n%q", record)
	}
}

func TestVerifyKeepsTabsButNotLineBreaksInCriterionText(t *testing.T) {
	// A tab is ordinary inside a hand-written bullet and cannot repaint a
	// terminal, so mangling it corrupts the text and the record permanently. A
	// line break is different: the record is one line per criterion, so an
	// embedded one would forge extra record lines.
	criteria := []Criterion{{Text: "column\tone\ttwo"}, {Text: "forged\n- PASS (exit 0): nothing"}}
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Criterion.Text != "column\tone\ttwo" {
		t.Errorf("tabs in criterion text were corrupted: %q", results[0].Criterion.Text)
	}
	if strings.ContainsRune(results[1].Criterion.Text, '\n') {
		t.Errorf("newline survived in criterion text: %q", results[1].Criterion.Text)
	}

	record := FormatVerifyRecord(results, VerifyProvenance{}, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(record, "column\tone\ttwo") {
		t.Errorf("recorded results dropped the criterion's tabs:\n%q", record)
	}
	// Summary and provenance lines plus one line per criterion — the forged
	// bullet did not become a line of its own.
	if got := len(strings.Split(record, "\n")); got != 4 {
		t.Errorf("record has %d lines, want 4:\n%q", got, record)
	}
}

func TestRunVerifyAllowListIsExactNotBasename(t *testing.T) {
	dir := t.TempDir()
	evil := filepath.Join(dir, "go")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	criteria := []Criterion{{Text: "impostor", Command: evil + " test ./..."}}
	results, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: []string{"go"}})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyRefused {
		t.Errorf("status = %q, want refused: an entry of %q must not admit %q", results[0].Status, "go", evil)
	}
}

func TestRunVerifyGivesNoShellSemantics(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel.txt")

	// Every metacharacter here is inert without a shell: the whole tail is
	// argument text for /bin/echo, and the chained touch never runs.
	command := "/bin/echo a ; touch " + sentinel + " && echo $(whoami) `id` ~ *"
	results, err := RunVerify(context.Background(), []Criterion{{Text: "metacharacters", Command: command}}, dir, VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}

	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("the second half of a chained command ran: sentinel stat err = %v", err)
	}
	if results[0].Status != VerifyPass {
		t.Fatalf("result = %+v, want a pass", results[0])
	}
	want := "a ; touch " + sentinel + " && echo $(whoami) `id` ~ *"
	if results[0].Output != want {
		t.Errorf("output = %q, want the metacharacters echoed back literally: %q", results[0].Output, want)
	}
}

func TestRunVerifyQuotingGroupsArguments(t *testing.T) {
	criteria := []Criterion{{Text: "quoted", Command: `/bin/echo -n 'one arg' "two  args"`}}
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || results[0].Output != "one arg two  args" {
		t.Errorf("result = %+v, want the quoted arguments passed through whole", results[0])
	}
}

func TestRunVerifyRefusesUnterminatedQuote(t *testing.T) {
	criteria := []Criterion{{Text: "malformed", Command: `/bin/echo 'unterminated`}}
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyRefused || !strings.Contains(results[0].Output, "unterminated") {
		t.Errorf("result = %+v, want a refusal naming the parse error", results[0])
	}
}

func TestTokenizeCommand(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
		wantErr bool
	}{
		{name: "plain", command: "go test ./...", want: []string{"go", "test", "./..."}},
		{name: "collapses whitespace", command: "  go \t test  ", want: []string{"go", "test"}},
		{name: "single quotes", command: "go test -run 'TestA|TestB'", want: []string{"go", "test", "-run", "TestA|TestB"}},
		{name: "double quotes", command: `go test -run "TestA TestB"`, want: []string{"go", "test", "-run", "TestA TestB"}},
		{name: "quotes inside a word", command: `go 'te'st`, want: []string{"go", "test"}},
		{name: "empty quoted argument", command: `go ''`, want: []string{"go", ""}},
		{name: "no expansion", command: "go $HOME ~/x *.go \\n `id`", want: []string{"go", "$HOME", "~/x", "*.go", "\\n", "`id`"}},
		{name: "unterminated single quote", command: "go 'oops", wantErr: true},
		{name: "unterminated double quote", command: `go "oops`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tokenizeCommand(tc.command)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("tokenizeCommand(%q) = %q, want an error", tc.command, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("tokenizeCommand(%q): %v", tc.command, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("tokenizeCommand(%q) = %q, want %q", tc.command, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("argv[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestBareCriteria(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "no acceptance section",
			body: "\nJust a description.\n",
		},
		{
			name: "every criterion marked",
			body: "\n## Acceptance Criteria\n\n- Checked.\n  verify: go test ./...\n- Cannot be.\n  unverifiable: needs a human to look.\n",
		},
		{
			name: "mix reports only the bare ones",
			body: "\n## Acceptance Criteria\n\n- Checked.\n  verify: go test ./...\n- Bare one.\n- Cannot be.\n  unverifiable: needs a human to look.\n- Bare two.\n",
			want: []string{"Bare one.", "Bare two."},
		},
		{
			name: "unverifiable with no reason still carries the claim",
			body: "\n## Acceptance Criteria\n\n- Cannot be.\n  unverifiable:\n",
		},
		{
			// The other half of the asymmetry: an empty verify line is not a
			// command, so the criterion is still bare and still reported.
			name: "empty verify line leaves the criterion bare",
			body: "\n## Acceptance Criteria\n\n- Half done.\n  verify:\n",
			want: []string{"Half done."},
		},
		{
			name: "text is sanitized",
			body: "\n## Acceptance Criteria\n\n- Bare\x1b[2K one.\n",
			want: []string{"Bare�[2K one."},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BareCriteria(tc.body)
			if len(got) != len(tc.want) {
				t.Fatalf("BareCriteria = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("bare[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestBareAcceptanceWarning(t *testing.T) {
	if got := BareAcceptanceWarning("proj/tk-0001", nil); got != "" {
		t.Errorf("BareAcceptanceWarning with nothing bare = %q, want empty", got)
	}
	got := BareAcceptanceWarning("proj/tk-0001", []string{"Bare one.", "Bare two."})
	for _, want := range []string{"proj/tk-0001", "Bare one.", "Bare two.", "verify:", "unverifiable:"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q does not name %q", got, want)
		}
	}
	// The CLI prints this same sentence, and `tk edit` cannot write the
	// acceptance section — naming it would send an operator to a flag that
	// does not exist.
	if strings.Contains(got, "tk edit") {
		t.Errorf("warning names `tk edit`, which has no acceptance flag: %q", got)
	}
}

func TestUnparsedAcceptance(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			// The shape that went unreported: three criteria, each with a verify
			// line, none of which ParseCriteria takes.
			name: "numbered list with verify lines",
			body: "Why.\n\n## Acceptance Criteria\n\n1. First.\n   verify: go test ./a\n2. Second.\n   verify: go test ./b\n3. Third.\n   verify: go test ./c\n",
			want: true,
		},
		{
			name: "parenthesised numbers",
			body: "Why.\n\n## Acceptance Criteria\n\n1) First.\n  verify: go test ./...\n",
			want: true,
		},
		{
			name: "star bullets",
			body: "Why.\n\n## Acceptance Criteria\n\n* First.\n  verify: go test ./...\n",
			want: true,
		},
		{
			name: "plus bullets",
			body: "Why.\n\n## Acceptance Criteria\n\n+ First.\n",
			want: true,
		},
		{
			name: "prose",
			body: "Why.\n\n## Acceptance Criteria\n\nIt works.\n",
			want: true,
		},
		{
			// Every bullet is indented, the first as much as the rest, so none
			// starts a criterion — the section's first line included.
			name: "indented dash bullets",
			body: "Why.\n\n## Acceptance Criteria\n\n  - First.\n    verify: go test ./a\n  - Second.\n    verify: go test ./b\n",
			want: true,
		},
		{
			// The same list split across two acceptance blocks: the later block's
			// first line is merged in indented, as written.
			name: "indented dash bullets across two acceptance blocks",
			body: "Why.\n\n## Acceptance Criteria\n\n  - First.\n    verify: go test ./a\n\n## Acceptance Notes\n\n  - Second.\n    verify: go test ./b\n",
			want: true,
		},
		{
			name: "dash bullets parse",
			body: "Why.\n\n## Acceptance Criteria\n\n- First.\n  verify: go test ./...\n",
		},
		{
			// Bare criteria parse; BareCriteria reports those, not this.
			name: "bare dash bullet parses",
			body: "Why.\n\n## Acceptance Criteria\n\n- First.\n",
		},
		{
			name: "no acceptance section",
			body: "Why.\n",
		},
		{
			// EmptyAcceptance's case, not this one.
			name: "whitespace-only acceptance section",
			body: "Why.\n\n## Acceptance Criteria\n\n   \n\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnparsedAcceptance(tc.body); got != tc.want {
				t.Errorf("UnparsedAcceptance = %v, want %v", got, tc.want)
			}
			if tc.want && EmptyAcceptance(&Ticket{Type: TypeFeature, Body: tc.body}) {
				t.Errorf("EmptyAcceptance also reports this body; the two must be distinct")
			}
		})
	}
}

func TestUnparsedAcceptanceWarning(t *testing.T) {
	got := UnparsedAcceptanceWarning("proj/tk-0001")
	for _, want := range []string{"proj/tk-0001", "`- `", "verify:", "unverifiable:", "ticket_edit", "## Acceptance Criteria"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q does not name %q", got, want)
		}
	}
	if strings.Contains(got, "tk edit") {
		t.Errorf("warning names `tk edit`, which has no acceptance flag: %q", got)
	}
}
