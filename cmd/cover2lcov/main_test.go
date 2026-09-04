package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripModulePrefix(t *testing.T) {
	cases := []struct {
		in, mod, want string
	}{
		{"github.com/twjohnwu/releaseGuard/internal/report/arbitration.go", "github.com/twjohnwu/releaseGuard", "internal/report/arbitration.go"},
		{"unrelated/path/file.go", "github.com/twjohnwu/releaseGuard", "unrelated/path/file.go"},
		{"some/file.go", "", "some/file.go"},
	}
	for _, c := range cases {
		got := stripModulePrefix(c.in, c.mod)
		if got != c.want {
			t.Errorf("stripModulePrefix(%q,%q)=%q want %q", c.in, c.mod, got, c.want)
		}
	}
}

func TestDetectModule(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/foo/bar\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := detectModule(); got != "github.com/foo/bar" {
		t.Errorf("detectModule=%q", got)
	}
}

func TestDetectModuleMissing(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if got := detectModule(); got != "" {
		t.Errorf("detectModule=%q want empty", got)
	}
}
