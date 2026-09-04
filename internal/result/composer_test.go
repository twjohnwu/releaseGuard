package result

import (
	"strings"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

func TestComposerFiltersByFlags(t *testing.T) {
	outs := []interfaces.AgentOutput{
		{Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusOK,
			Findings: []interfaces.Finding{{Severity: interfaces.SeverityHigh, Title: "x"}}},
		{Agent: interfaces.AgentSelectiveTest, Status: interfaces.StatusOK,
			Metadata: map[string]any{"required": []string{"T"}, "analysis_level": "L1", "confidence": 0.5}},
	}
	flags := Flags{AIReviewer: true, SelectiveTest: false}
	md := Compose(outs, flags)
	if !strings.Contains(md, "REVIEW") {
		t.Fatalf("recommendation lost: %s", md)
	}
	if strings.Contains(md, "Selective test plan") {
		t.Fatalf("selective should be excluded by flag: %s", md)
	}
}
