package reviewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/ai"
	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

type Agent struct {
	provider     ai.Provider
	projectsDir  string
	systemName   string
	serviceTypes []string
	maxTokens    int
}

func New(p ai.Provider, projectsDir, systemName string, serviceTypes []string) *Agent {
	return &Agent{
		provider: p, projectsDir: projectsDir, systemName: systemName,
		serviceTypes: serviceTypes, maxTokens: 8000,
	}
}

func (a *Agent) Name() interfaces.AgentName { return interfaces.AgentAIReviewer }

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	prompts, err := LoadPromptFiles(a.projectsDir, a.systemName, a.serviceTypes)
	if err != nil {
		return interfaces.AgentOutput{
			Agent:         interfaces.AgentAIReviewer,
			Status:        interfaces.StatusFailed,
			DurationMs:    int(time.Since(start) / time.Millisecond),
			SchemaVersion: "1",
			Summary:       "loader error: " + err.Error(),
		}, nil
	}
	system, _ := TruncateToBudget(prompts, a.maxTokens)
	system = ComposeSystemPrompt([]string{system}, nil)

	diffBlocks := []string{}
	for _, f := range in.Diff {
		diffBlocks = append(diffBlocks, fmt.Sprintf("--- %s ---\n%s", f.Path, f.Patch))
	}
	user := composeUserPrompt(in.CommitSHA, diffBlocks)

	raw, err := a.provider.CallWithTool(ctx, system,
		[]ai.Message{{Role: "user", Content: user}},
		ai.ToolSpec{Name: "submit_review", InputSchema: ReviewToolSchema()})
	if err != nil {
		return interfaces.AgentOutput{
			Agent:         interfaces.AgentAIReviewer,
			Status:        interfaces.StatusFailed,
			DurationMs:    int(time.Since(start) / time.Millisecond),
			SchemaVersion: "1",
			Summary:       "ai error: " + err.Error(),
		}, nil
	}
	var parsed struct {
		Summary  string `json:"summary"`
		Findings []struct {
			Severity   string               `json:"severity"`
			Category   string               `json:"category"`
			Title      string               `json:"title"`
			Body       string               `json:"body"`
			Location   *interfaces.Location `json:"location,omitempty"`
			Suggestion string               `json:"suggestion,omitempty"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return interfaces.AgentOutput{
			Agent:         interfaces.AgentAIReviewer,
			Status:        interfaces.StatusFailed,
			DurationMs:    int(time.Since(start) / time.Millisecond),
			SchemaVersion: "1",
			Summary:       "json: " + err.Error(),
		}, nil
	}
	findings := make([]interfaces.Finding, 0, len(parsed.Findings))
	for i, f := range parsed.Findings {
		var file string
		if f.Location != nil {
			file = f.Location.File
		}
		stable := makeStableID("ai_reviewer", f.Category, file, f.Title)
		findings = append(findings, interfaces.Finding{
			ID:         fmt.Sprintf("ai-%03d", i+1),
			StableID:   stable,
			Severity:   interfaces.Severity(f.Severity),
			Category:   f.Category,
			Title:      f.Title,
			Body:       f.Body,
			Location:   f.Location,
			Suggestion: f.Suggestion,
		})
	}
	return interfaces.AgentOutput{
		Agent:         interfaces.AgentAIReviewer,
		Status:        interfaces.StatusOK,
		DurationMs:    int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1",
		Findings:      findings,
		Summary:       parsed.Summary,
	}, nil
}

func makeStableID(agent, category, file, title string) string {
	h := sha256.Sum256([]byte(agent + ":" + category + ":" + file + ":" + title))
	return hex.EncodeToString(h[:])
}
