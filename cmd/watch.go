package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/journal"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Background git commit watcher",
	Long:  "Watch git history and link commits to tickets via bracket refs [ticket-id]. Use 'tk serve' for the MCP server.",
}

var watchStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start watcher as background process",
	RunE:  runWatchStart,
}

var watchStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background watcher",
	RunE:  runWatchStop,
}

var watchStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show watcher status",
	RunE:  runWatchStatus,
}

var watchLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show watcher log output",
	RunE:  runWatchLogs,
}

var watchRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Run watcher in foreground",
	Hidden: true,
	RunE:   runWatchRun,
}

var (
	watchInterval time.Duration
	watchOnce     bool
	watchLogLines int
)

func init() {
	watchStartCmd.Flags().DurationVar(&watchInterval, "interval", 5*time.Second, "polling interval")
	watchRunCmd.Flags().DurationVar(&watchInterval, "interval", 5*time.Second, "polling interval")
	watchRunCmd.Flags().BoolVar(&watchOnce, "once", false, "run one cycle and exit")
	watchLogsCmd.Flags().IntVarP(&watchLogLines, "n", "n", 50, "number of lines to show")

	watchCmd.AddCommand(watchStartCmd, watchStopCmd, watchStatusCmd, watchLogsCmd, watchRunCmd)
	rootCmd.AddCommand(watchCmd)
}

func watchPIDPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ticket", "state", "watch.pid"), nil
}

func watchLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ticket", "state", "watch.log"), nil
}

func watchRunningPID(pidPath string) (int, bool) {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return 0, false
	}
	return pid, true
}

func runWatchStart(cmd *cobra.Command, args []string) error {
	if err := refuseIsolatedStore("watch"); err != nil {
		return err
	}

	pidPath, err := watchPIDPath()
	if err != nil {
		return err
	}
	logPath, err := watchLogPath()
	if err != nil {
		return err
	}

	if pid, running := watchRunningPID(pidPath); running {
		if jsonOutput {
			return emitWatchJSON(map[string]any{"status": "already_running", "pid": pid})
		}
		fmt.Printf("watch already running (pid %d)\n", pid)
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find executable: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	childArgs := []string{self, "watch", "run", "--interval=" + watchInterval.String()}
	procAttr := &os.ProcAttr{
		Files: []*os.File{nil, logFile, logFile},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}

	proc, err := os.StartProcess(self, childArgs, procAttr)
	logFile.Close()
	if err != nil {
		return fmt.Errorf("start watch process: %w", err)
	}
	_ = proc.Release()

	if jsonOutput {
		return emitWatchJSON(map[string]any{
			"status":   "started",
			"pid_file": pidPath,
			"log_file": logPath,
		})
	}
	fmt.Printf("watch started (log: %s)\n", logPath)
	return nil
}

func runWatchStop(cmd *cobra.Command, args []string) error {
	if err := refuseIsolatedStore("watch"); err != nil {
		return err
	}

	pidPath, err := watchPIDPath()
	if err != nil {
		return err
	}

	pid, running := watchRunningPID(pidPath)
	if !running {
		if jsonOutput {
			return emitWatchJSON(map[string]any{"status": "not_running"})
		}
		fmt.Println("watch is not running")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("signal process %d: %w", pid, err)
	}
	_ = os.Remove(pidPath)

	if jsonOutput {
		return emitWatchJSON(map[string]any{"status": "stopped", "pid": pid})
	}
	fmt.Printf("watch stopped (pid %d)\n", pid)
	return nil
}

func runWatchStatus(cmd *cobra.Command, args []string) error {
	if err := refuseIsolatedStore("watch"); err != nil {
		return err
	}

	pidPath, err := watchPIDPath()
	if err != nil {
		return err
	}
	logPath, err := watchLogPath()
	if err != nil {
		return err
	}

	pid, running := watchRunningPID(pidPath)

	// A running watcher says nothing about whether it journals anything, so the
	// per-project flags are part of its status. A config that will not load
	// costs the flag list, not the status: whether a watcher is running is
	// answered by the pid file alone, and this command is what a user reaches
	// for when the config is broken.
	cfg, cfgErr := project.Load()
	names := make([]string, 0, len(cfg.Projects))
	for name := range cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)

	if jsonOutput {
		out := map[string]any{
			"running":  running,
			"pid":      pid,
			"pid_file": pidPath,
			"log_file": logPath,
		}
		if cfgErr != nil {
			out["config_error"] = cfgErr.Error()
		} else {
			projects := make([]map[string]any, 0, len(names))
			for _, name := range names {
				p := cfg.Projects[name]
				projects = append(projects, map[string]any{
					"project":         name,
					"auto_link":       p.AutoLink,
					"auto_close":      p.AutoClose,
					"auto_retrospect": p.AutoRetrospect,
				})
			}
			out["projects"] = projects
		}
		return emitWatchJSON(out)
	}

	if running {
		fmt.Printf("watch running (pid %d)\n", pid)
	} else {
		fmt.Println("watch is not running")
	}
	fmt.Printf("log: %s\n", logPath)
	if cfgErr != nil {
		fmt.Printf("config error: %v\n", cfgErr)
		return nil
	}
	for _, name := range names {
		p := cfg.Projects[name]
		fmt.Printf("  %-24s auto_link=%t auto_close=%t auto_retrospect=%t\n", name, p.AutoLink, p.AutoClose, p.AutoRetrospect)
	}
	return nil
}

func runWatchLogs(cmd *cobra.Command, args []string) error {
	if err := refuseIsolatedStore("watch"); err != nil {
		return err
	}

	logPath, err := watchLogPath()
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			if jsonOutput {
				return emitWatchJSON(map[string]any{"log_file": logPath, "lines": []string{}})
			}
			fmt.Println("(no log file)")
			return nil
		}
		return err
	}

	allLines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	start := 0
	if len(allLines) > watchLogLines {
		start = len(allLines) - watchLogLines
	}
	tail := allLines[start:]

	if jsonOutput {
		return emitWatchJSON(map[string]any{"log_file": logPath, "lines": tail})
	}

	for _, line := range tail {
		fmt.Println(line)
	}
	return nil
}

func runWatchRun(cmd *cobra.Command, args []string) error {
	if err := refuseIsolatedStore("watch"); err != nil {
		return err
	}

	if watchInterval <= 0 {
		return fmt.Errorf("--interval must be > 0")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Write PID file
	pidPath, err := watchPIDPath()
	if err == nil {
		_ = os.MkdirAll(filepath.Dir(pidPath), 0o755)
		_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
		defer os.Remove(pidPath)
	}

	cfg, err := project.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	migrateJournalDefaults(&cfg)

	summary := journalingSummary(cfg)
	log.Printf("watch starting: %s interval=%s once=%t", summary, watchInterval, watchOnce)
	defer log.Println("watch stopping")

	cycle := func() {
		cfg, err = project.Load()
		if err != nil {
			log.Printf("warning: reload config: %v", err)
			return
		}
		// The config is reloaded every tick, so a project switched on or off
		// mid-run is reported once rather than every cycle or never.
		if reloaded := journalingSummary(cfg); reloaded != summary {
			log.Printf("watch config changed: %s", reloaded)
			summary = reloaded
		}

		for name, entry := range cfg.Projects {
			if !entry.AutoLink && !entry.AutoClose && !entry.AutoRetrospect {
				continue
			}

			store, err := watchStoreFor(cfg, name)
			if err != nil {
				log.Printf("%s: resolve store: %v", name, err)
				continue
			}

			result, err := journal.RunWatchCycle(name, entry, store)
			// Reported before the error is: the retrospect scan runs ahead of the
			// git work, so a project whose repo is missing still fires runs and
			// still has warnings to report on a cycle that ends in an error.
			if result.RetrospectFired > 0 {
				log.Printf("%s: retrospect: fired %d", name, result.RetrospectFired)
			}
			for _, w := range result.Warnings {
				log.Printf("%s: %s", name, w)
			}
			if err != nil {
				log.Printf("%s: %v", name, err)
				continue
			}
			if result.Appended > 0 || result.Closed > 0 {
				log.Printf("%s: appended %d, closed %d", name, result.Appended, result.Closed)
			}
		}
	}

	cycle()
	if watchOnce {
		return nil
	}

	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			cancel()
			return nil
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cycle()
		}
	}
}

// journalingSummary names which projects the cycle journals and which it walks
// past, so a watcher that is running but inert is visible in the log rather
// than silent. The predicate is the one both cycle loops enter on, so skipped is
// exactly the set they walk past — a project with only auto_retrospect on is
// processed every tick and belongs on the journaling side. It is also the
// change-detection key for a config reload, so a flag missing from it makes
// toggling that flag invisible.
func journalingSummary(cfg project.Config) string {
	journaling := 0
	var skipped []string
	for name, entry := range cfg.Projects {
		if entry.AutoLink || entry.AutoClose || entry.AutoRetrospect {
			journaling++
			continue
		}
		skipped = append(skipped, name)
	}
	sort.Strings(skipped)
	return fmt.Sprintf("projects=%d journaling=%d skipped=%v", len(cfg.Projects), journaling, skipped)
}

// migrateJournalDefaults applies the one-time journal-defaults migration to cfg
// and persists it. Both journal entry points run it — `tk watch run` and the
// loop `tk serve` starts — since either can be the first to open a store whose
// registrations predate the flip; the marker in the shared config makes the
// second one a no-op. Both are already past the TK_STORE_ROOT guard, so no
// sandbox config is rewritten.
//
// A failed save is a warning, not a failure: the migration decides whether a
// journal runs, not whether the watcher or the MCP server may.
func migrateJournalDefaults(cfg *project.Config) {
	// The marker is a shared-config field, so with no central root to write it
	// to, Save would persist the flipped flags and drop the marker — the flip
	// would run again on every start and re-enable what a user turned off. A
	// machine without a central root also has no central registration to flip.
	if _, err := project.CentralStoreRoot(); err != nil {
		return
	}

	flipped, changed := project.MigrateJournalDefaults(cfg)
	if !changed {
		return
	}
	if err := project.Save(*cfg); err != nil {
		log.Printf("warning: save journal defaults migration: %v", err)
		return
	}
	if len(flipped) > 0 {
		log.Printf("journal defaults migrated: auto_link and auto_close enabled for %s", strings.Join(flipped, " "))
	}
}

// watchSource attributes the journal loop's auto-close writes in the mutation
// log.
const watchSource = "watch"

// watchStoreFor is the store a journal watch cycle writes into, for `tk watch`
// and for the loop `tk serve` runs: the project's directory in the central
// store, or no store at all for a project that is not registered centrally — its
// commits are still journalled, and RunWatchCycle skips the auto-close that
// needs a store.
//
// "No store" is a nil ticket.Store interface, not a nil *FileStore inside one:
// RunWatchCycle decides on `store != nil`, which a typed nil satisfies, so
// returning one would send every unregistered project's auto-close into a store
// with no directory.
func watchStoreFor(cfg project.Config, name string) (ticket.Store, error) {
	if !project.CentralRegistered(cfg, name) {
		return nil, nil
	}
	dir, err := project.CentralProjectDir(name)
	if err != nil {
		return nil, err
	}
	// Project-scoped: auto-close writes go through the same validation as MCP
	// writes and must resolve the namespaced parent/dep IDs the central store
	// records. Attributed to the watcher in the mutation log: an auto-close is a
	// daemon acting on a commit, and an unattributed write records "human".
	return ticket.WithSource(ticket.NewProjectFileStore(dir, name), watchSource), nil
}

func emitWatchJSON(data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
