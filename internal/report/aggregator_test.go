package report

import (
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestFlattenSortsAndDedupes(t *testing.T) {
	outs := []interfaces.AgentOutput{
		{Findings: []interfaces.Finding{
			{StableID: "x", Severity: interfaces.SeverityLow, Title: "low one"},
			{StableID: "y", Severity: interfaces.SeverityCritical, Title: "crit"},
		}},
		{Findings: []interfaces.Finding{
			{StableID: "x", Severity: interfaces.SeverityLow, Title: "low one"}, // dup
			{StableID: "z", Severity: interfaces.SeverityHigh, Title: "h"},
		}},
	}
	flat := FlattenFindings(outs)
	if len(flat) != 3 {
		t.Fatalf("expected 3 unique, got %d", len(flat))
	}
	if flat[0].Severity != interfaces.SeverityCritical {
		t.Fatalf("expected critical first, got %s", flat[0].Severity)
	}
}
