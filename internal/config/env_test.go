package config

import (
	"os"
	"strings"
	"testing"
)

func withEnv(env map[string]string, fn func()) {
	old := map[string]string{}
	for k := range env {
		old[k] = os.Getenv(k)
	}
	for k, v := range env {
		os.Setenv(k, v)
	}
	defer func() {
		for k, v := range old {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()
	fn()
}

func TestLoadConfigDefaults(t *testing.T) {
	withEnv(map[string]string{
		"AI_PROVIDER":     "anthropic",
		"AI_PROVIDER_KEY": "key",
		"GITLAB_TOKEN":    "tok",
	}, func() {
		c, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !c.Agents.SelectiveTest || !c.Agents.RolloutRisk ||
			!c.Agents.Ownership || !c.Agents.AIReviewer {
			t.Fatalf("agents should default to true: %+v", c.Agents)
		}
		if !c.RAGEnabled {
			t.Fatalf("rag should default to true")
		}
		if c.AnalyzeTimeoutSec != 180 {
			t.Fatalf("timeout default wrong: %d", c.AnalyzeTimeoutSec)
		}
		if c.AIModel != "claude-sonnet-4-6" {
			t.Fatalf("AIModel default wrong: %q", c.AIModel)
		}
	})
}

func TestLoadConfigAIModelOverride(t *testing.T) {
	withEnv(map[string]string{
		"AI_PROVIDER_KEY": "key",
		"GITLAB_TOKEN":    "tok",
		"RG_AI_MODEL":     "claude-opus-4-8",
	}, func() {
		c, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if c.AIModel != "claude-opus-4-8" {
			t.Fatalf("AIModel override wrong: %q", c.AIModel)
		}
	})
}

func TestLoadConfigMissingRequired(t *testing.T) {
	withEnv(map[string]string{}, func() {
		_, err := Load()
		if err == nil {
			t.Fatalf("expected error for missing required env")
		}
	})
}

func TestParseServiceTypes(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"backend"}},
		{"   ", []string{"backend"}},
		{"backend", []string{"backend"}},
		{"backend,frontend", []string{"backend", "frontend"}},
		{"backend, frontend ,  worker ", []string{"backend", "frontend", "worker"}},
		{",,,", []string{"backend"}},
	}
	for _, c := range cases {
		got := parseServiceTypes(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseServiceTypes(%q)=%v want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseServiceTypes(%q)=%v want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestLoadConfigPipelineVars(t *testing.T) {
	withEnv(map[string]string{
		"AI_PROVIDER_KEY":      "key",
		"GITLAB_TOKEN":         "tok",
		"RG_SERVICE_NAME":      "checkout-svc",
		"RG_SERVICE_TYPE":      "backend, worker",
		"CI_COMMIT_SHA":        "abc123",
		"CI_PROJECT_ID":        "42",
		"CI_MERGE_REQUEST_IID": "7",
	}, func() {
		c, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if c.TargetServiceName != "checkout-svc" {
			t.Errorf("TargetServiceName=%q", c.TargetServiceName)
		}
		if len(c.TargetServiceTypes) != 2 || c.TargetServiceTypes[0] != "backend" || c.TargetServiceTypes[1] != "worker" {
			t.Errorf("TargetServiceTypes=%v", c.TargetServiceTypes)
		}
		if c.CommitSHA != "abc123" {
			t.Errorf("CommitSHA=%q", c.CommitSHA)
		}
		if c.CIProjectID != 42 {
			t.Errorf("CIProjectID=%d", c.CIProjectID)
		}
		if c.CIMergeRequestIID != 7 {
			t.Errorf("CIMergeRequestIID=%d", c.CIMergeRequestIID)
		}
	})
}

func TestLoadConfigAgentTimeoutDefault(t *testing.T) {
	withEnv(map[string]string{
		"AI_PROVIDER_KEY": "key",
		"GITLAB_TOKEN":    "tok",
	}, func() {
		c, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if c.AgentTimeoutSec != 60 {
			t.Fatalf("AgentTimeoutSec default wrong: %d", c.AgentTimeoutSec)
		}
	})
}

func TestLoadConfigAgentTimeoutRejectsGEAnalyzeTimeout(t *testing.T) {
	withEnv(map[string]string{
		"AI_PROVIDER_KEY":     "key",
		"GITLAB_TOKEN":        "tok",
		"ANALYZE_TIMEOUT_SEC": "60",
		"AGENT_TIMEOUT_SEC":   "60",
	}, func() {
		_, err := Load()
		if err == nil {
			t.Fatalf("expected error when AgentTimeoutSec >= AnalyzeTimeoutSec")
		}
	})
}

func TestLoadConfigAgentTimeoutRejectsZero(t *testing.T) {
	withEnv(map[string]string{
		"AI_PROVIDER_KEY":   "key",
		"GITLAB_TOKEN":      "tok",
		"AGENT_TIMEOUT_SEC": "0",
	}, func() {
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "AGENT_TIMEOUT_SEC") {
			t.Fatalf("expected error mentioning AGENT_TIMEOUT_SEC, got: %v", err)
		}
	})
}
