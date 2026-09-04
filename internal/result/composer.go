package result

import (
	"encoding/json"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
	"github.com/twjohnwu/releaseGuard/internal/report"
)

type Flags struct {
	SelectiveTest bool
	RolloutRisk   bool
	Ownership     bool
	AIReviewer    bool
}

func Compose(outputs []interfaces.AgentOutput, flags Flags) string {
	md, _ := ComposeWithDecision(outputs, flags)
	return md
}

// ComposeWithDecision renders the MR comment markdown and also returns the
// arbitration decision, so callers (e.g. the JSON report writer) can serialize
// the decision + triggered_signals without re-running arbitration.
func ComposeWithDecision(outputs []interfaces.AgentOutput, flags Flags) (string, report.Recommendation) {
	enabled := filterByFlags(outputs, flags)
	rec := report.Arbitrate(enabled)
	r := report.ImpactScopeReport{
		Recommendation: rec,
		EnabledFlags:   asMap(flags),
	}
	for _, o := range enabled {
		switch o.Agent {
		case interfaces.AgentRolloutRisk:
			if lvl, ok := o.Metadata["riskLevel"].(string); ok {
				r.RiskLevel = lvl
			}
			if zones, ok := o.Metadata["zones"]; ok {
				r.RiskZones = extractZones(zones)
			}
		case interfaces.AgentSelectiveTest:
			if flags.SelectiveTest {
				r.SelectiveTests = extractTestPlan(o)
			}
		case interfaces.AgentAIReviewer:
			r.AIReviewFindings = o.Findings
		}
		if o.Status == interfaces.StatusFailed {
			r.FailedAgents = append(r.FailedAgents, string(o.Agent))
		}
	}
	r.HighSeverityFindings = report.FlattenFindings(enabled)
	return report.Render(r), rec
}

func filterByFlags(outs []interfaces.AgentOutput, f Flags) []interfaces.AgentOutput {
	out := make([]interfaces.AgentOutput, 0, len(outs))
	for _, o := range outs {
		switch o.Agent {
		case interfaces.AgentSelectiveTest:
			if f.SelectiveTest {
				out = append(out, o)
			}
		case interfaces.AgentRolloutRisk:
			if f.RolloutRisk {
				out = append(out, o)
			}
		case interfaces.AgentOwnership:
			if f.Ownership {
				out = append(out, o)
			}
		case interfaces.AgentAIReviewer:
			if f.AIReviewer {
				out = append(out, o)
			}
		}
	}
	return out
}

func extractTestPlan(o interfaces.AgentOutput) *report.TestPlan {
	tp := &report.TestPlan{}
	if v, ok := o.Metadata["required"].([]string); ok {
		tp.Required = v
	}
	if v, ok := o.Metadata["skippable"].([]string); ok {
		tp.Skippable = v
	}
	if v, ok := o.Metadata["confidence"].(float64); ok {
		tp.Confidence = v
	}
	if v, ok := o.Metadata["analysis_level"].(string); ok {
		tp.AnalysisLevel = v
	}
	if v, ok := o.Metadata["reason"].(string); ok {
		tp.Reason = v
	}
	return tp
}

func extractZones(zones any) []report.ZoneInfo {
	raw, err := json.Marshal(zones)
	if err != nil {
		return nil
	}
	var arr []struct {
		Type, Detail, Severity string
	}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	out := make([]report.ZoneInfo, 0, len(arr))
	for _, z := range arr {
		out = append(out, report.ZoneInfo{Type: z.Type, Detail: z.Detail, Severity: z.Severity})
	}
	return out
}

func asMap(f Flags) map[string]bool {
	return map[string]bool{
		"selective_test": f.SelectiveTest,
		"rollout_risk":   f.RolloutRisk,
		"ownership":      f.Ownership,
		"ai_reviewer":    f.AIReviewer,
	}
}
