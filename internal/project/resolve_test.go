package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveName(t *testing.T) {
	t.Run("explicit flag wins", func(t *testing.T) {
		cfg := Config{Projects: map[string]ProjectConfig{
			"config-match": {Path: "/some/path", Store: "central"},
		}}
		name, source := ResolveName(cfg, "/some/path", "explicit-name")
		if name != "explicit-name" {
			t.Errorf("name = %q, want explicit-name", name)
		}
		if source != "flag" {
			t.Errorf("source = %q, want flag", source)
		}
	})

	t.Run("config path match", func(t *testing.T) {
		dir := t.TempDir()
		cfg := Config{Projects: map[string]ProjectConfig{
			"matched": {Path: dir, Store: "central"},
		}}
		name, source := ResolveName(cfg, dir, "")
		if name != "matched" {
			t.Errorf("name = %q, want matched", name)
		}
		if source != "config" {
			t.Errorf("source = %q, want config", source)
		}
	})

	t.Run("dirname fallback", func(t *testing.T) {
		dir := t.TempDir()
		cfg := Config{Projects: map[string]ProjectConfig{}}
		name, source := ResolveName(cfg, dir, "")
		expected := filepath.Base(dir)
		if name != expected {
			t.Errorf("name = %q, want %q", name, expected)
		}
		// Source is either git_remote or dirname depending on whether git is available
		if source != "dirname" && source != "git_remote" {
			t.Errorf("source = %q, want dirname or git_remote", source)
		}
	})

	t.Run("empty explicit is ignored", func(t *testing.T) {
		dir := t.TempDir()
		cfg := Config{Projects: map[string]ProjectConfig{}}
		name, _ := ResolveName(cfg, dir, "   ")
		if name == "" {
			t.Error("should resolve via fallback, not return empty")
		}
	})
}

func TestDetectProjectPath(t *testing.T) {
	dir := t.TempDir()
	result := DetectProjectPath(dir)
	if result == "" {
		t.Error("DetectProjectPath returned empty")
	}
}

func TestMatchProjectByPath(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		dir := t.TempDir()
		cfg := Config{Projects: map[string]ProjectConfig{
			"proj": {Path: dir},
		}}
		name, ok := matchProjectByPath(cfg, dir)
		if !ok || name != "proj" {
			t.Errorf("matchProjectByPath = (%q, %v), want (proj, true)", name, ok)
		}
	})

	t.Run("subdirectory match", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "subdir")
		os.MkdirAll(sub, 0o755)
		cfg := Config{Projects: map[string]ProjectConfig{
			"proj": {Path: dir},
		}}
		name, ok := matchProjectByPath(cfg, sub)
		if !ok || name != "proj" {
			t.Errorf("subdirectory match = (%q, %v), want (proj, true)", name, ok)
		}
	})

	t.Run("no match", func(t *testing.T) {
		cfg := Config{Projects: map[string]ProjectConfig{
			"proj": {Path: "/nonexistent/path"},
		}}
		_, ok := matchProjectByPath(cfg, "/completely/different")
		if ok {
			t.Error("should not match")
		}
	})

	t.Run("longest path wins", func(t *testing.T) {
		parent := t.TempDir()
		child := filepath.Join(parent, "child")
		os.MkdirAll(child, 0o755)
		cfg := Config{Projects: map[string]ProjectConfig{
			"parent": {Path: parent},
			"child":  {Path: child},
		}}
		name, ok := matchProjectByPath(cfg, child)
		if !ok || name != "child" {
			t.Errorf("longest path match = (%q, %v), want (child, true)", name, ok)
		}
	})
}

func TestResolveNameRejectsPathTraversal(t *testing.T) {
	cfg := Config{Projects: map[string]ProjectConfig{}}
	dir := t.TempDir()

	name, source := ResolveName(cfg, dir, "../../etc")
	if name != "" {
		t.Errorf("path traversal name should be rejected, got %q", name)
	}
	if source != "none" {
		t.Errorf("source = %q, want none", source)
	}

	name, _ = ResolveName(cfg, dir, "foo/bar")
	if name != "" {
		t.Errorf("name with slash should be rejected, got %q", name)
	}
}

func TestValidName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"ticket", true},
		{".", false},
		{"..", false},
		{"", false},
		{"foo/bar", false},
	}
	for _, tc := range cases {
		if got := ValidName(tc.name); got != tc.want {
			t.Errorf("ValidName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestProjectFromDir(t *testing.T) {
	dir := t.TempDir()
	name, ok := projectFromDir(dir)
	if !ok {
		t.Fatal("projectFromDir should succeed for temp dir")
	}
	if name == "" || name == "." || name == "/" {
		t.Errorf("unexpected dirname: %q", name)
	}
}

func TestExecutionDir(t *testing.T) {
	checkout := t.TempDir()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Projects: map[string]ProjectConfig{
		"registered": {Path: checkout, Store: "central"},
		"pathless":   {Store: "central"},
		"unmarked":   {Path: checkout},
		"gone":       {Path: filepath.Join(checkout, "missing"), Store: "central"},
		"notadir":    {Path: file, Store: "central"},
	}}
	cases := []struct {
		namespace, want, wantErr string
	}{
		{"registered", checkout, ""},
		{RootNamespace, "", "Root has no repository"},
		{"pathless", "", `project "pathless" has no checkout registered on this machine`},
		{"unmarked", "", `project "unmarked" has no checkout registered on this machine`},
		{"unknown", "", `project "unknown" has no checkout registered on this machine`},
		{"gone", "", `project "gone" checkout ` + filepath.Join(checkout, "missing") + " is not a directory on this machine"},
		{"notadir", "", `project "notadir" checkout ` + file + " is not a directory on this machine"},
	}
	for _, tc := range cases {
		t.Run(tc.namespace, func(t *testing.T) {
			got, err := ExecutionDir(cfg, tc.namespace)
			if tc.wantErr == "" {
				if err != nil || got != tc.want {
					t.Fatalf("ExecutionDir = %q, %v; want %q", got, err, tc.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ExecutionDir = %q, %v; want an error containing %q", got, err, tc.wantErr)
			}
			if got != "" {
				t.Errorf("a refusal returned a directory %q", got)
			}
		})
	}
}
