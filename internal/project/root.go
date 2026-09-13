package project

import "errors"

// RootNamespace is the reserved namespace ideas live in before they have a
// repository, displayed as Root. It is a namespace and never a project: no
// repository binds to it, no watcher journals it, and it is catalogued rather
// than registered — the catalog (pkg/ticket) lists it with kind root, and
// CentralRegistered is never true for it. The leading underscore keeps it out
// of the space a git remote or a directory basename can resolve to, so no
// repo detects itself as Root by accident.
const RootNamespace = "_root"

// IsRoot reports whether a namespace name is the reserved Root.
func IsRoot(name string) bool {
	return name == RootNamespace
}

// ErrRootBinding is the refusal every path that would bind Root to a
// repository returns: `tk init` when the resolved name is _root, and the
// inventory and activation preflight when a config entry already carries one.
// Root ideas have no checkout by definition — a repository is what an idea
// gains when it becomes a project, and the tickets move there with it.
var ErrRootBinding = errors.New("cannot register " + RootNamespace + " as a project: Root has no repository")
