package ai

import (
	"context"
	"encoding/json"
	"testing"
)

func TestProviderInterfaceShape(t *testing.T) {
	var p Provider
	_ = p
	spec := ToolSpec{Name: "submit", InputSchema: map[string]any{"type": "object"}}
	if spec.Name == "" {
		t.Fatal("zero")
	}
	_ = json.RawMessage(nil)
	_ = context.Background()
}
