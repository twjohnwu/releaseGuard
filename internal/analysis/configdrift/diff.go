package configdrift

import (
	"strings"

	"github.com/twjohnwu/releaseGuard/internal/analysis/schema"
	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

type Zone = schema.Zone

func classify(path string) string {
	low := strings.ToLower(path)
	switch {
	case strings.Contains(low, "secret") || strings.Contains(low, "credential"):
		return "critical"
	case strings.HasPrefix(low, "k8s/") || strings.Contains(low, "deployment"):
		return "high"
	case strings.HasPrefix(low, "helm/") ||
		strings.Contains(low, "feature") ||
		strings.HasPrefix(low, "config/"):
		return "medium"
	default:
		return "low"
	}
}

// FromDiff classifies each changed config file. Caller filters via globs upstream.
func FromDiff(files []interfaces.DiffFile) []Zone {
	var zones []Zone
	for _, f := range files {
		if !looksLikeConfig(f.Path) {
			continue
		}
		zones = append(zones, Zone{
			Type:     "config_drift",
			Detail:   f.Path,
			Severity: classify(f.Path),
		})
	}
	return zones
}

func looksLikeConfig(p string) bool {
	low := strings.ToLower(p)
	if strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml") ||
		strings.HasSuffix(low, ".env") || strings.HasSuffix(low, ".conf") {
		return strings.HasPrefix(low, "config/") || strings.HasPrefix(low, "helm/") ||
			strings.HasPrefix(low, "k8s/") || strings.Contains(low, "secret")
	}
	return false
}
