package depgraph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoModDiff(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.mod")
	head := filepath.Join(dir, "head.mod")
	if err := os.WriteFile(base, []byte(`module x
go 1.23
require github.com/foo/bar v1.0.0
require github.com/baz/qux v2.0.0`), 0644); err != nil {
		t.Fatalf("write base go.mod: %v", err)
	}
	if err := os.WriteFile(head, []byte(`module x
go 1.23
require github.com/foo/bar v2.0.0
require github.com/baz/qux v2.0.0
require github.com/new/dep v0.1.0`), 0644); err != nil {
		t.Fatalf("write head go.mod: %v", err)
	}

	zones, err := GoModDiff(base, head)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	gotMajor := false
	gotAdded := false
	for _, z := range zones {
		if z.Type == "dep_bump" && z.Severity == "high" {
			gotMajor = true
		}
		if z.Type == "dep_added" {
			gotAdded = true
		}
	}
	if !gotMajor {
		t.Fatalf("major bump not flagged: %+v", zones)
	}
	if !gotAdded {
		t.Fatalf("new dep not flagged: %+v", zones)
	}
}
