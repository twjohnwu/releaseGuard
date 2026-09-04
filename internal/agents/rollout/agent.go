package rollout

import (
	"context"
	"fmt"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/analysis/schema"
	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

type Zone = schema.Zone

type Deps struct {
	OASDiff func(base, head string) ([]Zone, error)
	Zones   []Zone // direct injection point: caller pre-computes zones from sub-analyses
}

type Agent struct {
	deps            Deps
	testInjectZones []Zone // for tests
}

func New(deps Deps) *Agent { return &Agent{deps: deps} }

func (a *Agent) Name() interfaces.AgentName { return interfaces.AgentRolloutRisk }

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	var zones []Zone
	if a.testInjectZones != nil {
		zones = a.testInjectZones
	} else if a.deps.OASDiff != nil {
		z, err := a.deps.OASDiff("", "")
		if err == nil {
			zones = append(zones, z...)
		}
	}
	if len(a.deps.Zones) > 0 {
		zones = append(zones, a.deps.Zones...)
	}
	level := riskLevel(zones)
	findings := zonesToFindings(zones)
	return interfaces.AgentOutput{
		Agent:         interfaces.AgentRolloutRisk,
		Status:        interfaces.StatusOK,
		DurationMs:    int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1",
		Findings:      findings,
		Summary:       "rollout risk evaluated",
		Metadata: map[string]any{
			"riskLevel": level,
			"zones":     zones,
		},
	}, nil
}

func zonesToFindings(zones []Zone) []interfaces.Finding {
	out := make([]interfaces.Finding, 0, len(zones))
	for i, z := range zones {
		sev := interfaces.ParseSeverity(z.Severity)
		if sev == interfaces.SeverityInfo {
			sev = interfaces.SeverityLow // rollout treats unknown severity as low risk
		}
		out = append(out, interfaces.Finding{
			ID:       fmt.Sprintf("rl-%03d", i+1),
			StableID: fmt.Sprintf("rollout_risk:%s:%s", z.Type, z.Detail),
			Severity: sev,
			Category: z.Type,
			Title:    z.Detail,
			Body:     z.Detail,
		})
	}
	return out
}
