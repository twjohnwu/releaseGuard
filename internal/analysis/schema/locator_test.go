package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateOpenAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "api"), 0755); err != nil {
		t.Fatalf("mkdir api: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api", "openapi.yaml"), []byte("openapi: 3.0.0"), 0644); err != nil {
		t.Fatalf("write OpenAPI fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service.proto"), []byte("syntax = \"proto3\";"), 0644); err != nil {
		t.Fatalf("write Protobuf fixture: %v", err)
	}

	specs, err := LocateInDir(dir)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(specs.OpenAPIs) != 1 || len(specs.Protos) != 1 {
		t.Fatalf("expected 1 each, got %+v", specs)
	}
}
