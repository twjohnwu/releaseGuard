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
		{"github.com/acme/releaseguard/internal/report/arbitration.go", "github.com/acme/releaseguard", "internal/report/arbitration.go"},
		{"unrelated/path/file.go", "github.com/acme/releaseguard", "unrelated/path/file.go"},
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
	old, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(old) })
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
	old, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if got := detectModule(); got != "" {
		t.Errorf("detectModule=%q want empty", got)
	}
}
