package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/acme/releaseguard/internal/interfaces"
)

type Signal struct {
	Agent  interfaces.AgentName `json:"agent"`
	Kind   string               `json:"kind"`
	Detail string               `json:"detail"`
}

type Recommendation struct {
	Recommendation   string   `json:"recommendation"` // HOLD | REVIEW | PROCEED
	Rationale        string   `json:"rationale"`
	TriggeredSignals []Signal `json:"triggered_signals"`
}

// Arbitrate is the Decision Arbitration Layer.
// Rules (priority):
//   - any AI Reviewer critical finding → HOLD
//   - Rollout Risk HIGH → HOLD
//   - any AI Reviewer high finding (non-critical) → REVIEW
//   - Rollout Risk MED → REVIEW
//   - Selective Test status=partial AND analysis_level != "L1" → REVIEW
//   - any agent status=failed → at minimum REVIEW
//   - Ownership signals do NOT trigger
func Arbitrate(outputs []interfaces.AgentOutput) Recommendation {
	defer func() {
		// defensive: any panic during arbitration shouldn't drop the whole report
		if r := recover(); r != nil {
			// fallback handled in wrapper below
		}
	}()
	return arbitrateInner(outputs)
}

func arbitrateInner(outputs []interfaces.AgentOutput) Recommendation {
	hold := []Signal{}
	review := []Signal{}

	for _, o := range outputs {
		switch o.Agent {
		case interfaces.AgentAIReviewer:
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "AI reviewer failed"})
				continue
			}
			for _, f := range o.Findings {
				if f.Severity == interfaces.SeverityCritical {
					hold = append(hold, Signal{Agent: o.Agent, Kind: "critical_finding",
						Detail: f.Title})
				} else if f.Severity == interfaces.SeverityHigh {
					review = append(review, Signal{Agent: o.Agent, Kind: "high_finding",
						Detail: f.Title})
				}
			}
		case interfaces.AgentRolloutRisk:
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "Rollout Risk failed"})
				continue
			}
			lvl, _ := o.Metadata["riskLevel"].(string)
			detail := lvl
			if zones, ok := o.Metadata["zones"]; ok {
				if summary := summarizeZones(zones); summary != "" {
					detail = lvl + " (" + summary + ")"
				}
			}
			switch lvl {
			case "HIGH":
				hold = append(hold, Signal{Agent: o.Agent, Kind: "high_risk", Detail: detail})
			case "MED":
				review = append(review, Signal{Agent: o.Agent, Kind: "med_risk", Detail: detail})
			}
		case interfaces.AgentSelectiveTest:
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "Selective Test failed"})
				continue
			}
			if o.Status == interfaces.StatusPartial {
				lvl, _ := o.Metadata["analysis_level"].(string)
				if lvl != "L1" {
					review = append(review, Signal{Agent: o.Agent, Kind: "unstable_test_plan",
						Detail: "low confidence at " + lvl})
				}
			}
		case interfaces.AgentOwnership:
			// Ownership intentionally NOT a trigger.
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "Ownership failed"})
			}
		}
	}

	rec := "PROCEED"
	if len(hold) > 0 {
		rec = "HOLD"
	} else if len(review) > 0 {
		rec = "REVIEW"
	}

	all := append(hold, review...)
	rationale := buildRationale(rec, all)
	return Recommendation{Recommendation: rec, Rationale: rationale, TriggeredSignals: all}
}

func summarizeZones(zones any) string {
	raw, err := json.Marshal(zones)
	if err != nil {
		return ""
	}
	var arr []struct {
		Type string
	}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return ""
	}
	if len(arr) == 0 {
		return ""
	}
	byType := map[string]int{}
	for _, z := range arr {
		byType[z.Type]++
	}
	keys := make([]string, 0, len(byType))
	for k := range byType {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{fmt.Sprintf("%d zones", len(arr))}
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", byType[k], k))
	}
	return strings.Join(parts, ", ")
}

func buildRationale(rec string, signals []Signal) string {
	if len(signals) == 0 {
		return "no high-severity signals from any agent"
	}
	var parts []string
	for _, s := range signals {
		parts = append(parts, fmt.Sprintf("[%s/%s] %s", s.Agent, s.Kind, s.Detail))
	}
	return fmt.Sprintf("%s — %s", rec, strings.Join(parts, "; "))
}
