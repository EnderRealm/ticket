package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	ticketmcp "github.com/EnderRealm/ticket/v7/internal/mcp"
	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/internal/state"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mutationProject is the project the log is keyed on for these tests. The
// package-wide testServer builds a store with no project, which writes no log
// at all — the shape mutation logging deliberately skips.
const mutationProject = "alpha"

// projectServer is testServer over a project-scoped store, with HOME pointed at
// a temp tree so the mutation log lands there. The client declares the name a
// write with no source argument is attributed to.
func projectServer(t *testing.T) *mcp.ClientSession {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	// Absent rather than empty: an empty TK_STORE_ROOT is a set-but-unusable
	// value, and TK_SOURCE would out-rank everything these tests assert.
	for _, key := range []string{project.StoreRootEnv, ticket.SourceEnv} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	store := ticket.NewProjectFileStore(t.TempDir(), mutationProject)
	server := ticketmcp.NewServer(store, "", "")

	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go server.Run(ctx, st)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func mutationSources(t *testing.T) []string {
	t.Helper()
	path, err := state.MutationLogPath(mutationProject)
	if err != nil {
		t.Fatalf("mutation log path: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open mutation log: %v", err)
	}
	defer f.Close()

	var sources []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e ticket.MutationEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("mutation log line %q: %v", line, err)
		}
		sources = append(sources, e.Source)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read mutation log: %v", err)
	}
	return sources
}

func TestMCPWriteAttributesSource(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		env  string
		want string
	}{
		// The client above declares itself "test", which is what a write with no
		// source argument is attributed to.
		{name: "defaults to the client name", want: "test"},
		{name: "source argument", arg: "codex", want: "codex"},
		{name: "env wins over the argument", arg: "codex", env: "harness", want: "harness"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session := projectServer(t)
			if tc.env != "" {
				t.Setenv(ticket.SourceEnv, tc.env)
			}

			args := map[string]any{"title": "Attributed write", "type": "feature"}
			if tc.arg != "" {
				args["source"] = tc.arg
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "ticket_create",
				Arguments: args,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("tool returned error: %v", result.Content)
			}

			sources := mutationSources(t)
			if len(sources) != 1 {
				t.Fatalf("logged %d entries, want 1: %v", len(sources), sources)
			}
			if sources[0] != tc.want {
				t.Errorf("source = %q, want %q", sources[0], tc.want)
			}
		})
	}
}

func TestMCPAddNoteAttributesSource(t *testing.T) {
	session := projectServer(t)
	ctx := context.Background()

	id := createTicketID(t, session, map[string]any{
		"title":  "Noted ticket",
		"type":   "feature",
		"source": "claude",
	})
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_add_note",
		Arguments: map[string]any{"id": id, "text": "a note", "source": "codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	sources := mutationSources(t)
	want := []string{"claude", "codex"}
	if len(sources) != len(want) {
		t.Fatalf("logged %d entries, want %d: %v", len(sources), len(want), sources)
	}
	for i := range want {
		if sources[i] != want[i] {
			t.Errorf("entry %d source = %q, want %q", i, sources[i], want[i])
		}
	}
}
