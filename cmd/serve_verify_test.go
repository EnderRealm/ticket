package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Run the actual serve entry point in a process that exits as soon as it
// returns. An in-process server leaves detached goroutines alive and cannot
// prove that verification children are reaped before tk exits.
func TestServeVerifyProcessHelper(t *testing.T) {
	if os.Getenv("TK_VERIFY_PROCESS_TEST") != "1" {
		return
	}
	// TestMain isolates HOME for every test process; restore this fixture only.
	os.Setenv("HOME", os.Getenv("TK_VERIFY_TEST_HOME"))
	switch os.Args[len(os.Args)-1] {
	case "serve":
		if err := serveCmd.RunE(serveCmd, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	case "verify":
		rootCmd.SetArgs([]string{"--project", "alpha", "verify", os.Getenv("TK_VERIFY_TICKET_ID")})
		Execute()
		os.Exit(0)
	case "worker":
		child := exec.Command(os.Args[0], "-test.run=^TestServeVerifyProcessHelper$", "--", "child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Getenv("TK_VERIFY_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	case "child":
		if err := os.WriteFile(os.Getenv("TK_VERIFY_PID_FILE")+".child", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func TestServeStopsVerificationProcessGroup(t *testing.T) {
	for _, mode := range []string{"disconnect", "cancel", "timeout", "cli-interrupt"} {
		t.Run(mode, func(t *testing.T) { serveStopsVerificationProcessGroup(t, mode) })
	}
}

func serveStopsVerificationProcessGroup(t *testing.T, mode string) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(project.StoreRootEnv, "")
	// Save the test host's policy before setting the isolated store root.
	if err := os.Unsetenv(project.StoreRootEnv); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(project.Config{VerifyAllow: []string{executable}}); err != nil {
		t.Fatal(err)
	}
	root, dir := t.TempDir(), t.TempDir()
	t.Setenv(project.StoreRootEnv, root)
	timeout := ""
	if mode == "timeout" {
		timeout = "1s"
	}
	if err := project.Save(project.Config{CentralRoot: root, Projects: map[string]project.ProjectConfig{
		"alpha": {Path: dir, Store: "central", VerifyTimeout: timeout},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tickets", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "pid")
	process := exec.Command(executable, "-test.run=^TestServeVerifyProcessHelper$", "--", "serve")
	process.Env = append(os.Environ(), "TK_VERIFY_PROCESS_TEST=1", "TK_VERIFY_PID_FILE="+pidFile, "TK_VERIFY_TEST_HOME="+os.Getenv("HOME"))
	in, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	process.Stderr = os.Stderr
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Process.Kill() })
	client := gomcp.NewClient(&gomcp.Implementation{Name: "test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &gomcp.IOTransport{Reader: out, Writer: in}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	created, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "ticket_create", Arguments: map[string]any{
		"project": "alpha", "title": "Disconnect cleanup",
		"acceptance": "- Child is supervised.\n  verify: " + strconv.Quote(executable) + " -test.run=^TestServeVerifyProcessHelper$ -- worker\n",
	}})
	if err != nil || created.IsError {
		t.Fatalf("create: %v, %v", created, err)
	}
	var ticket map[string]any
	if err := json.Unmarshal([]byte(created.Content[0].(*gomcp.TextContent).Text), &ticket); err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	var cli *exec.Cmd
	if mode == "cli-interrupt" {
		cli = exec.Command(executable, "-test.run=^TestServeVerifyProcessHelper$", "--", "verify")
		cli.Env = append(process.Env, "TK_VERIFY_TICKET_ID="+ticket["id"].(string))
		cli.Stderr = os.Stderr
		if err := cli.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cli.Process.Kill() })
		go func() { callDone <- cli.Wait() }()
	} else {
		go func() {
			_, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "ticket_verify", Arguments: map[string]any{"id": ticket["id"]}})
			callDone <- err
		}()
	}
	var pids []int
	for _, file := range []string{pidFile, pidFile + ".child"} {
		var pid int
		for pid == 0 {
			if raw, err := os.ReadFile(file); err == nil {
				pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
			}
			select {
			case <-ctx.Done():
				t.Fatal("verification process did not start")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}
		pids = append(pids, pid)
		t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	}
	if mode == "cli-interrupt" {
		if err := cli.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-callDone:
			if err == nil {
				t.Fatal("interrupted verify succeeded")
			}
		case <-ctx.Done():
			t.Fatal("CLI did not stop on interrupt")
		}
		assertVerifyProcessesStopped(t, pids)
	} else if mode != "disconnect" {
		if mode == "cancel" {
			r, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "ticket_verify_cancel", Arguments: map[string]any{"id": ticket["id"]}})
			if err != nil || r.IsError {
				t.Fatalf("cancel: %v, %v", r, err)
			}
		}
		r, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "ticket_verify_status", Arguments: map[string]any{"id": ticket["id"]}})
		if err != nil || r.IsError {
			t.Fatalf("status: %v, %v", r, err)
		}
		var status struct {
			State  string
			Report *struct{ Summary struct{ Fail int } }
		}
		if err := json.Unmarshal([]byte(r.Content[0].(*gomcp.TextContent).Text), &status); err != nil {
			t.Fatal(err)
		}
		if mode == "cancel" && (status.State != "cancelled" || status.Report != nil) {
			t.Fatalf("cancel status: %+v", status)
		}
		if mode == "timeout" && (status.State != "completed" || status.Report == nil || status.Report.Summary.Fail != 1) {
			t.Fatalf("timeout status: %+v", status)
		}
		assertVerifyProcessesStopped(t, pids)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- process.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("serve exit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("serve did not drain and exit")
	}
	if mode != "cli-interrupt" {
		<-callDone
	}
	assertVerifyProcessesStopped(t, pids)
}

func assertVerifyProcessesStopped(t *testing.T, pids []int) {
	t.Helper()
	for _, pid := range pids {
		// The host's reaper can lag delivery of SIGKILL to an orphaned child.
		deadline := time.Now().Add(time.Second)
		for syscall.Kill(pid, 0) == nil {
			if time.Now().After(deadline) {
				t.Fatalf("verification process %d is still alive after completion", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
