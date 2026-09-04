package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicCallWithToolReturnsToolInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing api key header")
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{
			"content": [{
				"type": "tool_use",
				"name": "submit_review",
				"input": {"findings": [{"title": "x"}]}
			}]
		}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-6")
	p.endpoint = srv.URL + "/v1/messages"

	got, err := p.CallWithTool(context.Background(), "sys",
		[]Message{{Role: "user", Content: "review"}},
		ToolSpec{Name: "submit_review", InputSchema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var parsed struct {
		Findings []struct{ Title string } `json:"findings"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Findings) != 1 || parsed.Findings[0].Title != "x" {
		t.Fatalf("got: %s", got)
	}
}

func TestAnthropicErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		if _, err := w.Write([]byte(`{"error":"oops"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	p := NewAnthropic("k", "m")
	p.endpoint = srv.URL + "/v1/messages"
	_, err := p.CallWithTool(context.Background(), "", []Message{}, ToolSpec{Name: "t"})
	if err == nil {
		t.Fatal("expected error")
	}
}
