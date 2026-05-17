package ai

import (
	"context"
	"encoding/json"
)

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type Message struct {
	Role    string `json:"role"`    // "user" | "system"
	Content string `json:"content"`
}

type Provider interface {
	Name() string
	CallWithTool(ctx context.Context, system string, messages []Message, tool ToolSpec) (json.RawMessage, error)
}
