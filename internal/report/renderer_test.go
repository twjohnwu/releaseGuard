package report

import (
	"strings"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

func TestRendererPutsRecommendationFirst(t *testing.T) {
	r := ImpactScopeReport{
		Recommendation: Recommendation{Recommendation: "HOLD", Rationale: "x",
			TriggeredSignals: []Signal{{Agent: interfaces.AgentAIReviewer, Kind: "critical_finding", Detail: "f"}}},
	}
	md := Render(r)
	if !strings.HasPrefix(md, "## 🔴 ReleaseGuard recommendation: HOLD") {
		t.Fatalf("recommendation not first: %s", md[:80])
	}
}

func TestRendererProceedEmoji(t *testing.T) {
	r := ImpactScopeReport{Recommendation: Recommendation{Recommendation: "PROCEED"}}
	md := Render(r)
	if !strings.Contains(md, "✅") {
		t.Fatalf("missing PROCEED emoji: %s", md)
	}
}

func TestRendererZoneListDetail(t *testing.T) {
	r := ImpactScopeReport{
		Recommendation: Recommendation{Recommendation: "REVIEW"},
		RiskLevel:      "MED",
		RiskZones: []ZoneInfo{
			{Type: "config_drift", Detail: "config/x.yaml", Severity: "medium"},
		},
		SelectiveTests: &TestPlan{
			AnalysisLevel: "L1",
			Confidence:    0.4,
			Reason:        "L1 file-path mapping",
			Required:      []string{"a/...", "b/..."},
		},
	}
	md := Render(r)
	if !strings.Contains(md, "config/x.yaml") {
		t.Errorf("missing zone detail: %s", md)
	}
	if !strings.Contains(md, "L1 file-path mapping") {
		t.Errorf("missing reason: %s", md)
	}
	if !strings.Contains(md, "`a/...`") {
		t.Errorf("missing test list: %s", md)
	}
}

func TestRendererCollapsesLongRequiredList(t *testing.T) {
	required := []string{}
	for i := 0; i < 12; i++ {
		required = append(required, "TestX"+string(rune('A'+i)))
	}
	r := ImpactScopeReport{
		SelectiveTests: &TestPlan{AnalysisLevel: "L3", Confidence: 0.9, Required: required},
	}
	md := Render(r)
	// First 5 should be visible (outside <details>)
	preview, after, ok := strings.Cut(md, "<details>")
	if !ok {
		t.Fatalf("expected <details> tag for 12-item required list:\n%s", md)
	}
	for _, name := range required[:5] {
		if !strings.Contains(preview, "`"+name+"`") {
			t.Errorf("preview missing %s:\n%s", name, preview)
		}
	}
	// Remaining 7 should sit inside the collapsed block
	for _, name := range required[5:] {
		if !strings.Contains(after, "`"+name+"`") {
			t.Errorf("collapsed block missing %s:\n%s", name, after)
		}
	}
	// Summary text shows the remaining count
	if !strings.Contains(md, "<summary>… 7 more</summary>") {
		t.Errorf("missing collapsed summary text:\n%s", md)
	}
	if !strings.Contains(md, "</details>") {
		t.Errorf("missing closing </details>:\n%s", md)
	}
}

func TestRendererSkipsCollapseWhenShortList(t *testing.T) {
	r := ImpactScopeReport{
		SelectiveTests: &TestPlan{
			AnalysisLevel: "L1",
			Confidence:    0.4,
			Required:      []string{"a", "b", "c"}, // <= 5
		},
	}
	md := Render(r)
	if strings.Contains(md, "<details>") {
		t.Errorf("did not expect <details> for 3-item list:\n%s", md)
	}
}
