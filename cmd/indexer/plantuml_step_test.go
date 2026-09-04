package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestStepPlantUML_WalkToleratesUnreadableDir verifies that an unreadable
// subdirectory does not abort the whole step: stepPlantUML must log and
// continue instead of returning walkErr. No .puml files are readable in
// this fixture, so the insert loop never runs and pool can stay nil.
func TestStepPlantUML_WalkToleratesUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0o000 does not restrict access on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permission bits")
	}

	repoPath := t.TempDir()
	blocked := filepath.Join(repoPath, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatalf("mkdir blocked: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "diagram.puml"), []byte("@startuml\n@enduml\n"), 0o644); err != nil {
		t.Fatalf("write puml: %v", err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("chmod blocked: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o755); err != nil {
			t.Errorf("restore blocked perms: %v", err)
		}
	})

	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "diagram_aliases.yaml"), []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatalf("write aliases config: %v", err)
	}

	t.Setenv("DOCS_REPO_NAMES", "some-repo")
	t.Setenv("CONFIG_DIR", configDir)

	// pool is never touched: the only readable file tree entries are
	// unreadable/non-.puml, so the insert loop below the walk never runs.
	if err := stepPlantUML(context.Background(), nil, 0, repoPath); err != nil {
		t.Fatalf("stepPlantUML() error = %v, want nil (unreadable dir should be skipped, not fatal)", err)
	}
}
