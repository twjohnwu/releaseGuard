package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGoBuilderOnTinyFixture(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\ngo 1.23\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func A() { B() }
func B() {}

func main() { A() }
`), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	b := NewGoBuilder()
	r, err := b.Build(context.Background(), dir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	hasA, hasB := false, false
	for _, s := range r.Symbols {
		if s.ID == "fixture.A" {
			hasA = true
		}
		if s.ID == "fixture.B" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Fatalf("symbols missing: %+v", r.Symbols)
	}
	hasEdge := false
	for _, e := range r.Edges {
		if e.Caller == "fixture.A" && e.Callee == "fixture.B" {
			hasEdge = true
		}
	}
	if !hasEdge {
		t.Fatalf("A→B edge missing: %+v", r.Edges)
	}
}
