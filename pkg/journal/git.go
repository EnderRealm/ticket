package journal

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GitCommit represents a parsed git log entry.
type GitCommit struct {
	SHA    string
	TS     string
	Author string
	Msg    string
}

// CollectCommits runs git log and returns commits in chronological order.
// If lastSHA is provided, returns commits after that SHA. If lastSHA is empty
// or the SHA range fails (history rewrite), falls back to --since=registeredAt.
func CollectCommits(repoPath, lastSHA, registeredAt string) ([]GitCommit, error) {
	args := []string{"-C", repoPath, "log", "--reverse", "--pretty=format:%H%x1f%cI%x1f%an%x1f%B%x1e"}
	if strings.TrimSpace(lastSHA) != "" {
		args = append(args, fmt.Sprintf("%s..HEAD", strings.TrimSpace(lastSHA)))
	} else if strings.TrimSpace(registeredAt) != "" {
		args = append(args, "--since="+strings.TrimSpace(registeredAt))
	}

	out, err := exec.Command("git", args...).Output()
	if err != nil {
		if strings.TrimSpace(lastSHA) != "" {
			return CollectCommits(repoPath, "", registeredAt)
		}
		return nil, fmt.Errorf("git log failed at %s: %v", repoPath, err)
	}

	records := strings.Split(string(out), "\x1e")
	commits := make([]GitCommit, 0, len(records))
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\x1f", 4)
		if len(parts) < 4 {
			continue
		}
		commits = append(commits, GitCommit{
			SHA:    parts[0],
			TS:     parts[1],
			Author: parts[2],
			Msg:    strings.TrimSpace(parts[3]),
		})
	}
	return commits, nil
}

var (
	refPattern         = regexp.MustCompile(`\[([A-Za-z0-9][A-Za-z0-9_-]*)\]`)
	closePrefixPattern = regexp.MustCompile(`(?i)\b(?:closes|fixes)\s*:?\s*`)
	leadingRefPattern  = regexp.MustCompile(`^\[([A-Za-z0-9][A-Za-z0-9_-]*)\]`)
)

// ExtractTicketActions parses a commit message and returns a map of ticket ID
// to action. Bracket refs [ticket-id] produce action "ref". Closes:/Fixes:
// prefixes followed by bracket refs produce action "close".
func ExtractTicketActions(message string) map[string]string {
	actions := map[string]string{}
	for _, loc := range closePrefixPattern.FindAllStringIndex(message, -1) {
		for _, ticketID := range extractBracketList(message[loc[1]:]) {
			actions[ticketID] = "close"
		}
	}
	for _, match := range refPattern.FindAllStringSubmatch(message, -1) {
		if len(match) > 1 {
			if _, ok := actions[match[1]]; !ok {
				actions[match[1]] = "ref"
			}
		}
	}
	return actions
}

// extractBracketList extracts consecutive bracket-enclosed IDs from the start of input.
func extractBracketList(input string) []string {
	var ids []string
	rest := input
	for {
		rest = strings.TrimLeft(rest, " \t,")
		match := leadingRefPattern.FindStringSubmatch(rest)
		if len(match) < 2 {
			break
		}
		ids = append(ids, match[1])
		rest = rest[len(match[0]):]
	}
	return ids
}

// GetDiffStats runs git diff-tree --numstat for a commit and returns changed
// files, total lines added, total lines removed, and the branch name.
func GetDiffStats(repoPath, sha string) (files []string, added, removed int, branch string, err error) {
	out, err := exec.Command("git", "-C", repoPath, "diff-tree", "--numstat", "--root", "-r", sha).Output()
	if err != nil {
		return nil, 0, 0, "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		a, _ := strconv.Atoi(parts[0])
		r, _ := strconv.Atoi(parts[1])
		added += a
		removed += r
		if parts[2] != "" {
			files = append(files, parts[2])
		}
	}
	branch = ResolveStableBranchName(repoPath, sha)
	return files, added, removed, branch, nil
}

// ResolveStableBranchName returns a single stable branch label for a commit.
// Returns empty string if the commit belongs to zero or multiple branches.
func ResolveStableBranchName(repoPath, sha string) string {
	local := listContainingRefs(repoPath, sha, "refs/heads")
	if len(local) == 1 {
		return local[0]
	}
	if len(local) > 1 {
		return ""
	}
	remote := listContainingRefs(repoPath, sha, "refs/remotes")
	var filtered []string
	for _, name := range remote {
		if !strings.HasSuffix(name, "/HEAD") {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return ""
}

func listContainingRefs(repoPath, sha, refPrefix string) []string {
	out, err := exec.Command(
		"git", "-C", repoPath,
		"for-each-ref", "--format=%(refname:short)", "--contains", sha, refPrefix,
	).Output()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		refs = append(refs, name)
	}
	sort.Strings(refs)
	return refs
}

// GetParentCommitTS returns the author date of the parent commit (sha^),
// used as a work_started proxy. Returns empty string on any error.
func GetParentCommitTS(repoPath, sha string) string {
	out, err := exec.Command("git", "-C", repoPath, "log", "-1", "--pretty=format:%aI", sha+"^").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsLiveCommit returns true if the commit timestamp is within the last 60 seconds.
func IsLiveCommit(ts string) bool {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return time.Since(t) < 60*time.Second
}

// WorkDurationSeconds computes the duration in seconds between two RFC3339 timestamps.
// Returns 0 if either is empty or unparseable.
func WorkDurationSeconds(started, ended string) int {
	if started == "" || ended == "" {
		return 0
	}
	s, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return 0
	}
	e, err := time.Parse(time.RFC3339, ended)
	if err != nil {
		return 0
	}
	d := int(e.Sub(s).Seconds())
	if d < 0 {
		return 0
	}
	return d
}
