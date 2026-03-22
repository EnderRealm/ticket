package project

import (
	"os"
	"path/filepath"
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
