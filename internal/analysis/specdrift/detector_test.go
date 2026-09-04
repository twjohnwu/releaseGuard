package specdrift

import (
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

func TestDetectsHandlerChangedSpecUntouched(t *testing.T) {
	files := []interfaces.DiffFile{
		{Path: "internal/orders/handler.go", Status: "modified"},
		{Path: "README.md", Status: "modified"},
		// note: no api/openapi.yaml in diff
	}
	specPaths := []string{"api/openapi.yaml"}
	findings := Detect(files, specPaths)
	if len(findings) != 1 {
		t.Fatalf("expected 1 drift finding, got %d", len(findings))
	}
	if findings[0].Severity != interfaces.SeverityMedium {
		t.Fatalf("severity wrong: %s", findings[0].Severity)
	}
}

func TestNoDriftWhenSpecAlsoChanged(t *testing.T) {
	files := []interfaces.DiffFile{
		{Path: "internal/orders/handler.go", Status: "modified"},
		{Path: "api/openapi.yaml", Status: "modified"},
	}
	findings := Detect(files, []string{"api/openapi.yaml"})
	if len(findings) != 0 {
		t.Fatalf("expected no drift, got %+v", findings)
	}
}
