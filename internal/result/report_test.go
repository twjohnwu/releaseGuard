package result

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
	"github.com/twjohnwu/releaseGuard/internal/report"
)

func TestWriteReportWritesExpectedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "releaseguard-report.json")

	rec := report.Recommendation{
		Recommendation: "HOLD",
		Rationale:      "HOLD — [ai_reviewer/critical_finding] boom",
		TriggeredSignals: []report.Signal{
			{Agent: interfaces.AgentAIReviewer, Kind: "critical_finding", Detail: "boom"},
		},
	}
	outs := []interfaces.AgentOutput{
		{Agent: interfaces.AgentSelectiveTest, Status: interfaces.StatusOK, DurationMs: 12,
			SchemaVersion: "1", Findings: []interfaces.Finding{}},
	}

	if err := WriteReport(path, rec, outs); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var got Report
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SchemaVersion != "1" {
		t.Errorf("schema_version=%q", got.SchemaVersion)
	}
	if got.Decision != "HOLD" {
		t.Errorf("decision=%q", got.Decision)
	}
	if len(got.TriggeredSignals) != 1 || got.TriggeredSignals[0].Kind != "critical_finding" {
		t.Errorf("triggered_signals=%+v", got.TriggeredSignals)
	}
	if len(got.Agents) != 1 || got.Agents[0].Agent != interfaces.AgentSelectiveTest {
		t.Errorf("agents=%+v", got.Agents)
	}
}

func TestWriteReportSetsReadablePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "releaseguard-report.json")

	if err := WriteReport(path, report.Recommendation{}, nil); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat report: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode=%#o", info.Mode().Perm())
	}
}

func TestWriteReportEmptyPathDisables(t *testing.T) {
	if err := WriteReport("", report.Recommendation{}, nil); err != nil {
		t.Fatalf("empty path should be a no-op, got: %v", err)
	}
}
