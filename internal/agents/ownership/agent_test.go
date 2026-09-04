package ownership

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

func TestOutputSchemaHasNoScoreField(t *testing.T) {
	a := New(nil, 0)
	out, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"score":`) {
		t.Fatalf("score must not appear in JSON: %s", raw)
	}
	if strings.Contains(string(raw), `"kind":`) {
		t.Fatalf("kind must not appear in JSON output: %s", raw)
	}
}
