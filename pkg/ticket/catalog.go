package ticket

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"gopkg.in/yaml.v3"
)

const (
	// CatalogFile is the shared namespace catalog, <central_root>/catalog.yaml.
	// It sits beside config.yaml and outside every namespace directory, so it
	// syncs with the store and no namespace's own contents decide whether the
	// namespace exists. Absent, the store is in its preactivation state: every
	// namespace is inferred from config and directories exactly as before the
	// catalog existed, and nothing here refuses a write to an ordinary project.
	CatalogFile = "catalog.yaml"

	// NamespaceMarker is the tracked file inside a namespace directory that
	// keeps the directory in git while it holds no tickets. Git tracks no empty
	// directories, so without it a namespace that has never held a ticket, or
	// whose last ticket moved out, arrives from a clone indistinguishable from
	// one that was never catalogued.
	NamespaceMarker = ".namespace"

	// ticketsDirName is the directory under the central root that holds one
	// directory per namespace — the layout project.CentralProjectDir fixes.
	ticketsDirName = "tickets"

	// FeatureRootNamespace is the required-feature name whose presence in the
	// catalog activates Root: until it is required, a write to _root is refused
	// as not activated, and a binary that does not list it in
	// SupportedFeatures refuses every write once it is.
	FeatureRootNamespace = "root-namespace"

	// FeatureCrossProjectParents is the required-feature name whose presence
	// in the catalog lets a leaf name an epic in another namespace as its
	// parent. Until it is required, the write boundary refuses a foreign
	// parent exactly as it did before the central graph existed, and a legacy
	// file already holding one is a relationship issue rather than a child.
	FeatureCrossProjectParents = "cross-project-parents"
)

// SupportedFeatures is the set of catalog required_features this binary
// implements. A catalog naming one outside it was written for a newer tk, and
// the store may hold data this binary would misread — a Root ticket, a parent
// in another project — so the guard refuses every write rather than letting
// this binary write what it does not understand. A feature joins the list when
// the code that honours it lands: the central snapshot (snapshot.go) resolves
// a foreign parent and derives its epic across namespaces, so cross-project
// parents are claimed, and stay gated on the catalog requiring them.
var SupportedFeatures = []string{FeatureRootNamespace, FeatureCrossProjectParents}

// NamespaceKind says what a catalogued namespace is.
type NamespaceKind string

const (
	// KindRoot is the built-in Root namespace: catalogued, never registered,
	// and bound to no repository.
	KindRoot NamespaceKind = "root"
	// KindProject is a namespace a repository registered through `tk init`.
	KindProject NamespaceKind = "project"
	// KindLegacy is a namespace admitted from a directory that predates the
	// catalog and has no repository registration behind it. Admitting it
	// invents none: the tickets stay where they are, readable under their
	// existing IDs, and nothing registers a repository for them.
	KindLegacy NamespaceKind = "legacy"
)

// CatalogEntry is one catalogued namespace.
type CatalogEntry struct {
	Kind NamespaceKind `yaml:"kind" json:"kind"`
}

// Catalog is the shared namespace catalog. RequiredFeatures doubles as the
// activation marker: a feature listed there is one every binary sharing the
// store must implement, and listing it is what turns the feature on.
type Catalog struct {
	RequiredFeatures []string                `yaml:"required_features,omitempty" json:"required_features,omitempty"`
	Namespaces       map[string]CatalogEntry `yaml:"namespaces" json:"namespaces"`
}

// CatalogPath returns <centralRoot>/catalog.yaml.
func CatalogPath(centralRoot string) string {
	return filepath.Join(centralRoot, CatalogFile)
}

// LoadCatalog reads the central root's catalog. An absent file is the
// preactivation state and returns a nil catalog with no error; every method
// on a nil *Catalog answers for that state. A file that exists but cannot be
// read or parsed, or that catalogues something the rules forbid, is an error:
// it is the source of truth for which namespaces exist, and a listing that
// silently treated a corrupt catalog as none would report a store with no
// namespaces as healthy.
func LoadCatalog(centralRoot string) (*Catalog, error) {
	path := CatalogPath(centralRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading catalog %s: %w", path, err)
	}
	var cat Catalog
	if err := yaml.Unmarshal(raw, &cat); err != nil {
		return nil, fmt.Errorf("parsing catalog %s: %w", path, err)
	}
	for name, entry := range cat.Namespaces {
		if !project.ValidName(name) {
			return nil, fmt.Errorf("catalog %s: invalid namespace name %q", path, name)
		}
		switch entry.Kind {
		case KindRoot, KindProject, KindLegacy:
		default:
			return nil, fmt.Errorf("catalog %s: namespace %q has unknown kind %q (root, project or legacy)", path, name, entry.Kind)
		}
		// The reserved name and the kind go together: a _root catalogued as a
		// project would be a repository binding by another route, and a second
		// namespace of kind root would be a second Root.
		if project.IsRoot(name) != (entry.Kind == KindRoot) {
			return nil, fmt.Errorf("catalog %s: namespace %q has kind %q: kind %s belongs to %s alone, and %s is kind %s alone", path, name, entry.Kind, KindRoot, project.RootNamespace, project.RootNamespace, KindRoot)
		}
	}
	return &cat, nil
}

// Requires reports whether the catalog lists a feature as required. A nil
// catalog requires nothing.
func (c *Catalog) Requires(feature string) bool {
	return c != nil && slices.Contains(c.RequiredFeatures, feature)
}

// RootActivated reports whether Root writes are allowed: the catalog requires
// root-namespace. Before activation nothing writes to _root, so a store that
// predates the catalog can never have a Root directory to collide with.
func (c *Catalog) RootActivated() bool {
	return c.Requires(FeatureRootNamespace)
}

// CrossProjectParentsActivated reports whether a leaf may be parented to an
// epic in another namespace: the catalog requires cross-project-parents.
func (c *Catalog) CrossProjectParentsActivated() bool {
	return c.Requires(FeatureCrossProjectParents)
}

// UnsupportedFeatures returns the required features this binary does not
// implement, in catalog order.
func (c *Catalog) UnsupportedFeatures() []string {
	if c == nil {
		return nil
	}
	var unsupported []string
	for _, f := range c.RequiredFeatures {
		if !slices.Contains(SupportedFeatures, f) {
			unsupported = append(unsupported, f)
		}
	}
	return unsupported
}

// UnsupportedFeatureError is a catalog requiring a feature this binary does
// not implement. Checked with errors.As, since it travels wrapped through the
// store's write path.
type UnsupportedFeatureError struct {
	Path     string   // the catalog that requires them
	Features []string // the required features outside SupportedFeatures
}

// Error names the features and the remedy: the binary is what has to change.
// A marker in the store cannot make a binary released before the guard refuse,
// so the refusal here is the whole of what the marker buys, and it is only as
// good as the deployment that replaced every binary sharing the store.
func (e *UnsupportedFeatureError) Error() string {
	return fmt.Sprintf("catalog %s requires feature %s, which this tk does not implement (supports: %s) — replace this binary, and every tk binary, embedded library and long-lived process sharing the store, before writing to it",
		e.Path, strings.Join(e.Features, ", "), strings.Join(SupportedFeatures, ", "))
}

// CheckFeatures returns an UnsupportedFeatureError when the catalog requires a
// feature outside SupportedFeatures, and nil otherwise — for a nil catalog
// included, which requires nothing.
func (c *Catalog) CheckFeatures(centralRoot string) error {
	unsupported := c.UnsupportedFeatures()
	if len(unsupported) == 0 {
		return nil
	}
	return &UnsupportedFeatureError{Path: CatalogPath(centralRoot), Features: unsupported}
}

// ErrRootNotActivated is a write to _root before the catalog requires
// root-namespace. Checked with errors.Is; the wrapped message names the
// catalog it consulted.
var ErrRootNotActivated = errors.New("Root (" + project.RootNamespace + ") is not activated")

// checkWrite is the guard every central write passes before it changes
// anything: the catalog's required features against what this binary
// implements, and Root's activation for a write to _root. Defined once and
// called from FileStore's write entry points, which every surface's write
// reaches through (MultiStore delegates); no call site carries a copy.
//
// An absent catalog is the preactivation store, where every write to an
// ordinary project behaves exactly as it did before the catalog existed. Root
// is the one exception in that state: it is built in rather than catalogued,
// so its absence from the catalog is not what refuses it — the missing
// activation marker is.
func checkWrite(centralRoot, namespace string) error {
	cat, err := LoadCatalog(centralRoot)
	if err != nil {
		return err
	}
	if err := cat.CheckFeatures(centralRoot); err != nil {
		return err
	}
	if project.IsRoot(namespace) && !cat.RootActivated() {
		if cat == nil {
			return fmt.Errorf("%w: %s has no catalog, so nothing has activated it (a catalog requiring %s does)", ErrRootNotActivated, centralRoot, FeatureRootNamespace)
		}
		return fmt.Errorf("%w: catalog %s does not require %s, which activation writes", ErrRootNotActivated, CatalogPath(centralRoot), FeatureRootNamespace)
	}
	return nil
}

// NamespaceState says what a namespace's directory holds.
type NamespaceState string

const (
	// StatePresent is a directory holding at least one ticket file.
	StatePresent NamespaceState = "present"
	// StateEmpty is a directory holding no ticket file — with its marker, the
	// healthy shape of a namespace that has never held one.
	StateEmpty NamespaceState = "empty"
	// StateMissing is no directory at all.
	StateMissing NamespaceState = "missing"
)

// Namespace is one entry of an Inventory. Kind comes from the catalog when
// the namespace is catalogued and is inferred otherwise; Registered and
// RepoPath come from the config and say, independently of the kind, whether a
// repository stands behind the name on this machine. RepoPath is always empty
// for Root.
type Namespace struct {
	Name       string         `json:"name"`
	Kind       NamespaceKind  `json:"kind"`
	Registered bool           `json:"registered"`
	RepoPath   string         `json:"repo_path,omitempty"`
	Directory  bool           `json:"directory"`
	Marker     bool           `json:"marker"`
	State      NamespaceState `json:"state"`
}

// Inventory is what a central root holds, read without changing it. Complete
// is false whenever the namespaces listed may not be the namespaces there are
// — no catalog to say, a catalogued namespace with no directory, a required
// feature this binary lacks — and Diagnostics names each cause, so a consumer
// that falls back to what it can see says why the view may be partial rather
// than reporting an empty namespace as a healthy one.
type Inventory struct {
	// Catalog is the loaded catalog, nil before activation.
	Catalog     *Catalog    `json:"catalog,omitempty"`
	Namespaces  []Namespace `json:"namespaces"`
	Complete    bool        `json:"complete"`
	Diagnostics []string    `json:"diagnostics,omitempty"`
}

// ReadInventory lists the namespaces under a central root: Root, which is
// built in and listed even when nothing on disk or in the catalog names it;
// every catalogued namespace; every project the config registers with the
// central store; and every directory under tickets/, so a namespace that
// predates the catalog is discovered rather than dropped. It creates, renames
// and rewrites nothing.
//
// cfg is the merged config the caller loaded; a config that could not be read
// fails at project.Load, not here, so an unreadable one never reaches this
// as an empty one. A catalog that cannot be read, or a tickets directory that
// cannot be listed, is an error for the same reason.
func ReadInventory(centralRoot string, cfg project.Config) (Inventory, error) {
	cat, err := LoadCatalog(centralRoot)
	if err != nil {
		return Inventory{}, err
	}
	ticketsDir := filepath.Join(centralRoot, ticketsDirName)
	dirs := map[string]bool{}
	entries, err := os.ReadDir(ticketsDir)
	if err != nil && !os.IsNotExist(err) {
		return Inventory{}, fmt.Errorf("listing namespaces in %s: %w", ticketsDir, err)
	}
	for _, e := range entries {
		// IsDir is false for a symlink, matching MultiStore.projects and
		// lstatProjectDir: a linked directory is not a namespace of this store.
		if e.IsDir() {
			dirs[e.Name()] = true
		}
	}

	names := map[string]bool{project.RootNamespace: true}
	if cat != nil {
		for name := range cat.Namespaces {
			names[name] = true
		}
	}
	for name := range cfg.Projects {
		if project.CentralRegistered(cfg, name) {
			names[name] = true
		}
	}
	for name := range dirs {
		names[name] = true
	}

	inv := Inventory{Catalog: cat, Complete: true}
	if cat == nil {
		inv.Complete = false
		inv.Diagnostics = append(inv.Diagnostics, fmt.Sprintf("no catalog at %s: the store predates activation, so its namespaces are inferred from config and directories", CatalogPath(centralRoot)))
	}
	if err := cat.CheckFeatures(centralRoot); err != nil {
		inv.Complete = false
		inv.Diagnostics = append(inv.Diagnostics, err.Error())
	}

	for _, name := range sortedKeys(names) {
		ns := Namespace{Name: name, Registered: project.CentralRegistered(cfg, name)}
		entry, catalogued := cat.entry(name)
		switch {
		case catalogued:
			ns.Kind = entry.Kind
		case project.IsRoot(name):
			ns.Kind = KindRoot
		case ns.Registered:
			ns.Kind = KindProject
		default:
			ns.Kind = KindLegacy
		}
		if !project.IsRoot(name) {
			ns.RepoPath = cfg.Projects[name].Path
		}
		ns.State = StateMissing
		if dirs[name] {
			ns.Directory = true
			dir := filepath.Join(ticketsDir, name)
			ns.Marker = markerExists(dir)
			ns.State, err = directoryState(dir)
			if err != nil {
				return Inventory{}, err
			}
		}

		if project.IsRoot(name) {
			for _, c := range rootCollisions(cfg, cat, ns.Directory) {
				inv.Complete = false
				inv.Diagnostics = append(inv.Diagnostics, c)
			}
		}
		// Root is expected on disk once activated: activation writes its marker
		// alongside the feature. Before that its absence is the normal state
		// and nothing about it is missing.
		expected := catalogued || (project.IsRoot(name) && cat.RootActivated())
		switch {
		case expected && !ns.Directory:
			inv.Complete = false
			inv.Diagnostics = append(inv.Diagnostics, fmt.Sprintf("namespace %q is expected (catalogued, or Root once activated) but has no directory under %s: not cloned, or its %s marker was never written", name, ticketsDir, NamespaceMarker))
		case expected && !ns.Marker:
			inv.Complete = false
			inv.Diagnostics = append(inv.Diagnostics, fmt.Sprintf("namespace %q has no %s marker in %s: a clone that holds none of its tickets would drop it", name, NamespaceMarker, filepath.Join(ticketsDir, name)))
		case cat != nil && !catalogued && !project.IsRoot(name):
			// Not incomplete: the tickets are there and readable. Named so
			// activation can admit it, and so a consumer can tell a catalogued
			// namespace from one the catalog does not yet know.
			inv.Diagnostics = append(inv.Diagnostics, fmt.Sprintf("namespace %q (%s) is not catalogued in %s", name, ns.Kind, CatalogPath(centralRoot)))
		}
		inv.Namespaces = append(inv.Namespaces, ns)
	}
	return inv, nil
}

// entry looks a namespace up in the catalog; a nil catalog holds none.
func (c *Catalog) entry(name string) (CatalogEntry, bool) {
	if c == nil {
		return CatalogEntry{}, false
	}
	e, ok := c.Namespaces[name]
	return e, ok
}

// rootCollisions is what stands in the way of reserving _root: a config
// entry that binds it to a repository or registers it as a project, and a
// directory that exists without being catalogued as Root. The inventory
// reports them and PreflightActivation refuses on them, off one definition.
// Neither adopts, moves or renames anything: a colliding directory is
// somebody's data, and the decision about it is theirs.
func rootCollisions(cfg project.Config, cat *Catalog, directory bool) []string {
	var collisions []string
	if p, ok := cfg.Projects[project.RootNamespace]; ok && (p.Path != "" || project.CentralRegistered(cfg, project.RootNamespace)) {
		collisions = append(collisions, fmt.Sprintf("%v: config carries a project entry for it (path %q, store %q) — remove the entry", project.ErrRootBinding, p.Path, p.Store))
	}
	if entry, ok := cat.entry(project.RootNamespace); directory && !(ok && entry.Kind == KindRoot) {
		collisions = append(collisions, fmt.Sprintf("directory %s exists but is not catalogued as Root: nothing adopts or moves it — rename it, or catalogue it as Root deliberately, before activating", filepath.Join(ticketsDirName, project.RootNamespace)))
	}
	return collisions
}

// markerExists reports whether a namespace directory holds its marker as a
// regular file. Lstat, for the reason lstatProjectDir gives: a symlink named
// like the marker is not the marker.
func markerExists(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, NamespaceMarker))
	return err == nil && info.Mode().IsRegular()
}

// directoryState reports whether a namespace directory holds any ticket
// file, selecting on the same suffix listStored reads: the marker, a stranded
// temp file and anything else in the directory are not tickets.
func directoryState(dir string) (NamespaceState, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("listing namespace %s: %w", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			return StatePresent, nil
		}
	}
	return StateEmpty, nil
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// WriteNamespaceMarker writes the tracked marker into a namespace directory,
// creating the directory if it is missing. It is the primitive activation
// materialises every admitted namespace with — Root included — so a healthy
// empty namespace survives a clone; no command calls it yet. The name is
// bounded the way CentralProjectDir bounds a project name, since it becomes a
// path element.
//
// The central store is a git repo and git tracks symlinks, so both the
// namespace directory and the marker can arrive as one from another
// committer. Neither is followed: the directory goes through lstatProjectDir
// like every other store write, and the marker is checked with Lstat and then
// published by renaming a temp file over it — a rename replaces a symlink at
// the destination rather than writing through it, where os.WriteFile would
// have truncated whatever the link pointed at.
func WriteNamespaceMarker(centralRoot, name string) error {
	if !project.ValidName(name) {
		return fmt.Errorf("invalid namespace name %q", name)
	}
	ticketsDir := filepath.Join(centralRoot, ticketsDirName)
	if _, err := lstatProjectDir(ticketsDir, name); err != nil {
		return err
	}
	dir := filepath.Join(ticketsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, NamespaceMarker)
	info, err := os.Lstat(path)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return fmt.Errorf("%s in %s is not a regular file — refusing to write outside the store", NamespaceMarker, dir)
	case err != nil && !os.IsNotExist(err):
		return err
	}
	content := "tk namespace marker: keeps this directory in git while it holds no tickets\n"
	tmp, err := os.CreateTemp(dir, ".tk-write-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(createMode); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// PreflightActivation reports what would stop activation from reserving _root
// and requiring its feature: a catalog this binary could not honour, a config
// entry that binds Root to a repository, or a _root directory on disk that is
// not already catalogued as Root. It never adopts or moves that directory —
// a collision is a rename decision for a human, taken before activation runs
// — and it writes nothing. Nil means the collision checks pass; the activation
// itself is not implemented here.
func PreflightActivation(centralRoot string, cfg project.Config) error {
	cat, err := LoadCatalog(centralRoot)
	if err != nil {
		return err
	}
	if err := cat.CheckFeatures(centralRoot); err != nil {
		return err
	}
	missing, err := lstatProjectDir(filepath.Join(centralRoot, ticketsDirName), project.RootNamespace)
	if err != nil {
		return err
	}
	collisions := rootCollisions(cfg, cat, !missing)
	if len(collisions) == 0 {
		return nil
	}
	return fmt.Errorf("cannot activate Root in %s: %s", centralRoot, strings.Join(collisions, "; "))
}
