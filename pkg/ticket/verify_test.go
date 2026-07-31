package ticket

import (
	"context"
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

func TestRunVerify(t *testing.T) {
	criteria := []Criterion{
		{Text: "passes", Command: "true"},
		{Text: "fails", Command: "false"},
		{Text: "fails loudly", Command: "echo boom; exit 7"},
		{Text: "no command"},
	}

	results, err := RunVerify(context.Background(), criteria, t.TempDir())
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

	results, err := RunVerify(context.Background(), []Criterion{{Text: "in dir", Command: "cat marker.txt"}}, dir)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || results[0].Output != "here" {
		t.Errorf("result = %+v, want a pass reading the marker file in dir", results[0])
	}
}

func TestRunVerifyMissingDir(t *testing.T) {
	criteria := []Criterion{{Text: "passes", Command: "true"}}
	if _, err := RunVerify(context.Background(), criteria, filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Error("RunVerify should error once when the directory doesn't exist")
	}
}

func TestRunVerifyHonorsCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := RunVerify(ctx, []Criterion{{Text: "passes", Command: "true"}}, t.TempDir())
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

	results, err := RunVerify(context.Background(), []Criterion{{Text: "hangs", Command: "sleep 5"}}, t.TempDir())
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
	results, err := RunVerify(context.Background(), []Criterion{{Text: "noisy", Command: "yes x | head -c 20000"}}, t.TempDir())
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
		{Criterion: Criterion{Text: "passes", Command: "true"}, Status: VerifyPass},
		{Criterion: Criterion{Text: "fails", Command: "false"}, Status: VerifyFail, ExitCode: 1},
		{Criterion: Criterion{Text: "no command"}, Status: VerifyUnverified},
	}
	at := time.Date(2026, 7, 31, 22, 10, 0, 0, time.UTC)

	got := FormatVerifyRecord(results, at)
	want := "verify 2026-07-31T22:10:00Z: 1 pass, 1 fail, 1 unverified\n" +
		"- PASS (exit 0): passes\n" +
		"- FAIL (exit 1): fails\n" +
		"- UNVERIFIED: no command"
	if got != want {
		t.Errorf("FormatVerifyRecord =\n%s\nwant\n%s", got, want)
	}
}

func TestNewVerifyReport(t *testing.T) {
	results := []VerifyResult{
		{Criterion: Criterion{Text: "passes", Command: "true"}, Status: VerifyPass},
		{Criterion: Criterion{Text: "no command"}, Status: VerifyUnverified},
	}

	report := NewVerifyReport("alpha/tk-1", "/repo", results)
	if !report.OK {
		t.Error("report with no failures should be ok")
	}
	if report.Summary.Pass != 1 || report.Summary.Unverified != 1 || report.Summary.Fail != 0 {
		t.Errorf("summary = %+v, want 1 pass, 0 fail, 1 unverified", report.Summary)
	}
	if report.Results[0].Command != "true" || report.Results[1].Command != "" {
		t.Errorf("results = %+v, want the command only on the first criterion", report.Results)
	}
}
