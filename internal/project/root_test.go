package project

import "testing"

func TestIsRoot(t *testing.T) {
	if !IsRoot(RootNamespace) {
		t.Errorf("IsRoot(%q) = false", RootNamespace)
	}
	for _, name := range []string{"root", "Root", "_root/", ""} {
		if IsRoot(name) {
			t.Errorf("IsRoot(%q) = true", name)
		}
	}
	// The reserved name has to be a valid path element: it is joined into
	// <central_root>/tickets/ like every other namespace.
	if !ValidName(RootNamespace) {
		t.Errorf("ValidName(%q) = false", RootNamespace)
	}
}
