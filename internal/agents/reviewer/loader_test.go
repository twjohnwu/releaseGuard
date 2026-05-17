package reviewer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMDInOrder(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "_shared"), 0755)
	os.WriteFile(filepath.Join(dir, "_shared/01-base.md"), []byte("base\n"), 0644)
	os.WriteFile(filepath.Join(dir, "_shared/02-extra.md"), []byte("extra\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "_shared/backend"), 0755)
	os.WriteFile(filepath.Join(dir, "_shared/backend/api.md"), []byte("api\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "my-system"), 0755)
	os.WriteFile(filepath.Join(dir, "my-system/review-focus.md"), []byte("focus\n"), 0644)

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
