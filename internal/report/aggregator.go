package report

import (
	"sort"

	"github.com/acme/releaseguard/internal/interfaces"
)

var sevWeight = map[interfaces.Severity]int{
	interfaces.SeverityCritical: 5,
	interfaces.SeverityHigh:     4,
	interfaces.SeverityMedium:   3,
	interfaces.SeverityLow:      2,
	interfaces.SeverityInfo:     1,
}

func FlattenFindings(outputs []interfaces.AgentOutput) []interfaces.Finding {
	seen := map[string]bool{}
	var out []interfaces.Finding
	for _, o := range outputs {
		for _, f := range o.Findings {
			if seen[f.StableID] {
				continue
			}
			seen[f.StableID] = true
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return sevWeight[out[i].Severity] > sevWeight[out[j].Severity]
	})
	return out
}
