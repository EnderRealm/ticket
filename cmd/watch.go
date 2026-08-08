package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/journal"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
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

	if jsonOutput {
		return emitWatchJSON(map[string]any{
			"running":  running,
			"pid":      pid,
			"pid_file": pidPath,
			"log_file": logPath,
		})
	}

	if running {
		fmt.Printf("watch running (pid %d)\n", pid)
	} else {
		fmt.Println("watch is not running")
	}
	fmt.Printf("log: %s\n", logPath)
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

	log.Printf("watch starting: projects=%d interval=%s once=%t", len(cfg.Projects), watchInterval, watchOnce)
	defer log.Println("watch stopping")

	cycle := func() {
		cfg, err = project.Load()
		if err != nil {
			log.Printf("warning: reload config: %v", err)
			return
		}

		for name, entry := range cfg.Projects {
			if !entry.AutoLink && !entry.AutoClose {
				continue
			}

			var store ticket.Store
			if entry.Store == "central" {
				dir, err := project.CentralProjectDir(name)
				if err != nil {
					log.Printf("%s: resolve store: %v", name, err)
					continue
				}
				// Project-scoped: auto-close writes go through the same
				// validation as MCP writes and must resolve the
				// namespaced parent/dep IDs the central store records.
				store = ticket.NewProjectFileStore(dir, name)
			} else if entry.Path != "" {
				store = ticket.NewFileStore(filepath.Join(entry.Path, ".tickets"))
			}

			result, err := journal.RunWatchCycle(name, entry, store)
			if err != nil {
				log.Printf("%s: %v", name, err)
				continue
			}
			if result.Appended > 0 || result.Closed > 0 {
				log.Printf("%s: appended %d, closed %d", name, result.Appended, result.Closed)
			}
			for _, w := range result.Warnings {
				log.Printf("%s: %s", name, w)
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

func emitWatchJSON(data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
