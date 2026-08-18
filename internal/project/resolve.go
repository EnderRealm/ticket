package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveName resolves project name with precedence:
// 1) explicit override
// 2) config path mapping
// 3) git remote name
// 4) directory name
//
// Names are sanitized to prevent path traversal (no ".." or path separators).
func ResolveName(cfg Config, cwd string, explicit string) (name string, source string) {
	if strings.TrimSpace(explicit) != "" {
		n := sanitizeProjectName(strings.TrimSpace(explicit))
		if n == "" {
			return "", "none"
		}
		return n, "flag"
	}

	if fromPath, ok := matchProjectByPath(cfg, cwd); ok {
		return fromPath, "config"
	}

	if fromRemote, ok := projectFromGitRemote(cwd); ok {
		if n := sanitizeProjectName(fromRemote); n != "" {
			return n, "git_remote"
		}
	}

	if fromDir, ok := projectFromDir(cwd); ok {
		if n := sanitizeProjectName(fromDir); n != "" {
			return n, "dirname"
		}
	}

	return "", "none"
}

// ConfiguredRepoPath resolves an exact project name to the repository path
// registered for it on this machine. Callers keep path handling as their
// fallback, so a name wins when it also names a relative filesystem path.
func ConfiguredRepoPath(cfg Config, name string) (string, bool) {
	p, ok := cfg.Projects[name]
	if !ok || p.Path == "" || !CentralRegistered(cfg, name) {
		return "", false
	}
	return p.Path, true
}

// DetectProjectPath returns git top-level directory if available; otherwise cwd.
func DetectProjectPath(cwd string) string {
	if root, ok := gitRoot(cwd); ok {
		return canonicalPath(root)
	}
	return canonicalPath(cwd)
}

func matchProjectByPath(cfg Config, cwd string) (string, bool) {
	abs := canonicalPath(cwd)
	if abs == "" {
		return "", false
	}

	bestName := ""
	bestLen := -1

	for name, project := range cfg.Projects {
		if project.Path == "" {
			continue
		}
		projectPath := canonicalPath(project.Path)
		if projectPath == "" {
			continue
		}
		if abs == projectPath || strings.HasPrefix(abs, projectPath+string(os.PathSeparator)) {
			if len(projectPath) > bestLen {
				bestLen = len(projectPath)
				bestName = name
			}
		}
	}

	if bestName == "" {
		return "", false
	}
	return bestName, true
}

func projectFromGitRemote(cwd string) (string, bool) {
	cmd := exec.Command("git", "-C", cwd, "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	remote := strings.TrimSpace(string(out))
	if remote == "" {
		return "", false
	}

	remote = strings.TrimSuffix(remote, ".git")
	remote = strings.TrimSuffix(remote, "/")

	var name string
	if idx := strings.LastIndex(remote, "/"); idx >= 0 {
		name = remote[idx+1:]
	} else if idx := strings.LastIndex(remote, ":"); idx >= 0 {
		name = remote[idx+1:]
	} else {
		name = remote
	}
	name = strings.TrimSpace(name)
	return name, name != ""
}

func projectFromDir(cwd string) (string, bool) {
	root := DetectProjectPath(cwd)
	name := strings.TrimSpace(filepath.Base(root))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", false
	}
	return name, true
}

func gitRoot(cwd string) (string, bool) {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root), true
	}
	return filepath.Clean(abs), true
}

// ValidName reports whether a project name is safe to join into a filesystem
// path. Names containing path separators or ".." traverse out of the root they
// are joined into, and filepath.Join cleans those segments instead of failing;
// "" and "." collapse onto the root itself, rooting a project store at a
// directory no project owns. Names reach here from config, git remotes, and
// directory basenames as well as from the project half of a namespaced ticket
// ID.
func ValidName(name string) bool {
	if strings.Contains(name, "/") || strings.Contains(name, string(os.PathSeparator)) {
		return false
	}
	return name != "" && name != "." && name != ".."
}

// sanitizeProjectName returns the name if it is safe to join into a path,
// otherwise the empty string.
func sanitizeProjectName(name string) string {
	if !ValidName(name) {
		return ""
	}
	return name
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(eval)
	}
	return filepath.Clean(abs)
}
