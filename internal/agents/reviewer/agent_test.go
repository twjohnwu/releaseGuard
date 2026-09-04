package reviewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/ai"
	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

type stubProvider struct{ raw json.RawMessage }

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) CallWithTool(ctx context.Context, sys string, msgs []ai.Message, t ai.ToolSpec) (json.RawMessage, error) {
	return s.raw, nil
}

func TestReviewerStableID(t *testing.T) {
	resp := `{"findings":[{"severity":"high","category":"logic_bug","title":"x","body":"y"}]}`
	p := &stubProvider{raw: json.RawMessage(resp)}
	a := New(p, "/nonexistent", "system", []string{"backend"})
	out, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(out.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(out.Findings))
	}
	expectedHash := sha256.Sum256([]byte("ai_reviewer:logic_bug::x"))
	want := hex.EncodeToString(expectedHash[:])
	if out.Findings[0].StableID != want {
		t.Fatalf("stable id = %s, want %s", out.Findings[0].StableID, want)
	}
}
