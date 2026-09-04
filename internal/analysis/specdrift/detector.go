package specdrift

import (
	"fmt"
	"strings"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

// Detect returns spec-code drift findings.
// Heuristic for PoC: any .go/.ts file whose path looks like an API handler
// (contains "handler" or "controller" or under "api/", "routes/", "endpoints/")
// triggers a drift finding if NONE of specPaths is in the diff.
func Detect(diff []interfaces.DiffFile, specPaths []string) []interfaces.Finding {
	specChanged := false
	specSet := map[string]bool{}
	for _, p := range specPaths {
		specSet[p] = true
	}
	for _, f := range diff {
		if specSet[f.Path] {
			specChanged = true
		}
	}

	var handlers []string
	for _, f := range diff {
		if !looksLikeHandler(f.Path) {
			continue
		}
		handlers = append(handlers, f.Path)
	}
	if specChanged || len(handlers) == 0 {
		return nil
	}
	return []interfaces.Finding{{
		ID:       "specdrift-001",
		StableID: fmt.Sprintf("rollout_risk:spec_code_drift:%s", strings.Join(handlers, ",")),
		Severity: interfaces.SeverityMedium,
		Category: "spec_code_drift",
		Title:    "Endpoint handler changed without updating spec",
		Body: fmt.Sprintf("Files %s changed; corresponding spec files (%s) were not updated. Consider updating spec.",
			strings.Join(handlers, ", "), strings.Join(specPaths, ", ")),
		Suggestion: "Update OpenAPI / Protobuf spec to reflect handler changes.",
	}}
}

func looksLikeHandler(path string) bool {
	low := strings.ToLower(path)
	if !strings.HasSuffix(low, ".go") && !strings.HasSuffix(low, ".ts") && !strings.HasSuffix(low, ".tsx") {
		return false
	}
	keys := []string{"handler", "controller", "/api/", "/routes/", "/endpoints/"}
	for _, k := range keys {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}
