package ownership

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestOutputSchemaHasNoScoreField(t *testing.T) {
	a := New(nil, 0)
	out, _ := a.Run(context.Background(), interfaces.AgentInput{})
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), `"score":`) {
		t.Fatalf("score must not appear in JSON: %s", raw)
	}
	if strings.Contains(string(raw), `"kind":`) {
		t.Fatalf("kind must not appear in JSON output: %s", raw)
	}
}
