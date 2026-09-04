package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type Anthropic struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

func NewAnthropic(apiKey, model string) *Anthropic {
	return &Anthropic{
		apiKey:   apiKey,
		model:    model,
		endpoint: "https://api.anthropic.com/v1/messages",
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *Anthropic) Name() string { return "anthropic" }

type anthropicReq struct {
	Model      string              `json:"model"`
	MaxTokens  int                 `json:"max_tokens"`
	System     string              `json:"system,omitempty"`
	Messages   []Message           `json:"messages"`
	Tools      []anthropicTool     `json:"tools"`
	ToolChoice anthropicToolChoice `json:"tool_choice"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type anthropicResp struct {
	Content []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
}

func (a *Anthropic) CallWithTool(ctx context.Context, system string, msgs []Message, tool ToolSpec) (json.RawMessage, error) {
	req := anthropicReq{
		Model:     a.model,
		MaxTokens: 4000,
		System:    system,
		Messages:  msgs,
		Tools: []anthropicTool{{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}},
		ToolChoice: anthropicToolChoice{Type: "tool", Name: tool.Name},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var raw json.RawMessage
	err = Retry(ctx, 3, 500*time.Millisecond, func() error {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", a.endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		httpReq.Header.Set("x-api-key", a.apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := a.http.Do(httpReq)
		if err != nil {
			return err
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("close Anthropic response body: %v", err)
			}
		}()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(out))
		}
		var r anthropicResp
		if err := json.Unmarshal(out, &r); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		for _, c := range r.Content {
			if c.Type == "tool_use" && c.Name == tool.Name {
				raw = c.Input
				return nil
			}
		}
		return fmt.Errorf("no tool_use found in response")
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}
