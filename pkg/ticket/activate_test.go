package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// TestActivateEditsEveryCatalogShape: activation starts from no catalog, from
// one whose namespaces key holds nothing, and from one a staged activation
// left in flow style, and in each it adds only what is missing, keeps what is
// there as written, and produces a catalog every reader accepts.
func TestActivateEditsEveryCatalogShape(t *testing.T) {
	for _, tc := range []struct {
		name, catalog string
		keep          []string
	}{
		{name: "no catalog"},
		{name: "empty namespaces", catalog: "# kept\nnamespaces:\n", keep: []string{"# kept"}},
		{
			name:    "staged flow style",
			catalog: "required_features: [root-namespace]\nnamespaces:\n  _root: {kind: root}\n  old: {kind: legacy} # retired\n",
			keep:    []string{"[root-namespace, cross-project-parents]", "_root: {kind: root}", "old: {kind: legacy} # retired"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := catalogRoot(t)
			if tc.catalog != "" {
				writeCatalog(t, root, tc.catalog)
			}
			for _, ns := range []string{"proj", "old", project.RootNamespace} {
				if ns == project.RootNamespace && !strings.Contains(tc.catalog, "_root") {
					continue
				}
				mkNamespaceDir(t, root, ns)
			}
			cfg := registered(root, "proj")

			act, err := Activate(root, cfg, nil, false)
			if err != nil {
				t.Fatalf("Activate: %v", err)
			}
			if !act.Changed() {
				t.Fatal("activation of an unactivated store wrote nothing")
			}
			cat, err := LoadCatalog(root)
			if err != nil {
				t.Fatalf("activated catalog does not load: %v", err)
			}
			if !cat.RootActivated() || !cat.CrossProjectParentsActivated() || len(cat.RequiredFeatures) != 2 {
				t.Errorf("required features = %v", cat.RequiredFeatures)
			}
			for ns, kind := range map[string]NamespaceKind{project.RootNamespace: KindRoot, "proj": KindProject, "old": KindLegacy} {
				if cat.Namespaces[ns].Kind != kind {
					t.Errorf("%s catalogued as %q, want %q", ns, cat.Namespaces[ns].Kind, kind)
				}
			}
			raw, _ := os.ReadFile(CatalogPath(root))
			for _, want := range tc.keep {
				if !strings.Contains(string(raw), want) {
					t.Errorf("catalog lost %q:\n%s", want, raw)
				}
			}
			inv, err := ReadInventory(root, cfg)
			if err != nil || !inv.Complete {
				t.Errorf("inventory after activation: complete = %v, err = %v, %q", inv.Complete, err, inv.Diagnostics)
			}

			again, err := Activate(root, cfg, nil, false)
			if err != nil || again.Changed() {
				t.Errorf("re-activation = %+v, %v; want nothing to write", again, err)
			}
		})
	}
}

// TestActivatedCatalogWriterRefusesALinkedCatalog: the writer checks the file
// itself, not only the plan before it, so a catalog swapped for a symlink is
// neither read through nor replaced.
func TestActivatedCatalogWriterRefusesALinkedCatalog(t *testing.T) {
	root := catalogRoot(t)
	target := filepath.Join(t.TempDir(), "secret.yaml")
	const secret = "namespaces: {}\ntoken: dummy-secret-token\n"
	if err := os.WriteFile(target, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, CatalogPath(root)); err != nil {
		t.Fatal(err)
	}

	err := writeActivatedCatalog(root, []ActivatedNamespace{{Name: "proj", Kind: KindProject}}, []string{FeatureRootNamespace})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("writer over a linked catalog = %v, want refused", err)
	}
	if info, err := os.Lstat(CatalogPath(root)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link must be left as found (err %v)", err)
	}
	if got, _ := os.ReadFile(target); string(got) != secret {
		t.Errorf("link target changed: %q", got)
	}
}
