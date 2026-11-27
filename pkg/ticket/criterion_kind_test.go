package ticket

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCriteriaParsesTestMarker(t *testing.T) {
	cases := []struct {
		name    string
		section string
		want    Criterion
		kind    CriterionKind
	}{
		{
			name:    "test line marks the criterion and keeps its text",
			section: "- Rows fit.\n  test: run tk ls at 40 columns",
			want:    Criterion{Text: "Rows fit.", Test: true, TestText: "run tk ls at 40 columns"},
			kind:    CriterionTest,
		},
		{
			name:    "no space after the marker",
			section: "- Rows fit.\n  test:look",
			want:    Criterion{Text: "Rows fit.", Test: true, TestText: "look"},
			kind:    CriterionTest,
		},
		{
			name:    "empty test line still marks and consumes the slot",
			section: "- Rows fit.\n  test:\n  test: later text",
			want:    Criterion{Text: "Rows fit.", Test: true},
			kind:    CriterionTest,
		},
		{
			name:    "first test line wins",
			section: "- Rows fit.\n  test: first\n  test: second",
			want:    Criterion{Text: "Rows fit.", Test: true, TestText: "first"},
			kind:    CriterionTest,
		},
		{
			name:    "unindented test line is not a marker",
			section: "- Rows fit.\ntest: look",
			want:    Criterion{Text: "Rows fit."},
			kind:    CriterionJudged,
		},
		{
			name:    "test line without a preceding bullet",
			section: "  test: look\n- Rows fit.",
			want:    Criterion{Text: "Rows fit."},
			kind:    CriterionJudged,
		},
		{
			name:    "a verify command running test is a command",
			section: "- File exists.\n  verify: test -f x",
			want:    Criterion{Text: "File exists.", Command: "test -f x"},
			kind:    CriterionVerify,
		},
		{
			name:    "verify then test is a test criterion keeping its command",
			section: "- Both.\n  verify: go test ./...\n  test: look",
			want:    Criterion{Text: "Both.", Command: "go test ./...", Test: true, TestText: "look"},
			kind:    CriterionTest,
		},
		{
			name:    "test then verify is a test criterion keeping its command",
			section: "- Both.\n  test: look\n  verify: go test ./...",
			want:    Criterion{Text: "Both.", Command: "go test ./...", Test: true, TestText: "look"},
			kind:    CriterionTest,
		},
		{
			name:    "no line is judged by the run",
			section: "- Reads plainly.",
			want:    Criterion{Text: "Reads plainly."},
			kind:    CriterionJudged,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCriteria(tc.section)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("ParseCriteria = %+v, want %+v", got, tc.want)
			}
			if got[0].Kind() != tc.kind {
				t.Errorf("Kind = %q, want %q", got[0].Kind(), tc.kind)
			}
		})
	}

	report := NewCriteriaReport("proj/tk-0001", ParseCriteria("- Rows\x1b[2J fit.\n  test: run\x1b[2J tk ls"))
	got := report.Criteria[0]
	if got.Kind != CriterionTest || got.Test != "run\ufffd[2J tk ls" || got.Text != "Rows\ufffd[2J fit." {
		t.Errorf("report criterion = %+v, want a test criterion with sanitized strings", got)
	}
}

// An identity already recorded on a ticket, or held by a caller, is handed back
// later, so the criteria that produced it must go on producing it.
func TestCriteriaIdentityCoversTestLine(t *testing.T) {
	const unmarked = "- One.\n  verify: /bin/echo one\n- Two.\n  unverifiable: needs a human\n- Three.\n"
	const want = "sha256:8b230460debbd5939a6cc79ee8a0dc3bb28cd52834ec5c8bc8bf6dc83f5983e0"
	if got := CriteriaIdentity(ParseCriteria(unmarked)); got != want {
		t.Errorf("identity of criteria with no test line = %s, want %s", got, want)
	}

	sections := []string{
		unmarked,
		unmarked + "  test:\n",
		unmarked + "  test: look\n",
		unmarked + "  test: look again\n",
		"- One.\n  verify: /bin/echo one\n  test: look\n- Two.\n  unverifiable: needs a human\n- Three.\n",
	}
	seen := map[string]string{}
	for _, section := range sections {
		id := CriteriaIdentity(ParseCriteria(section))
		if prior, ok := seen[id]; ok {
			t.Errorf("two contracts share an identity:\n%s\n%s", prior, section)
		}
		seen[id] = section
	}
}

func TestBareCriterionIsNotFlagged(t *testing.T) {
	tk := mkBody("judged-0001", "\nA description.\n\n## Acceptance Criteria\n\n- Reads plainly.\n- Half done.\n  verify:\n")
	if f := BodyFindings(tk); !f.Empty() {
		t.Errorf("findings = %+v, want none for criteria with no line", f)
	}
}

func TestUnverifiableReadsAsJudgedByRun(t *testing.T) {
	for _, section := range []string{
		"- Recipe recorded.\n  unverifiable: a recorded artifact",
		"- Recipe recorded.\n  verify: unverifiable: a recorded artifact",
	} {
		criteria := ParseCriteria(section)
		if len(criteria) != 1 || criteria[0].Kind() != CriterionJudged || criteria[0].UnverifiableReason != "a recorded artifact" {
			t.Fatalf("ParseCriteria(%q) = %+v, want one judged criterion keeping its reason", section, criteria)
		}
		results, _, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if results[0].Status != VerifyUnverified || results[0].Criterion.UnverifiableReason != "a recorded artifact" {
			t.Errorf("result = %+v, want unverified keeping its reason", results[0])
		}
	}

	// Beside a verify line the retired line decides nothing.
	both := ParseCriteria("- Checked.\n  unverifiable: only in part\n  verify: /bin/echo ok")[0]
	if both.Kind() != CriterionVerify || both.Command != "/bin/echo ok" || both.UnverifiableReason != "only in part" {
		t.Errorf("criterion = %+v, want a verify criterion keeping its reason", both)
	}
}

func TestCriterionWithTwoMarkersIsReported(t *testing.T) {
	body := "\nA description.\n\n## Acceptance Criteria\n\n" +
		"- Both\x1b[2K one.\n  verify: go test ./...\n  test: look\n" +
		"- Both two.\n  test:\n  verify: go test ./...\n" +
		"- Command.\n  verify: go test ./...\n" +
		"- Person.\n  test: look\n" +
		"- Person, and an empty verify line.\n  verify:\n  test: look\n" +
		"- Person, and the retired line.\n  unverifiable: why\n  test: look\n" +
		"- Judged.\n"
	found := TwoMarkerCriteria(body)
	if len(found) != 2 || found[0] != "Both\ufffd[2K one." || found[1] != "Both two." {
		t.Fatalf("TwoMarkerCriteria = %q, want the two criteria carrying both lines, sanitized", found)
	}

	warning := TwoMarkersWarning("proj/tk-0001", found)
	for _, want := range []string{"proj/tk-0001", "Both two.", "`verify:`", "`test:`", "ticket_edit", "## Acceptance Criteria"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning %q does not name %q", warning, want)
		}
	}
	if got := TwoMarkersWarning("proj/tk-0001", nil); got != "" {
		t.Errorf("TwoMarkersWarning with nothing found = %q, want empty", got)
	}

	content := BodyFindings(mkBody("both-0001", body)).Content
	want := ContentIssue{ID: "both-0001", Kind: ContentTwoMarkers, Criteria: 2}
	if len(content) != 1 || content[0] != want {
		t.Errorf("content = %+v, want %+v alone", content, want)
	}
}

func TestVerifyReportsTestCriterionAsWaiting(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	touch := "/bin/sh -c 'touch " + sentinel + "'"
	criteria := ParseCriteria("- Person.\n  test: look\x1b[2J here\n" +
		"- Both.\n  verify: " + touch + "\n  test: look\n" +
		"- Judged.\n" +
		"- Not a command.\n  test: " + touch + "\n")
	policy := VerifyPolicy{Allow: testAllow}

	results, _, err := RunVerify(context.Background(), criteria, t.TempDir(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]VerifyResult{"run": results, "check": CheckVerify(criteria, policy, nil)} {
		for i, want := range []VerifyStatus{VerifyWaiting, VerifyWaiting, VerifyUnverified, VerifyWaiting} {
			if got[i].Status != want || got[i].Output != "" || got[i].Argv != nil {
				t.Errorf("%s result %d = %+v, want %s with no output", name, i, got[i], want)
			}
		}
		if text := got[0].Criterion.TestText; text != "look\ufffd[2J here" {
			t.Errorf("%s test text = %q, want it sanitized", name, text)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("a test criterion ran something: sentinel stat err = %v", err)
	}

	report := NewVerifyReport("proj/tk-0001", VerifyProvenance{Dir: "/repo"}, results)
	if want := (VerifyCounts{Waiting: 3, Unverified: 1}); report.Summary != want || !report.OK {
		t.Errorf("summary = %+v ok = %v, want %+v and ok", report.Summary, report.OK, want)
	}
	if got := report.Results[0]; got.Test != "look\ufffd[2J here" || got.Status != string(VerifyWaiting) {
		t.Errorf("report result = %+v, want waiting with the test text", got)
	}

	record := FormatVerifyRecord(results, VerifyProvenance{Dir: "/repo", AcceptanceID: "sha256:abc"}, time.Date(2026, 7, 31, 22, 10, 0, 0, time.UTC))
	want := "verify 2026-07-31T22:10:00Z: 0 pass, 0 fail, 0 refused, 1 unverified, 3 waiting\n" +
		"checkout /repo; acceptance sha256:abc\n" +
		"- WAITING: Person.\n" +
		"- WAITING: Both.\n" +
		"- UNVERIFIED: Judged.\n" +
		"- WAITING: Not a command."
	if record != want {
		t.Errorf("FormatVerifyRecord =\n%s\nwant\n%s", record, want)
	}
}
