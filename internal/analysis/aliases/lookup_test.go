package aliases

import (
	"path/filepath"
	"testing"
)

func TestLookupResolves(t *testing.T) {
	l, err := LoadFile(filepath.Join("..", "..", "..", "config", "diagram_aliases.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := l.Resolve("FE")
	if !ok || got != "web-dashboard" {
		t.Fatalf("FE → %q (ok=%v)", got, ok)
	}
}

func TestLookupSkip(t *testing.T) {
	l, _ := LoadFile(filepath.Join("..", "..", "..", "config", "diagram_aliases.yaml"))
	got, ok := l.Resolve("Postgres")
	if ok {
		t.Fatalf("expected skip for Postgres, got %q", got)
	}
}

func TestLookupUnknown(t *testing.T) {
	l, _ := LoadFile(filepath.Join("..", "..", "..", "config", "diagram_aliases.yaml"))
	if _, ok := l.Resolve("Unknown"); ok {
		t.Fatalf("expected unresolved")
	}
}
