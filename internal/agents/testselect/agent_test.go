package testselect

import (
	"context"
	"strings"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

func TestL1MapsFileToTestSamePackage(t *testing.T) {
	a := New(false /* hasDB */)
	in := interfaces.AgentInput{
		Diff: []interfaces.DiffFile{
			{Path: "internal/diff/local_fetcher.go", Status: "modified"},
		},
	}
	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	md := out.Metadata
	if md["analysis_level"] != "L1" {
		t.Fatalf("expected L1, got %v", md["analysis_level"])
	}
	required := md["required"].([]string)
	if len(required) == 0 {
		t.Fatalf("L1 should infer at least same-package tests")
	}
}

func TestL1ConfidenceCapped(t *testing.T) {
	a := New(false)
	out, _ := a.Run(context.Background(), interfaces.AgentInput{
		Diff: []interfaces.DiffFile{
			{Path: "x.go", Status: "modified"},
			{Path: "y.ts", Status: "modified"}, // mixed languages
		},
	})
	conf := out.Metadata["confidence"].(float64)
	if conf > 0.6 {
		t.Fatalf("L1 confidence must be <= 0.6 (mixed lang penalty), got %f", conf)
	}
}

func TestL1SkipsNonGoTestPaths(t *testing.T) {
	a := New(false)
	out, _ := a.Run(context.Background(), interfaces.AgentInput{
		Diff: []interfaces.DiffFile{
			{Path: "internal/foo.go", Status: "modified"},
			{Path: "config/x.yaml", Status: "modified"},
		},
	})
	required := out.Metadata["required"].([]string)
	for _, r := range required {
		if strings.HasSuffix(r, ".yaml_test.go") || strings.HasSuffix(r, ".yml_test.go") {
			t.Errorf("L1 produced bogus YAML _test.go path: %s", r)
		}
	}
}

func TestL1SkipsRootDotGlob(t *testing.T) {
	a := New(false)
	out, _ := a.Run(context.Background(), interfaces.AgentInput{
		Diff: []interfaces.DiffFile{
			{Path: "README.md", Status: "modified"},
		},
	})
	required := out.Metadata["required"].([]string)
	for _, r := range required {
		if r == "./..." {
			t.Errorf("L1 produced './...' for root-level file")
		}
	}
}
