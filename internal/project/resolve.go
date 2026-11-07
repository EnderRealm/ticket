package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// ExecutionDir is the directory a ticket in namespace may execute in — the
// checkout registered for that project on this machine — or the reason there
// is none. Root has no repository by definition; a namespace with no
// registered checkout here has nowhere to run; a registered path that is not a
// directory is a checkout this machine no longer has. Nothing else stands in:
// not the process working directory, not HOME, not a parent epic's project
// and not the ticket store, because each of those is a directory the ticket
// did not name, and a verify command running in one runs somewhere its
// author never saw.
//
// A project the shared config does register, but whose local config records
// no `path`, is told apart from one registered nowhere: the catalog plainly
// lists it, so "no checkout registered" reads as a contradiction, and the one
// thing missing is the local file's `path`.
func ExecutionDir(cfg Config, namespace string) (string, error) {
	if IsRoot(namespace) {
		return "", fmt.Errorf("Root has no repository: a Root ticket is groomed and completed through edits and notes, never run")
	}
	path, ok := ConfiguredRepoPath(cfg, namespace)
	if !ok {
		if CentralRegistered(cfg, namespace) {
			return "", fmt.Errorf("project %q has no local path in %s — run `tk init` in its checkout", namespace, localConfigName())
		}
		return "", fmt.Errorf("project %q has no checkout registered on this machine — run `tk init` in its repository", namespace)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project %q checkout %s is not a directory on this machine", namespace, path)
	}
	return path, nil
}

// VerifyTarget resolves which project a ticket in namespace verifies against
// and that project's checkout. implementation is the ticket's
// `implementation` field: empty means the ticket's own project, through
// ExecutionDir exactly as before. Set, it names the project whose checkout the
// commands run in instead — resolved through ExecutionDir as well, so only a
// checkout this machine registered with `tk init` qualifies, and every reason
// that refuses one refuses this one, naming the field. It is a project name and
// never a path, and a name that does not resolve is refused rather than falling
// back to the ticket's own checkout: running there would record the ticket's
// project as evidence for code that lives elsewhere. A Root ticket is refused
// either way, since Root tickets are never run.
func VerifyTarget(cfg Config, namespace, implementation string) (name, dir string, err error) {
	// Root goes through ExecutionDir too, which refuses it whatever the field says.
	if implementation == "" || IsRoot(namespace) {
		dir, err := ExecutionDir(cfg, namespace)
		return namespace, dir, err
	}
	if !ValidName(implementation) {
		return "", "", fmt.Errorf("ticket field implementation %q is not a project name", implementation)
	}
	dir, err = ExecutionDir(cfg, implementation)
	if err != nil {
		return "", "", fmt.Errorf("ticket field implementation %q: %w", implementation, err)
	}
	return implementation, dir, nil
}

// ValidateExecutionDir decides whether requested is somewhere a ticket whose
// project is registered at configured may execute, and returns it canonical
// (absolute, symlinks resolved) when it is. The configured checkout itself is
// always accepted. Anything else must be the root of a git worktree that
// shares the configured checkout's git common dir — a linked worktree of that
// repository, which is where a harness verifies a candidate without disturbing
// the main checkout. A subdirectory, a nested or unrelated repository, a
// directory that is not a git worktree, a path that does not exist, and a
// symlink alias resolving to any of those are each refused naming why. The
// check is over the resolved path, so an alias inside the checkout that points
// out of the repository is judged by where it lands.
func ValidateExecutionDir(configured, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", fmt.Errorf("dir is empty")
	}
	dir := canonicalPath(requested)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("dir %s: not an existing directory: %w", requested, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("dir %s: not an existing directory", requested)
	}
	if dir == canonicalPath(configured) {
		return dir, nil
	}
	top, ok := gitRoot(dir)
	if !ok {
		return "", fmt.Errorf("dir %s is not a git worktree of the configured checkout %s", dir, configured)
	}
	if canonicalPath(top) != dir {
		return "", fmt.Errorf("dir %s is not a worktree root: it is inside the worktree at %s", dir, top)
	}
	common, err := gitCommonDir(dir)
	if err != nil {
		return "", fmt.Errorf("dir %s: %w", dir, err)
	}
	configuredCommon, err := gitCommonDir(configured)
	if err != nil {
		return "", fmt.Errorf("configured checkout %s: %w", configured, err)
	}
	if common != configuredCommon {
		return "", fmt.Errorf("dir %s is not a worktree of the configured checkout %s: it belongs to another repository", dir, configured)
	}
	return dir, nil
}

// gitCommonDir is the canonical path of the git directory the worktree at dir
// shares with every other worktree of its repository: the one thing a linked
// worktree and its main checkout have in common on disk. git prints it
// relative to dir for the main worktree and absolute for a linked one, so it
// is resolved against dir before canonicalizing.
func gitCommonDir(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return "", fmt.Errorf("not a git repository")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return canonicalPath(common), nil
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

// ProjectsAtPath returns the config projects whose path is exactly dir, sorted by
// name. It answers who owns a directory, where matchProjectByPath answers which
// project encloses one: a symlink to a checkout, or a spelling of it in another
// case, is that checkout, but nothing matches by prefix, so a subdirectory is not.
// Every claimant comes back rather than one, since config is a map and picking
// would leave the answer to iteration order.
func ProjectsAtPath(cfg Config, dir string) []string {
	var names []string
	for name, project := range cfg.Projects {
		if project.Path != "" && sameDir(project.Path, dir) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// sameDir reports whether two paths name one directory. Identity is the inode
// where both can be stat'd, because canonicalPath does not fold case and APFS is
// case-insensitive by default, so ../proj from a checkout recorded as .../Proj
// compares unequal as a string. The strings are the fallback for a configured
// path this machine does not have. pkg/ticket's sameStoreDir applies the same
// rule to store directories.
func sameDir(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(infoA, infoB)
	}
	return canonicalPath(a) == canonicalPath(b)
}

// EnclosingProject names the registered project whose checkout holds dir: by
// path for a subdirectory, and through the git common dir for a linked worktree,
// which can sit anywhere on disk but shares the main checkout's .git. It is
// advisory, for wording a refusal, and never resolves a destination: the project
// comes back only if it is registered, because a refusal that sends the user to
// a name has to send them to one that works.
func EnclosingProject(cfg Config, dir string) (string, bool) {
	name, ok := projectAtOrAbove(cfg, dir)
	if !ok {
		common, err := gitCommonDir(dir)
		if err != nil {
			return "", false
		}
		name, ok = projectAtOrAbove(cfg, filepath.Dir(common))
	}
	if !ok || !CentralRegistered(cfg, name) {
		return "", false
	}
	return name, true
}

// projectAtOrAbove walks up from dir to the nearest directory a project owns. It
// answers what matchProjectByPath answers, but by ProjectsAtPath's identity, so a
// subdirectory reached through another case spelling of a checkout still counts.
func projectAtOrAbove(cfg Config, dir string) (string, bool) {
	for d := canonicalPath(dir); ; d = filepath.Dir(d) {
		if names := ProjectsAtPath(cfg, d); len(names) > 0 {
			return names[0], true
		}
		if d == filepath.Dir(d) {
			return "", false
		}
	}
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
