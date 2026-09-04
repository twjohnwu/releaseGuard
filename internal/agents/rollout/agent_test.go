package rollout

import (
	"context"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

func TestRiskLevelHIGHWhenCriticalZone(t *testing.T) {
	a := New(Deps{
		OASDiff: func(base, head string) ([]Zone, error) {
			return []Zone{{Type: "breaking_api", Detail: "removed field", Severity: "critical"}}, nil
		},
	})
	out, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Metadata["riskLevel"] != "HIGH" {
		t.Fatalf("expected HIGH, got %v", out.Metadata["riskLevel"])
	}
}

func TestRiskLevelMEDOnAccumulation(t *testing.T) {
	a := New(Deps{})
	a.testInjectZones = []Zone{
		{Type: "dep_bump", Severity: "medium"},
		{Type: "dep_bump", Severity: "medium"},
		{Type: "dep_bump", Severity: "medium"},
	}
	out, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Metadata["riskLevel"] != "MED" {
		t.Fatalf("expected MED, got %v", out.Metadata["riskLevel"])
	}
}

func TestRiskLevelLOWWhenEmpty(t *testing.T) {
	a := New(Deps{})
	out, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Metadata["riskLevel"] != "LOW" {
		t.Fatalf("expected LOW, got %v", out.Metadata["riskLevel"])
	}
}

func TestRiskLevelHIGHWhenZonesInjected(t *testing.T) {
	a := New(Deps{
		Zones: []Zone{{Type: "config_drift", Detail: "config/secrets.yaml", Severity: "critical"}},
	})
	out, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Metadata["riskLevel"] != "HIGH" {
		t.Fatalf("expected HIGH, got %v", out.Metadata["riskLevel"])
	}
	if len(out.Findings) != 1 {
		t.Fatalf("expected 1 finding from injected zone, got %d", len(out.Findings))
	}
}
