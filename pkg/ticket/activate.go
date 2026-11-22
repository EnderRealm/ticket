package ticket

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"gopkg.in/yaml.v3"
)

// ErrActivationRefused is an activation that wrote nothing because the store
// is not in a state it can be activated from. Checked with errors.Is; the
// wrapped message names every cause at once, so one run lists all the repairs
// rather than one per attempt.
var ErrActivationRefused = errors.New("activation refused")

// DefaultActivationFeatures is what Activate requires when the caller names no
// feature: Root and cross-project parents, the pair the catalog guard gates.
var DefaultActivationFeatures = []string{FeatureRootNamespace, FeatureCrossProjectParents}

// ActivatedNamespace is one namespace an activation adds to the catalog, with
// the kind it is admitted as.
type ActivatedNamespace struct {
	Name string        `json:"name"`
	Kind NamespaceKind `json:"kind"`
}

// Activation is what Activate wrote or, with DryRun, would write. Each list
// holds only what changes: a feature already required, a namespace already
// catalogued and a marker already present are left out, so an activation of
// an activated store is all empty lists.
type Activation struct {
	CentralRoot string `json:"central_root"`
	DryRun      bool   `json:"dry_run"`
	// Features is the required features added, in the order requested.
	Features []string `json:"features"`
	// Catalogued is the namespaces added to the catalog. An entry the catalog
	// already holds keeps its kind and is not listed.
	Catalogued []ActivatedNamespace `json:"catalogued"`
	// Markers is the namespaces whose .namespace marker is written.
	Markers []string `json:"markers"`
	// Inventory is the store as read before anything was written.
	Inventory Inventory `json:"inventory"`
}

// Changed reports whether the activation writes anything.
func (a *Activation) Changed() bool {
	return len(a.Features)+len(a.Catalogued)+len(a.Markers) > 0
}

// Activate turns on the requested catalog features — DefaultActivationFeatures
// when none is named — in the central store at centralRoot: it catalogues every
// inventoried namespace that is not yet (Root as root, a registered project as
// project, anything else as legacy), writes each namespace's marker, and adds
// the features to required_features. It never reads a ticket in order to
// rewrite it, never moves a directory and never invents a registration: the
// ticket files are byte-identical afterwards, and adoption into a foreign epic
// is a later, explicit edit.
//
// Everything is decided before anything is written, under the exclusive store
// lock — shared for a dry run, which writes nothing — so no snapshot or write
// on this machine sees a half-activated store. It refuses, writing nothing,
// on: a catalog that cannot be read or requires a feature this binary lacks;
// a requested feature outside SupportedFeatures; no tickets directory; a Root
// collision (PreflightActivation); a namespace the inventory cannot complete
// — catalogued with no directory, an invalid name, a path that is not a real
// directory, a marker that is not a regular file; and, when cross-project
// parents are being turned on, any stored ticket already naming a parent in
// another namespace, or a store too unreadable to prove none does, since the
// feature would silently make that ticket the foreign epic's child.
//
// The writes are ordered so a failure part-way leaves a store a re-run
// completes: the catalog entries first — Root's among them, so its directory
// is never an uncatalogued collision — then the markers, and the features
// last, so a store is never activated without its namespaces materialised.
// Re-running an activated store writes nothing.
func Activate(centralRoot string, cfg project.Config, features []string, dryRun bool) (*Activation, error) {
	if len(features) == 0 {
		features = DefaultActivationFeatures
	}
	var act *Activation
	err := WithStoreLock(centralRoot, !dryRun, func() error {
		var err error
		act, err = planActivation(centralRoot, cfg, features)
		if err != nil {
			return err
		}
		act.DryRun = dryRun
		if dryRun || !act.Changed() {
			return nil
		}
		return applyActivation(act)
	})
	if err != nil {
		return nil, err
	}
	return act, nil
}

// planActivation reads the store and decides what Activate writes, collecting
// every refusal. Called with the store lock held; it writes nothing.
func planActivation(centralRoot string, cfg project.Config, requested []string) (*Activation, error) {
	if err := regularCatalog(centralRoot); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrActivationRefused, err)
	}
	cat, err := LoadCatalog(centralRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrActivationRefused, err)
	}
	if err := cat.CheckFeatures(centralRoot); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrActivationRefused, err)
	}

	act := &Activation{CentralRoot: centralRoot, Features: []string{}, Catalogued: []ActivatedNamespace{}, Markers: []string{}}
	var reasons []string
	for _, f := range requested {
		switch {
		case !slices.Contains(SupportedFeatures, f):
			reasons = append(reasons, fmt.Sprintf("feature %q is not one this tk implements (supports: %s)", f, strings.Join(SupportedFeatures, ", ")))
		case !cat.Requires(f) && !slices.Contains(act.Features, f):
			act.Features = append(act.Features, f)
		}
	}

	ticketsDir := filepath.Join(centralRoot, ticketsDirName)
	// Lstat, and refused before anything under it is read: git can deliver
	// tickets/ itself as a symlink, and every namespace check below bounds
	// only the final path component, so a linked tickets/ would have markers
	// and Root's directory written wherever it points.
	info, err := os.Lstat(ticketsDir)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return nil, fmt.Errorf("%w for %s: %s is a symlink — refusing to write outside the store", ErrActivationRefused, centralRoot, ticketsDir)
	case err == nil && !info.IsDir():
		return nil, fmt.Errorf("%w for %s: %s is not a directory — refusing to write outside the store", ErrActivationRefused, centralRoot, ticketsDir)
	case err != nil:
		reasons = append(reasons, fmt.Sprintf("no tickets directory at %s: this is not a central store, or it was never cloned here", ticketsDir))
	}
	if err := PreflightActivation(centralRoot, cfg); err != nil {
		reasons = append(reasons, err.Error())
	}

	inv, err := ReadInventory(centralRoot, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrActivationRefused, err)
	}
	act.Inventory = inv
	for _, ns := range inv.Namespaces {
		if !project.ValidName(ns.Name) {
			reasons = append(reasons, fmt.Sprintf("namespace %q is not a valid name: it has to be a bare name with no path separator, and not . or .. — rename its config entry", ns.Name))
			continue
		}
		_, catalogued := cat.entry(ns.Name)
		// Root is the one catalogued namespace activation may materialise: it
		// is built in, and no tickets of it can exist anywhere before the
		// feature allows the first write.
		if catalogued && !ns.Directory && !project.IsRoot(ns.Name) {
			reasons = append(reasons, fmt.Sprintf("namespace %q is catalogued but has no directory under %s: its tickets may exist on another machine — sync the store, or remove it from the catalog deliberately; activation never creates it empty", ns.Name, ticketsDir))
			continue
		}
		if _, err := lstatProjectDir(ticketsDir, ns.Name); err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		if info, err := os.Lstat(filepath.Join(ticketsDir, ns.Name, NamespaceMarker)); err == nil && !info.Mode().IsRegular() {
			reasons = append(reasons, fmt.Sprintf("%s in %s is not a regular file — remove it before activating", NamespaceMarker, filepath.Join(ticketsDir, ns.Name)))
			continue
		}
		if !catalogued {
			act.Catalogued = append(act.Catalogued, ActivatedNamespace{Name: ns.Name, Kind: ns.Kind})
		}
		if !ns.Marker {
			act.Markers = append(act.Markers, ns.Name)
		}
	}

	if slices.Contains(act.Features, FeatureCrossProjectParents) {
		reasons = append(reasons, legacyForeignParents(centralRoot)...)
	}

	if len(reasons) > 0 {
		return nil, fmt.Errorf("%w for %s: %s", ErrActivationRefused, centralRoot, strings.Join(reasons, "; "))
	}
	return act, nil
}

// legacyForeignParents is what stops cross-project parents being turned on
// without review: every stored ticket whose parent already names another
// namespace. Before the feature such a ticket is no epic's child; after it,
// it would be one with nobody having chosen that. An incomplete snapshot is a
// refusal of its own — a file that could not be read may hold such a parent.
// Called with the store lock held, so it reads the snapshot without taking it.
func legacyForeignParents(centralRoot string) []string {
	c := &central{root: centralRoot, ticketsDir: filepath.Join(centralRoot, ticketsDirName)}
	snap, err := c.snapshotLocked()
	if err != nil {
		return []string{fmt.Sprintf("cannot read the store to check for parents in other namespaces: %v", err)}
	}
	var reasons []string
	if !snap.Complete {
		reasons = append(reasons, fmt.Sprintf("%s needs a complete read of the store to prove no ticket already names a parent in another namespace, and this one is not: %s",
			FeatureCrossProjectParents, strings.Join(snap.Diagnostics(), "; ")))
	}
	var foreign []string
	for _, t := range snap.Tickets {
		if t.Parent == "" {
			continue
		}
		ns := namespaceOf(t.ID)
		if parent := qualifyRef(ns, t.Parent); namespaceOf(parent) != ns {
			foreign = append(foreign, fmt.Sprintf("%s (parent %s)", t.ID, parent))
		}
	}
	if len(foreign) > 0 {
		slices.Sort(foreign)
		reasons = append(reasons, fmt.Sprintf("tickets already name a parent in another namespace, which %s would adopt without review: %s — clear each parent, then re-add the intended ones through the reviewed adoption list once activated",
			FeatureCrossProjectParents, strings.Join(foreign, ", ")))
	}
	return reasons
}

// applyActivation writes a planned activation in the order Activate describes.
func applyActivation(act *Activation) error {
	if len(act.Catalogued) > 0 {
		if err := writeActivatedCatalog(act.CentralRoot, act.Catalogued, nil); err != nil {
			return err
		}
	}
	for _, name := range act.Markers {
		if err := WriteNamespaceMarker(act.CentralRoot, name); err != nil {
			return fmt.Errorf("writing %s marker for %q: %w — the catalog entries are written; re-run activation once it is repaired", NamespaceMarker, name, err)
		}
	}
	if len(act.Features) > 0 {
		if err := writeActivatedCatalog(act.CentralRoot, nil, act.Features); err != nil {
			return err
		}
	}
	return nil
}

// writeActivatedCatalog adds namespaces and required features to the catalog
// and replaces the file atomically. The file is edited as a YAML node tree
// rather than re-marshalled from Catalog, so the entries, ordering, styles and
// comments already in it survive; what is added is appended. The result is
// parsed by the rules LoadCatalog applies before it replaces anything.
func writeActivatedCatalog(centralRoot string, add []ActivatedNamespace, features []string) error {
	path := CatalogPath(centralRoot)
	if err := regularCatalog(centralRoot); err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading catalog %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parsing catalog %s: %w", path, err)
	}
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return fmt.Errorf("catalog %s is not a mapping", path)
	}
	if len(features) > 0 {
		seq, err := catalogField(top, "required_features", yaml.SequenceNode, true)
		if err != nil {
			return fmt.Errorf("catalog %s: %w", path, err)
		}
		for _, f := range features {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f})
		}
	}
	if len(add) > 0 {
		namespaces, err := catalogField(top, "namespaces", yaml.MappingNode, false)
		if err != nil {
			return fmt.Errorf("catalog %s: %w", path, err)
		}
		for _, ns := range add {
			namespaces.Content = append(namespaces.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ns.Name},
				&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "kind"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(ns.Kind)},
				}})
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encoding catalog %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encoding catalog %s: %w", path, err)
	}
	written, err := parseCatalog(path, buf.Bytes())
	if err != nil {
		return fmt.Errorf("activation would write an invalid catalog: %w", err)
	}
	for _, ns := range add {
		if entry, ok := written.entry(ns.Name); !ok || entry.Kind != ns.Kind {
			return fmt.Errorf("activation would write a catalog without %s as %s", ns.Name, ns.Kind)
		}
	}
	for _, f := range features {
		if !written.Requires(f) {
			return fmt.Errorf("activation would write a catalog that does not require %s", f)
		}
	}

	tmp, err := createWriteTemp(centralRoot, 0o644)
	if err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	// A rename replaces a symlinked catalog.yaml rather than writing through
	// it, for the reason WriteNamespaceMarker gives.
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// regularCatalog refuses a catalog.yaml that exists as anything but a regular
// file. git can deliver it as a symlink, and activation reads it and writes
// what it read back into the store as a regular file that `tk sync` stages —
// so a linked catalog would copy whatever it points at, credentials
// included, into the shared remote. Checked before anything reads it.
func regularCatalog(centralRoot string) error {
	path := CatalogPath(centralRoot)
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("catalog %s: %w", path, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("catalog %s is not a regular file (%s) — refusing to read or replace it: replace it with the catalog's own contents", path, info.Mode().Type())
	}
	return nil
}

// catalogField returns the value node under key in the catalog's top-level
// mapping, adding the key — first or last — when it is absent, and replacing
// an empty value (`namespaces:` with nothing under it) with an empty node of
// the wanted kind. A value of another kind is an error, never overwritten.
func catalogField(top *yaml.Node, key string, kind yaml.Kind, first bool) (*yaml.Node, error) {
	tag := "!!seq"
	if kind == yaml.MappingNode {
		tag = "!!map"
	}
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value != key {
			continue
		}
		value := top.Content[i+1]
		switch {
		case value.Kind == kind:
			return value, nil
		case value.Kind == yaml.ScalarNode && value.Tag == "!!null":
			*value = yaml.Node{Kind: kind, Tag: tag, LineComment: value.LineComment}
			return value, nil
		default:
			return nil, fmt.Errorf("%s is not a %s", key, tag)
		}
	}
	value := &yaml.Node{Kind: kind, Tag: tag}
	pair := []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value}
	if first {
		top.Content = append(pair, top.Content...)
	} else {
		top.Content = append(top.Content, pair...)
	}
	return value, nil
}
