package ticket

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

	results, err := RunVerify(context.Background(), criteria, t.TempDir(), testAllow, nil)
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

func TestRunVerifyRunsInDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := RunVerify(context.Background(), []Criterion{{Text: "in dir", Command: "/bin/cat marker.txt"}}, dir, testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || results[0].Output != "here" {
		t.Errorf("result = %+v, want a pass reading the marker file in dir", results[0])
	}
}

func TestRunVerifyMissingDir(t *testing.T) {
	criteria := []Criterion{{Text: "passes", Command: "/bin/echo ok"}}
	if _, err := RunVerify(context.Background(), criteria, filepath.Join(t.TempDir(), "gone"), testAllow, nil); err == nil {
		t.Error("RunVerify should error once when the directory doesn't exist")
	}
}

func TestRunVerifyHonorsCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := RunVerify(ctx, []Criterion{{Text: "passes", Command: "/bin/echo ok"}}, t.TempDir(), testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyFail {
		t.Errorf("status = %q, want fail with a cancelled caller context", results[0].Status)
	}
}

func TestRunVerifyTimeout(t *testing.T) {
	old := verifyTimeout
	verifyTimeout = 50 * time.Millisecond
	defer func() { verifyTimeout = old }()

	results, err := RunVerify(context.Background(), []Criterion{{Text: "hangs", Command: "/bin/sh -c 'sleep 5'"}}, t.TempDir(), testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyFail {
		t.Errorf("status = %q, want fail", results[0].Status)
	}
	if !strings.Contains(results[0].Output, "timed out") {
		t.Errorf("output = %q, want a timeout note", results[0].Output)
	}
}

func TestRunVerifyCapsOutput(t *testing.T) {
	results, err := RunVerify(context.Background(), []Criterion{{Text: "noisy", Command: "/bin/sh -c 'yes x | head -c 20000'"}}, t.TempDir(), testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if len(results[0].Output) > maxVerifyOutput+len("\n... output truncated") {
		t.Errorf("output length = %d, want capped at %d", len(results[0].Output), maxVerifyOutput)
	}
	if !strings.HasSuffix(results[0].Output, "output truncated") {
		t.Errorf("capped output should be marked truncated, got tail %q", results[0].Output[len(results[0].Output)-30:])
	}
}

func TestCapOutputKeepsValidUTF8(t *testing.T) {
	// 3-byte runes: the byte cap lands mid-rune.
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

	got := FormatVerifyRecord(results, at)
	want := "verify 2026-07-31T22:10:00Z: 1 pass, 1 fail, 1 refused, 1 unverified\n" +
		"- PASS (exit 0): passes\n" +
		"- FAIL (exit 1): fails\n" +
		"- REFUSED (rm -rf /): not permitted\n" +
		"- UNVERIFIED: no command"
	if got != want {
		t.Errorf("FormatVerifyRecord =\n%s\nwant\n%s", got, want)
	}
}

func TestNewVerifyReport(t *testing.T) {
	results := []VerifyResult{
		{Criterion: Criterion{Text: "passes", Command: "/bin/echo ok"}, Status: VerifyPass},
		{Criterion: Criterion{Text: "no command"}, Status: VerifyUnverified},
	}

	report := NewVerifyReport("alpha/tk-1", "/repo", results)
	if !report.OK {
		t.Error("report with no failures should be ok")
	}
	if report.Summary.Pass != 1 || report.Summary.Unverified != 1 || report.Summary.Fail != 0 {
		t.Errorf("summary = %+v, want 1 pass, 0 fail, 1 unverified", report.Summary)
	}
	if report.Results[0].Command != "/bin/echo ok" || report.Results[1].Command != "" {
		t.Errorf("results = %+v, want the command only on the first criterion", report.Results)
	}
}

func TestNewVerifyReportRefusalIsNotOK(t *testing.T) {
	results := []VerifyResult{
		{Criterion: Criterion{Text: "passes", Command: "/bin/echo ok"}, Status: VerifyPass},
		{Criterion: Criterion{Text: "not permitted", Command: "rm -rf /"}, Status: VerifyRefused},
	}

	report := NewVerifyReport("alpha/tk-1", "/repo", results)
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
	results, err := RunVerify(context.Background(), criteria, dir, []string{"go"}, nil)
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
	results, err := RunVerify(context.Background(), criteria, dir, []string{}, errors.New("parsing config.yaml: mapping values are not allowed"))
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

func TestVerifyStripsControlCharactersFromEchoedCommand(t *testing.T) {
	// ParseCriteria rules out a newline, but an ANSI escape survives it and
	// would let a command repaint the operator's terminal at the moment they
	// are asked to judge the refusal.
	command := "\x1b[2J\x1b[1;32mPASS\x1b[0m"
	results, err := RunVerify(context.Background(), []Criterion{{Text: "spoofs", Command: command}}, t.TempDir(), testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if strings.ContainsRune(results[0].Output, 0x1b) {
		t.Errorf("refusal echoed a raw escape character:\n%q", results[0].Output)
	}
	if strings.ContainsRune(results[0].Criterion.Command, 0x1b) {
		t.Errorf("result carried a raw escape in the command:\n%q", results[0].Criterion.Command)
	}

	record := FormatVerifyRecord(results, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	if strings.ContainsRune(record, 0x1b) {
		t.Errorf("recorded results echoed a raw escape character:\n%q", record)
	}
}

func TestVerifyStripsControlCharactersFromCriterionText(t *testing.T) {
	// The bullet text is as attacker-writable as the verify line and prints on
	// the same line, so an escape planted there repaints the very output the
	// operator is judging — and the record replays it on every later read.
	criteria := []Criterion{
		{Text: "\x1b[1A\x1b[2KPASS (exit 0) all good", Command: "/bin/echo ok"},
		{Text: "refused \x1b[2Jspoof", Command: "rm -rf /"},
		{Text: "unverified ‮special"},
	}
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	for i, r := range results {
		if strings.ContainsRune(r.Criterion.Text, 0x1b) || strings.ContainsRune(r.Criterion.Text, 0x202e) {
			t.Errorf("result %d carried a raw escape in the criterion text:\n%q", i, r.Criterion.Text)
		}
	}

	record := FormatVerifyRecord(results, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
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
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Criterion.Text != "column\tone\ttwo" {
		t.Errorf("tabs in criterion text were corrupted: %q", results[0].Criterion.Text)
	}
	if strings.ContainsRune(results[1].Criterion.Text, '\n') {
		t.Errorf("newline survived in criterion text: %q", results[1].Criterion.Text)
	}

	record := FormatVerifyRecord(results, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(record, "column\tone\ttwo") {
		t.Errorf("recorded results dropped the criterion's tabs:\n%q", record)
	}
	// Summary line plus one line per criterion — the forged bullet did not
	// become a line of its own.
	if got := len(strings.Split(record, "\n")); got != 3 {
		t.Errorf("record has %d lines, want 3:\n%q", got, record)
	}
}

func TestRunVerifyAllowListIsExactNotBasename(t *testing.T) {
	dir := t.TempDir()
	evil := filepath.Join(dir, "go")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	criteria := []Criterion{{Text: "impostor", Command: evil + " test ./..."}}
	results, err := RunVerify(context.Background(), criteria, dir, []string{"go"}, nil)
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
	results, err := RunVerify(context.Background(), []Criterion{{Text: "metacharacters", Command: command}}, dir, testAllow, nil)
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
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), testAllow, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || results[0].Output != "one arg two  args" {
		t.Errorf("result = %+v, want the quoted arguments passed through whole", results[0])
	}
}

func TestRunVerifyRefusesUnterminatedQuote(t *testing.T) {
	criteria := []Criterion{{Text: "malformed", Command: `/bin/echo 'unterminated`}}
	results, err := RunVerify(context.Background(), criteria, t.TempDir(), testAllow, nil)
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
