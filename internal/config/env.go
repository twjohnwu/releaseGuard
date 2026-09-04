package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type AgentFlags struct {
	SelectiveTest bool
	RolloutRisk   bool
	Ownership     bool
	AIReviewer    bool
}

type Config struct {
	Agents                     AgentFlags
	RAGEnabled                 bool
	PostgresURL                string
	AIProvider                 string
	AIProviderKey              string
	AIModel                    string
	GitLabToken                string
	GitLabAPIBase              string
	ProjectsDir                string
	AnalyzeTimeoutSec          int
	AgentTimeoutSec            int
	OwnershipLookbackDays      int
	SelectiveTestMinConfidence float64
	PromptMaxTokens            int
	ReviewerSelfReflection     bool
	CoverageFormat             string
	DocsRepoNames              string
	ReportPath                 string

	// Caller-provided pipeline variables (per-MR).
	TargetServiceName  string
	TargetServiceTypes []string
	CommitSHA          string
	CIProjectID        int
	CIMergeRequestIID  int
}

// parseServiceTypes splits a comma-separated list, trims whitespace, and
// defaults to ["backend"] when empty (preserves prior analyzer behaviour).
func parseServiceTypes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{"backend"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return []string{"backend"}
	}
	return out
}

func boolEnv(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func intEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func floatEnv(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func strEnv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

// reportPathEnv resolves RG_REPORT_PATH. Unlike strEnv, an explicitly-set empty
// string disables report writing; only a completely unset var falls back to the
// default filename.
func reportPathEnv() string {
	v, ok := os.LookupEnv("RG_REPORT_PATH")
	if !ok {
		return "releaseguard-report.json"
	}
	return v
}

func agentTimeoutEnv(analyzeTimeoutSec int) int {
	v, ok := os.LookupEnv("AGENT_TIMEOUT_SEC")
	if !ok || v == "" {
		return analyzeTimeoutSec
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func Load() (*Config, error) {
	analyzeTimeoutSec := intEnv("ANALYZE_TIMEOUT_SEC", 180)
	c := &Config{
		Agents: AgentFlags{
			SelectiveTest: boolEnv("RG_AGENT_SELECTIVE_TEST_ENABLED", true),
			RolloutRisk:   boolEnv("RG_AGENT_ROLLOUT_RISK_ENABLED", true),
			Ownership:     boolEnv("RG_AGENT_OWNERSHIP_ENABLED", true),
			AIReviewer:    boolEnv("RG_AGENT_AI_REVIEWER_ENABLED", true),
		},
		RAGEnabled:                 boolEnv("RG_RAG_ENABLED", true),
		PostgresURL:                os.Getenv("POSTGRES_URL"),
		AIProvider:                 strEnv("AI_PROVIDER", "anthropic"),
		AIProviderKey:              os.Getenv("AI_PROVIDER_KEY"),
		AIModel:                    strEnv("RG_AI_MODEL", "claude-sonnet-4-6"),
		GitLabToken:                os.Getenv("GITLAB_TOKEN"),
		GitLabAPIBase:              strEnv("GITLAB_API_BASE", "https://gitlab.com/api/v4"),
		ProjectsDir:                strEnv("PROJECTS_DIR", "/app/projects"),
		AnalyzeTimeoutSec:          analyzeTimeoutSec,
		AgentTimeoutSec:            agentTimeoutEnv(analyzeTimeoutSec),
		OwnershipLookbackDays:      intEnv("OWNERSHIP_LOOKBACK_DAYS", 180),
		SelectiveTestMinConfidence: floatEnv("SELECTIVE_TEST_MIN_CONFIDENCE", 0.85),
		PromptMaxTokens:            intEnv("PROMPT_MAX_TOKENS", 8000),
		ReviewerSelfReflection:     boolEnv("RG_REVIEWER_SELF_REFLECTION", false),
		CoverageFormat:             strEnv("COVERAGE_FORMAT", "lcov"),
		DocsRepoNames:              os.Getenv("DOCS_REPO_NAMES"),
		ReportPath:                 reportPathEnv(),

		TargetServiceName:  os.Getenv("RG_SERVICE_NAME"),
		TargetServiceTypes: parseServiceTypes(os.Getenv("RG_SERVICE_TYPE")),
		CommitSHA:          os.Getenv("CI_COMMIT_SHA"),
		CIProjectID:        intEnv("CI_PROJECT_ID", 0),
		CIMergeRequestIID:  intEnv("CI_MERGE_REQUEST_IID", 0),
	}

	if c.AIProviderKey == "" {
		return nil, fmt.Errorf("AI_PROVIDER_KEY required")
	}
	if c.GitLabToken == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN required")
	}
	if c.AgentTimeoutSec <= 0 || c.AgentTimeoutSec > c.AnalyzeTimeoutSec {
		return nil, fmt.Errorf("AGENT_TIMEOUT_SEC (%d) must be > 0 and <= ANALYZE_TIMEOUT_SEC (%d)",
			c.AgentTimeoutSec, c.AnalyzeTimeoutSec)
	}
	return c, nil
}
