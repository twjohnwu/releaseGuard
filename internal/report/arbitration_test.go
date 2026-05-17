package report

import (
	"strings"
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func critical() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusOK,
		Findings: []interfaces.Finding{{Severity: interfaces.SeverityCritical, Title: "x"}}}
}
func high() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusOK,
		Findings: []interfaces.Finding{{Severity: interfaces.SeverityHigh, Title: "x"}}}
}
func rolloutHIGH() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentRolloutRisk, Status: interfaces.StatusOK,
		Metadata: map[string]any{"riskLevel": "HIGH"}}
}
func rolloutMED() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentRolloutRisk, Status: interfaces.StatusOK,
		Metadata: map[string]any{"riskLevel": "MED"}}
}
func selectivePartialL3() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentSelectiveTest, Status: interfaces.StatusPartial,
		Metadata: map[string]any{"analysis_level": "L3"}}
}
func selectivePartialL1() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentSelectiveTest, Status: interfaces.StatusPartial,
		Metadata: map[string]any{"analysis_level": "L1"}}
}
func ownership() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentOwnership, Status: interfaces.StatusOK,
		Metadata: map[string]any{"hidden_dependency_hints": []any{1, 2, 3}}}
}
func failed(name interfaces.AgentName) interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: name, Status: interfaces.StatusFailed}
}

func TestArbitrationRules(t *testing.T) {
	cases := []struct {
		name           string
		outputs        []interfaces.AgentOutput
		want           string
		signalsAtLeast int
	}{
		{"critical → HOLD", []interfaces.AgentOutput{critical()}, "HOLD", 1},
		{"rollout HIGH → HOLD", []interfaces.AgentOutput{rolloutHIGH()}, "HOLD", 1},
		{"high finding → REVIEW", []interfaces.AgentOutput{high()}, "REVIEW", 1},
		{"rollout MED → REVIEW", []interfaces.AgentOutput{rolloutMED()}, "REVIEW", 1},
		{"L3 partial → REVIEW", []interfaces.AgentOutput{selectivePartialL3()}, "REVIEW", 1},
		{"L1 partial → PROCEED", []interfaces.AgentOutput{selectivePartialL1()}, "PROCEED", 0},
		{"ownership only → PROCEED", []interfaces.AgentOutput{ownership()}, "PROCEED", 0},
		{"agent failed → REVIEW", []interfaces.AgentOutput{failed(interfaces.AgentRolloutRisk)}, "REVIEW", 1},
		{"all failed → REVIEW", []interfaces.AgentOutput{
			failed(interfaces.AgentAIReviewer), failed(interfaces.AgentRolloutRisk),
		}, "REVIEW", 2},
		{"empty → PROCEED", []interfaces.AgentOutput{}, "PROCEED", 0},
		{"critical + HIGH risk → HOLD with 2 signals", []interfaces.AgentOutput{
			critical(), rolloutHIGH(),
		}, "HOLD", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := Arbitrate(c.outputs)
			if rec.Recommendation != c.want {
				t.Errorf("got %s want %s", rec.Recommendation, c.want)
			}
			if len(rec.TriggeredSignals) < c.signalsAtLeast {
				t.Errorf("signal count: got %d want >=%d", len(rec.TriggeredSignals), c.signalsAtLeast)
			}
		})
	}
}

func TestArbitrationDetailIncludesZoneBreakdown(t *testing.T) {
	out := interfaces.AgentOutput{Agent: interfaces.AgentRolloutRisk, Status: interfaces.StatusOK,
		Metadata: map[string]any{
			"riskLevel": "MED",
			"zones": []map[string]any{
				{"Type": "config_drift", "Severity": "medium"},
				{"Type": "config_drift", "Severity": "medium"},
				{"Type": "spec_code_drift", "Severity": "medium"},
			},
		},
	}
	rec := Arbitrate([]interfaces.AgentOutput{out})
	if rec.Recommendation != "REVIEW" {
		t.Fatalf("rec: %s", rec.Recommendation)
	}
	if len(rec.TriggeredSignals) != 1 {
		t.Fatalf("signals: %d", len(rec.TriggeredSignals))
	}
	detail := rec.TriggeredSignals[0].Detail
	if !strings.Contains(detail, "MED") {
		t.Errorf("expected MED in detail: %q", detail)
	}
	if !strings.Contains(detail, "3 zones") {
		t.Errorf("expected zone count in detail: %q", detail)
	}
}
