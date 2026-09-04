package reviewer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMDInOrder(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{"_shared", "_shared/backend", "my-system"} {
		if err := os.MkdirAll(filepath.Join(dir, path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	files := map[string]string{
		"_shared/01-base.md":        "base\n",
		"_shared/02-extra.md":       "extra\n",
		"_shared/backend/api.md":    "api\n",
		"my-system/review-focus.md": "focus\n",
	}
	for path, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(contents), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	loaded, err := LoadPromptFiles(dir, "my-system", []string{"backend"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded[0] != "base\n" || loaded[1] != "extra\n" {
		t.Fatalf("shared root order broken: %v", loaded[:2])
	}
	if loaded[2] != "api\n" {
		t.Fatalf("backend not after shared: %v", loaded)
	}
	if loaded[3] != "focus\n" {
		t.Fatalf("system not last: %v", loaded)
	}
}
