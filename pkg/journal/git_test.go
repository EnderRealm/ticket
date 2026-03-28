package journal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractActions(t *testing.T) {
	tests := []struct {
		msg  string
		want map[string]string
	}{
		{
			msg:  "[fix-bug-1234] Fix the thing",
			want: map[string]string{"fix-bug-1234": "ref"},
		},
		{
			msg:  "[a] [b] Multi-ref commit",
			want: map[string]string{"a": "ref", "b": "ref"},
		},
		{
			msg:  "Closes: [fix-bug-1234] done",
			want: map[string]string{"fix-bug-1234": "close"},
		},
		{
			msg:  "Fixes: [a] [b] both fixed",
			want: map[string]string{"a": "close", "b": "close"},
		},
		{
			msg:  "[ref-ticket] Closes: [close-ticket]",
			want: map[string]string{"ref-ticket": "ref", "close-ticket": "close"},
		},
		{
			msg:  "closes [t-1]",
			want: map[string]string{"t-1": "close"},
		},
		{
			msg:  "fixes [t-1]",
			want: map[string]string{"t-1": "close"},
		},
		{
			msg:  "No tickets here",
			want: map[string]string{},
		},
		{
			msg:  "[migrate-tk-storage-eb6c] Port feature",
			want: map[string]string{"migrate-tk-storage-eb6c": "ref"},
		},
	}
	for _, tt := range tests {
		got := ExtractTicketActions(tt.msg)
		if len(got) != len(tt.want) {
			t.Errorf("ExtractTicketActions(%q): got %d actions, want %d: %v", tt.msg, len(got), len(tt.want), got)
			continue
		}
		for k, v := range tt.want {
			if got[k] != v {
				t.Errorf("ExtractTicketActions(%q)[%q] = %q, want %q", tt.msg, k, got[k], v)
			}
		}
	}
}

func TestMultiTicketCommit(t *testing.T) {
	msg := "[ticket-a] [ticket-b] Shared work"
	actions := ExtractTicketActions(msg)
	if len(actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(actions))
	}
	if actions["ticket-a"] != "ref" {
		t.Errorf("ticket-a: %q, want ref", actions["ticket-a"])
	}
	if actions["ticket-b"] != "ref" {
		t.Errorf("ticket-b: %q, want ref", actions["ticket-b"])
	}
}

func TestWorkDurationSeconds(t *testing.T) {
	got := WorkDurationSeconds("2026-03-28T09:55:00Z", "2026-03-28T10:00:00Z")
	if got != 300 {
		t.Errorf("duration = %d, want 300", got)
	}
}

func TestWorkDurationSeconds_Empty(t *testing.T) {
	if d := WorkDurationSeconds("", "2026-03-28T10:00:00Z"); d != 0 {
		t.Errorf("empty start: got %d, want 0", d)
	}
	if d := WorkDurationSeconds("2026-03-28T10:00:00Z", ""); d != 0 {
		t.Errorf("empty end: got %d, want 0", d)
	}
}

func TestIsLiveCommit(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	if !IsLiveCommit(now) {
		t.Error("current time should be live")
	}
	if IsLiveCommit("2020-01-01T00:00:00Z") {
		t.Error("old time should not be live")
	}
	if IsLiveCommit("not a timestamp") {
		t.Error("invalid should not be live")
	}
}

// initTestRepo creates a temp git repo with a commit for testing.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")
	return dir
}

func commitFile(t *testing.T, dir, filename, content, message string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644)
	cmd := exec.Command("git", "-C", dir, "add", filename)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", dir, "commit", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func TestCollectCommits(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "hello", "[t-1] Initial commit")
	commitFile(t, dir, "b.txt", "world", "[t-2] Second commit")

	commits, err := CollectCommits(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	if commits[0].Msg != "[t-1] Initial commit" {
		t.Errorf("commit[0].Msg = %q", commits[0].Msg)
	}
	if commits[1].Msg != "[t-2] Second commit" {
		t.Errorf("commit[1].Msg = %q", commits[1].Msg)
	}
}

func TestCollectCommits_SHARange(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "hello", "[t-1] First")
	// Get SHA of first commit
	out, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	firstSHA := string(out[:len(out)-1])

	commitFile(t, dir, "b.txt", "world", "[t-2] Second")

	commits, err := CollectCommits(dir, firstSHA, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits after SHA range, want 1", len(commits))
	}
	if commits[0].Msg != "[t-2] Second" {
		t.Errorf("commit.Msg = %q, want [t-2] Second", commits[0].Msg)
	}
}

func TestCollectSince(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "hello", "[t-1] Old commit")

	since := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	commits, err := CollectCommits(dir, "", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
}

func TestCollectFallback(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "hello", "[t-1] Commit")

	since := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	commits, err := CollectCommits(dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits after fallback, want 1", len(commits))
	}
}

func TestDiffStats(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "main.go", "package main\n\nfunc main() {}\n", "[t-1] Add main")

	out, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	sha := string(out[:len(out)-1])

	files, added, removed, _, err := GetDiffStats(dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Errorf("files = %v, want [main.go]", files)
	}
	if added != 3 {
		t.Errorf("added = %d, want 3", added)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}
