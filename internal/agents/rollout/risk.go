package rollout

import (
	"github.com/twjohnwu/releaseGuard/internal/analysis/schema"
)

func riskLevel(zones []schema.Zone) string {
	c, h, m := 0, 0, 0
	for _, z := range zones {
		switch z.Severity {
		case "critical":
			c++
		case "high":
			h++
		case "medium":
			m++
		}
	}
	switch {
	case c >= 1:
		return "HIGH"
	case h >= 2 || m >= 3:
		return "MED"
	default:
		return "LOW"
	}
}
