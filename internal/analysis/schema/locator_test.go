package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateOpenAPI(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "api"), 0755)
	os.WriteFile(filepath.Join(dir, "api", "openapi.yaml"), []byte("openapi: 3.0.0"), 0644)
	os.WriteFile(filepath.Join(dir, "service.proto"), []byte("syntax = \"proto3\";"), 0644)

	specs, err := LocateInDir(dir)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(specs.OpenAPIs) != 1 || len(specs.Protos) != 1 {
		t.Fatalf("expected 1 each, got %+v", specs)
	}
}
