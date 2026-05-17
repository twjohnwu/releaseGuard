package interfaces

import "context"

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

type AgentName string

const (
	AgentSelectiveTest AgentName = "selective_test"
	AgentRolloutRisk   AgentName = "rollout_risk"
	AgentOwnership     AgentName = "ownership"
	AgentAIReviewer    AgentName = "ai_reviewer"
)

type Status string

const (
	StatusOK      Status = "ok"
	StatusPartial Status = "partial"
	StatusFailed  Status = "failed"
)

type Location struct {
	File      string `json:"file"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
}

type Finding struct {
	ID         string    `json:"id"`
	StableID   string    `json:"stable_id"`
	Severity   Severity  `json:"severity"`
	Category   string    `json:"category"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Location   *Location `json:"location,omitempty"`
	Suggestion string    `json:"suggestion,omitempty"`
	References []string  `json:"references,omitempty"`
}

type AgentOutput struct {
	Agent         AgentName              `json:"agent"`
	Status        Status                 `json:"status"`
	DurationMs    int                    `json:"duration_ms"`
	SchemaVersion string                 `json:"schema_version"`
	Findings      []Finding              `json:"findings"`
	Summary       string                 `json:"summary,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type AgentInput struct {
	RepoID    int64
	RepoName  string
	MRIID     int
	CommitSHA string
	Diff      []DiffFile
	Config    AgentConfig
}

type DiffFile struct {
	Path    string
	OldPath string
	Status  string // added | modified | deleted | renamed
	Patch   string
}

type AgentConfig struct {
	Topology   string            // "0" or "1"
	ProjectDir string
	Extra      map[string]string // agent-specific
}

type IAgent interface {
	Name() AgentName
	Run(ctx context.Context, in AgentInput) (AgentOutput, error)
}
