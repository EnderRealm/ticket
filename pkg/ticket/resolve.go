package ticket

import (
	"fmt"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

// ResolveStoreForRepo opens the ticket Store configured for the given repo
// directory. Reads ~/.ticket/config.yaml plus the shared <central_root>/config.yaml,
// resolves the project name via explicit path mapping, git remote, or directory
// basename, and returns a FileStore rooted at <central_root>/tickets/<name>.
//
// Returns an error if no config is present or no project name resolves. The
// returned FileStore may wrap a directory that does not yet exist; List() on a
// missing directory returns (nil, nil) so callers can treat that as an empty
// project without special-casing.
func ResolveStoreForRepo(repoDir string) (Store, string, error) {
	cfg, err := project.Load()
	if err != nil {
		return nil, "", fmt.Errorf("load ticket config: %w", err)
	}
	name, _ := project.ResolveName(cfg, repoDir, "")
	if name == "" {
		return nil, "", fmt.Errorf("no project resolved for %s", repoDir)
	}
	dir, err := project.CentralProjectDir(name)
	if err != nil {
		return nil, "", err
	}
	return NewFileStore(dir), name, nil
}
