package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// WatchCycleResult holds the outcome of a single watch cycle for a project.
type WatchCycleResult struct {
	Appended int
	Closed   int
	// RetrospectFired counts the `loom retrospect` runs the cycle started;
	// RetrospectSeeded the closes it recorded without firing, which only the
	// first cycle after the flag goes on can be nonzero.
	RetrospectFired  int
	RetrospectSeeded int
	Warnings         []string
}

// RunWatchCycle processes new git commits for a project, extracts ticket
// references, computes diff stats, appends journal entries, optionally
// auto-closes tickets, and optionally hands each newly closed ticket to
// `loom retrospect`.
func RunWatchCycle(projectName string, cfg project.ProjectConfig, store ticket.Store) (WatchCycleResult, error) {
	var result WatchCycleResult

	// The scan needs the store and the marker file and nothing else, so it runs
	// around the git work rather than behind it: a project whose repo is missing
	// or unreadable fails the cycle on a git error, and its closes would never be
	// mined. This pass catches everything closed before the cycle; the one after
	// the close loop catches what the cycle itself closed. Neither can duplicate
	// the other — the marker gate decides what is pending, and a fire writes its
	// marker first.
	scanRetrospects := func() {
		if !cfg.AutoRetrospect || store == nil {
			return
		}
		// One budget across both passes, so the cap is per cycle rather than per
		// pass.
		budget := maxRetrospectsPerCycle - result.RetrospectFired
		if budget <= 0 {
			return
		}
		fired, seeded, warnings := runRetrospects(projectName, store, budget)
		result.RetrospectFired += fired
		result.RetrospectSeeded += seeded
		result.Warnings = append(result.Warnings, warnings...)
	}
	scanRetrospects()

	jPath, err := JournalPath(projectName)
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(jPath), 0o755); err != nil {
		return result, err
	}

	knownSHAs, lastSHA, err := LoadJournalState(jPath)
	if err != nil {
		return result, err
	}

	if !dirExists(filepath.Join(cfg.Path, ".git")) {
		return result, fmt.Errorf("no git repository at %s", cfg.Path)
	}

	commits, err := CollectCommits(cfg.Path, lastSHA, cfg.RegisteredAt)
	if err != nil {
		return result, err
	}

	var toAppend []Entry

	type diffResult struct {
		files        []string
		added        int
		removed      int
		branch       string
		workStarted  string
		durationSecs int
	}
	diffCache := map[string]diffResult{}

	for _, commit := range commits {
		if _, seen := knownSHAs[commit.SHA]; seen {
			continue
		}

		actions := ExtractTicketActions(commit.Msg)
		if len(actions) == 0 {
			continue
		}

		if _, cached := diffCache[commit.SHA]; !cached {
			files, added, removed, branch, derr := GetDiffStats(cfg.Path, commit.SHA)
			if derr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("diff-tree %s failed: %v", shortSHA(commit.SHA), derr))
			}
			workStarted := ""
			durationSecs := 0
			if IsLiveCommit(commit.TS) {
				workStarted = GetParentCommitTS(cfg.Path, commit.SHA)
				durationSecs = WorkDurationSeconds(workStarted, commit.TS)
			}
			diffCache[commit.SHA] = diffResult{
				files:        files,
				added:        added,
				removed:      removed,
				branch:       branch,
				workStarted:  workStarted,
				durationSecs: durationSecs,
			}
		}
		dr := diffCache[commit.SHA]

		// Refs are resolved to the IDs they are journalled and closed under
		// before the loop: the raw refs can name one ticket twice, and a ref
		// naming another project stays namespaced and is reported by the
		// auto-close warning below.
		resolved := ResolveActions(projectName, actions)

		tickets := make([]string, 0, len(resolved))
		for ticketID := range resolved {
			tickets = append(tickets, ticketID)
		}
		sort.Strings(tickets)

		for _, ticketID := range tickets {
			action := resolved[ticketID]

			if action == "close" && cfg.AutoClose && store != nil {
				t, err := store.Get(ticketID)
				switch {
				case err != nil:
					result.Warnings = append(result.Warnings, fmt.Sprintf("auto-close %s failed: %v", ticketID, err))
				case t.Type == ticket.TypeEpic:
					// An epic's status is derived from its children, so writing
					// one here would change nothing while appending a note and
					// counting a close that did not happen.
					result.Warnings = append(result.Warnings, fmt.Sprintf("auto-close %s skipped: an epic is closed by its children, not by a commit", ticketID))
				default:
					// Through Mutate: the close appends a note, so a plain
					// read-modify-write would drop whatever an agent wrote to the
					// ticket while the watch cycle was walking the commits.
					_, err := ticket.Mutate(store, ticketID, func(t *ticket.Ticket) error {
						t.Status = ticket.StatusDone
						t.Notes = append(t.Notes, ticket.Note{
							Timestamp: time.Now().UTC(),
							Text:      "auto-closed by commit",
						})
						branch := dr.branch
						if branch == "" {
							branch = t.Branch
						}
						ticket.PopulateDoneOutputs(t, commit.SHA, branch)
						return nil
					})
					if err != nil {
						result.Warnings = append(result.Warnings, fmt.Sprintf("auto-close %s failed: %v", ticketID, err))
					} else {
						result.Closed++
					}
				}
			}

			if !cfg.AutoLink {
				continue
			}

			workEnded := ""
			if dr.workStarted != "" {
				workEnded = commit.TS
			}

			toAppend = append(toAppend, Entry{
				SHA:          commit.SHA,
				Ticket:       ticketID,
				Repo:         cfg.Path,
				TS:           commit.TS,
				Msg:          commit.Msg,
				Author:       commit.Author,
				Action:       action,
				FilesChanged: dr.files,
				LinesAdded:   dr.added,
				LinesRemoved: dr.removed,
				Branch:       dr.branch,
				WorkStarted:  dr.workStarted,
				WorkEnded:    workEnded,
				DurationSecs: dr.durationSecs,
			})
		}

		if cfg.AutoLink {
			knownSHAs[commit.SHA] = struct{}{}
		}
	}

	if len(toAppend) > 0 {
		f, err := os.OpenFile(jPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return result, err
		}
		defer f.Close()

		enc := json.NewEncoder(f)
		for _, row := range toAppend {
			if err := enc.Encode(row); err != nil {
				return result, err
			}
		}
	}

	result.Appended = len(toAppend)

	// The second pass is for the tickets this cycle's own close loop wrote, so it
	// runs only when there were any: an unconditional one re-lists the store and
	// repeats whatever the first pass had to warn about, every tick.
	if result.Closed > 0 {
		scanRetrospects()
	}

	return result, nil
}

// LoadJournalState reads existing SHAs and the last SHA from a journal file.
func LoadJournalState(path string) (map[string]struct{}, string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, "", nil
		}
		return nil, "", err
	}
	defer f.Close()

	known := map[string]struct{}{}
	lastSHA := ""

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row Entry
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.SHA == "" {
			continue
		}
		known[row.SHA] = struct{}{}
		lastSHA = row.SHA
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	return known, lastSHA, nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
