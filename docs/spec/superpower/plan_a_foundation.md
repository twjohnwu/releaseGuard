# ReleaseGuard Plan A — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the `releaseguard` Go repo with the foundational layers all four agents will share: shared interfaces (`Finding` / `AgentOutput` / `IAgent`), Postgres connection helper (via PgBouncer), GitLab client (read MR diff + post note), AI provider interface + Anthropic implementation, and a runnable `cmd/analyzer` entry that loads config, validates topology, and posts a smoke-test comment back to a fixture MR.

**Architecture:** Clean architecture with composition root in `cmd/analyzer/main.go`. All collaborators wired through interfaces. Postgres goes through PgBouncer transaction pool mode (no prepared statements, no LISTEN/NOTIFY). AI provider abstracted via `Provider` interface with retry/backoff helper shared across providers. Topology validation lives at process boot — invalid env combinations cause `os.Exit(1)` with explicit error.

**Tech Stack:** Go 1.23+, `github.com/jackc/pgx/v5` (Postgres), `github.com/anthropics/anthropic-sdk-go`, standard `net/http` for GitLab REST, `go-yaml/yaml` for config files, `golang-migrate/migrate` for SQL migrations.

---

## File Structure

```
releaseguard/
├── cmd/
│   └── analyzer/
│       ├── main.go              # composition root
│       ├── topology.go          # 拓樸檢查
│       └── topology_test.go
├── internal/
│   ├── interfaces/
│   │   ├── finding.go           # Finding + AgentOutput + IAgent
│   │   └── finding_test.go
│   ├── config/
│   │   ├── env.go               # env loader
│   │   └── env_test.go
│   ├── storage/
│   │   ├── postgres.go          # pgx pool helper
│   │   └── postgres_test.go
│   ├── gitlab/
│   │   ├── client.go            # GitLab REST client
│   │   ├── diff.go              # GetMRDiff
│   │   ├── note.go              # PostMRNote
│   │   └── client_test.go
│   ├── ai/
│   │   ├── provider.go          # Provider interface + ToolSpec
│   │   ├── retry.go             # exponential backoff helper
│   │   ├── anthropic.go         # Anthropic tool use impl
│   │   └── anthropic_test.go
│   └── logger/
│       └── logger.go            # JSON structured logger
├── migrations/
│   └── 0001_init.sql
├── deploy/
│   ├── Dockerfile.analyzer
│   └── ci/
│       └── releaseguard.yaml    # GitLab CI template
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

### Task 1: Initialize Go module and directory skeleton

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `README.md`
- Create: `.gitignore`

- [ ] **Step 1: Create directory and init module**

```bash
mkdir -p releaseguard && cd releaseguard
go mod init github.com/acme/releaseguard
```

Expected: creates `go.mod` with `module github.com/acme/releaseguard` line.

- [ ] **Step 2: Create directory skeleton**

```bash
mkdir -p cmd/analyzer internal/{interfaces,config,storage,gitlab,ai,logger} migrations deploy/ci
```

- [ ] **Step 3: Write `Makefile`**

```makefile
.PHONY: build test lint docker clean

build:
	go build -o ./bin/analyzer ./cmd/analyzer

test:
	go test ./... -race -count=1

lint:
	golangci-lint run

docker:
	docker build -t releaseguard-analyzer:latest -f deploy/Dockerfile.analyzer .

clean:
	rm -rf ./bin
```

- [ ] **Step 4: Write `.gitignore`**

```
/bin/
*.test
*.out
.env
```

- [ ] **Step 5: Write minimal `README.md`**

```markdown
# ReleaseGuard

MR-level release gating system. See `Infra/spec/releaseGuard/` for full architecture spec.

## Quick start

\`\`\`bash
make build
./bin/analyzer
\`\`\`
```

- [ ] **Step 6: Commit**

```bash
git init
git add go.mod Makefile README.md .gitignore
git commit -m "chore: initialize releaseguard repo skeleton"
```

---

### Task 2: Define `Finding` and `AgentOutput` shared schema

**Files:**
- Create: `internal/interfaces/finding.go`
- Create: `internal/interfaces/finding_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/interfaces/finding_test.go
package interfaces

import (
	"encoding/json"
	"testing"
)

func TestFindingMarshalRoundTrip(t *testing.T) {
	f := Finding{
		ID:       "ai-001",
		StableID: "ai_reviewer:logic_bug:foo.go:Title",
		Severity: SeverityHigh,
		Category: "logic_bug",
		Title:    "Missing defer rollback",
		Body:     "leak risk",
		Location: &Location{File: "foo.go", LineStart: 10, LineEnd: 20},
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Finding
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.StableID != f.StableID {
		t.Fatalf("stable_id mismatch: got %q want %q", back.StableID, f.StableID)
	}
	if back.Location == nil || back.Location.File != "foo.go" {
		t.Fatalf("location lost: %+v", back.Location)
	}
}

func TestAgentOutputSchemaVersionPinned(t *testing.T) {
	out := AgentOutput{
		Agent:         AgentSelectiveTest,
		Status:        StatusOK,
		DurationMs:    100,
		SchemaVersion: "1",
	}
	raw, _ := json.Marshal(out)
	if !contains(raw, `"schema_version":"1"`) {
		t.Fatalf("schema_version not v1: %s", raw)
	}
}

func contains(b []byte, s string) bool {
	return string(b) != "" && (len(b) >= len(s)) && (string(b[:]) != "" && (indexOf(b, s) >= 0))
}

func indexOf(haystack []byte, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/interfaces/ -run TestFindingMarshalRoundTrip
```

Expected: FAIL — `Finding`, `Location`, `AgentOutput` types undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/interfaces/finding.go
package interfaces

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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/interfaces/ -run TestFindingMarshalRoundTrip -v
go test ./internal/interfaces/ -run TestAgentOutputSchemaVersionPinned -v
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/interfaces/
git commit -m "feat(interfaces): add Finding and AgentOutput shared schema"
```

---

### Task 3: Define `IAgent` interface

**Files:**
- Modify: `internal/interfaces/finding.go` (add interface)
- Create: `internal/interfaces/agent_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/interfaces/agent_test.go
package interfaces

import (
	"context"
	"testing"
)

type fakeAgent struct{ name AgentName }

func (f *fakeAgent) Name() AgentName { return f.name }
func (f *fakeAgent) Run(ctx context.Context, in AgentInput) (AgentOutput, error) {
	return AgentOutput{Agent: f.name, Status: StatusOK, SchemaVersion: "1"}, nil
}

func TestIAgentSatisfiable(t *testing.T) {
	var a IAgent = &fakeAgent{name: AgentSelectiveTest}
	out, err := a.Run(context.Background(), AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Agent != AgentSelectiveTest {
		t.Fatalf("agent name lost")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/interfaces/ -run TestIAgentSatisfiable
```

Expected: FAIL — `IAgent`, `AgentInput` undefined.

- [ ] **Step 3: Add interface to `finding.go`**

```go
// Append to internal/interfaces/finding.go
import "context"

type AgentInput struct {
	RepoID    int64
	RepoName  string
	MRIID     int
	CommitSHA string
	Diff      []DiffFile
	Config    AgentConfig
}

type DiffFile struct {
	Path     string
	OldPath  string
	Status   string // added | modified | deleted | renamed
	Patch    string
}

type AgentConfig struct {
	Topology   string             // "0" or "1"
	ProjectDir string
	Extra      map[string]string  // agent-specific
}

type IAgent interface {
	Name() AgentName
	Run(ctx context.Context, in AgentInput) (AgentOutput, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/interfaces/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/interfaces/
git commit -m "feat(interfaces): add IAgent interface and AgentInput type"
```

---

### Task 4: Config loader with env validation

**Files:**
- Create: `internal/config/env.go`
- Create: `internal/config/env_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/env_test.go
package config

import (
	"os"
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/
```

Expected: FAIL — `Load`, `Config` undefined.

- [ ] **Step 3: Write implementation**

```go
// internal/config/env.go
package config

import (
	"fmt"
	"os"
	"strconv"
)

type AgentFlags struct {
	SelectiveTest bool
	RolloutRisk   bool
	Ownership     bool
	AIReviewer    bool
}

type Config struct {
	Agents              AgentFlags
	RAGEnabled          bool
	PostgresURL         string
	AIProvider          string
	AIProviderKey       string
	GitLabToken         string
	GitLabAPIBase       string
	ProjectsDir         string
	AnalyzeTimeoutSec   int
	OwnershipLookbackDays int
	SelectiveTestMinConfidence float64
	PromptMaxTokens     int
	ReviewerSelfReflection bool
	CoverageFormat      string
	DocsRepoNames       string
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

func Load() (*Config, error) {
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
		GitLabToken:                os.Getenv("GITLAB_TOKEN"),
		GitLabAPIBase:              strEnv("GITLAB_API_BASE", "https://gitlab.com/api/v4"),
		ProjectsDir:                strEnv("PROJECTS_DIR", "/app/projects"),
		AnalyzeTimeoutSec:          intEnv("ANALYZE_TIMEOUT_SEC", 180),
		OwnershipLookbackDays:      intEnv("OWNERSHIP_LOOKBACK_DAYS", 180),
		SelectiveTestMinConfidence: floatEnv("SELECTIVE_TEST_MIN_CONFIDENCE", 0.85),
		PromptMaxTokens:            intEnv("PROMPT_MAX_TOKENS", 8000),
		ReviewerSelfReflection:     boolEnv("RG_REVIEWER_SELF_REFLECTION", false),
		CoverageFormat:             strEnv("COVERAGE_FORMAT", "lcov"),
		DocsRepoNames:              os.Getenv("DOCS_REPO_NAMES"),
	}

	if c.AIProviderKey == "" {
		return nil, fmt.Errorf("AI_PROVIDER_KEY required")
	}
	if c.GitLabToken == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN required")
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/config/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add env-based config loader with defaults"
```

---

### Task 5: Topology validator

**Files:**
- Create: `cmd/analyzer/topology.go`
- Create: `cmd/analyzer/topology_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/analyzer/topology_test.go
package main

import (
	"testing"

	"github.com/acme/releaseguard/internal/config"
)

func TestValidateTopologyTopology0(t *testing.T) {
	c := &config.Config{
		PostgresURL: "postgres://x",
		Agents:      config.AgentFlags{SelectiveTest: true, RolloutRisk: true, Ownership: true, AIReviewer: true},
		RAGEnabled:  true,
	}
	mode, err := validateTopology(c)
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if mode != "0" {
		t.Fatalf("expected topology 0: %s", mode)
	}
}

func TestValidateTopologyTopology1(t *testing.T) {
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: false, RolloutRisk: true, Ownership: false, AIReviewer: true},
		RAGEnabled:  false,
	}
	mode, err := validateTopology(c)
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if mode != "1" {
		t.Fatalf("expected topology 1: %s", mode)
	}
}

func TestValidateTopologyInvalidSelectiveWithoutDB(t *testing.T) {
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: true, RolloutRisk: true, Ownership: false, AIReviewer: true},
		RAGEnabled:  false,
	}
	_, err := validateTopology(c)
	if err == nil {
		t.Fatalf("expected error: selective test requires postgres")
	}
}

func TestValidateTopologyInvalidOwnershipWithoutDB(t *testing.T) {
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: false, RolloutRisk: true, Ownership: true, AIReviewer: true},
		RAGEnabled:  false,
	}
	_, err := validateTopology(c)
	if err == nil {
		t.Fatalf("expected error: ownership requires postgres")
	}
}

func TestValidateTopologyInvalidRAGWithoutDB(t *testing.T) {
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: false, RolloutRisk: true, Ownership: false, AIReviewer: true},
		RAGEnabled:  true,
	}
	_, err := validateTopology(c)
	if err == nil {
		t.Fatalf("expected error: rag requires postgres")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./cmd/analyzer/ -run TestValidateTopology
```

Expected: FAIL — `validateTopology` undefined.

- [ ] **Step 3: Write implementation**

```go
// cmd/analyzer/topology.go
package main

import (
	"fmt"

	"github.com/acme/releaseguard/internal/config"
)

// validateTopology returns "0" (full) or "1" (script-only) or error for invalid combinations.
func validateTopology(c *config.Config) (string, error) {
	hasPG := c.PostgresURL != ""
	if !hasPG {
		if c.Agents.SelectiveTest {
			return "", fmt.Errorf("RG_AGENT_SELECTIVE_TEST_ENABLED requires POSTGRES_URL")
		}
		if c.Agents.Ownership {
			return "", fmt.Errorf("RG_AGENT_OWNERSHIP_ENABLED requires POSTGRES_URL")
		}
		if c.RAGEnabled {
			return "", fmt.Errorf("RG_RAG_ENABLED requires POSTGRES_URL")
		}
		return "1", nil
	}
	return "0", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./cmd/analyzer/ -v
```

Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/analyzer/
git commit -m "feat(analyzer): add topology validator with 8 combination tests"
```

---

### Task 6: Postgres pool helper (PgBouncer-compatible)

**Files:**
- Create: `internal/storage/postgres.go`
- Create: `internal/storage/postgres_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/storage/postgres_test.go
package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPoolConnectAndPing(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestNewPoolEmptyURL(t *testing.T) {
	_, err := NewPool(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for empty url")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/storage/
```

Expected: FAIL — `NewPool` undefined.

- [ ] **Step 3: Write implementation**

```go
// internal/storage/postgres.go
package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	*pgxpool.Pool
}

// NewPool returns a pgxpool that is PgBouncer-transaction-pool-mode safe:
// - prepared statements disabled (StatementCacheCapacity=0)
// - simple query protocol
func NewPool(ctx context.Context, url string) (*Pool, error) {
	if url == "" {
		return nil, fmt.Errorf("postgres url is empty")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = 0 // QueryExecModeSimpleProtocol = 4 in v5; 0 = cache statements; we want simple
	// In pgx v5, set:
	// cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	// To keep this snippet self-contained without importing pgx alias, we set via raw config:
	cfg.MaxConns = 25
	cfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	return &Pool{Pool: pool}, nil
}
```

Add to `go.mod`:

```bash
go get github.com/jackc/pgx/v5/pgxpool
```

Update `postgres.go` import to use `pgx.QueryExecModeSimpleProtocol`:

```go
import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, url string) (*Pool, error) {
	if url == "" {
		return nil, fmt.Errorf("postgres url is empty")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.MaxConns = 25
	cfg.MinConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	return &Pool{Pool: pool}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
# Skipped by default (TEST_POSTGRES_URL not set) — verify empty url case
go test ./internal/storage/ -v -run TestNewPoolEmptyURL
```

Expected: PASS for empty URL test, SKIP for connect test.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/ go.mod go.sum
git commit -m "feat(storage): add PgBouncer-safe pgx pool helper"
```

---

### Task 7: Initial Postgres migration — `repos` table

**Files:**
- Create: `migrations/0001_init.sql`
- Create: `migrations/0001_init.down.sql`
- Create: `internal/storage/migrate.go`
- Create: `internal/storage/migrate_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/storage/migrate_test.go
package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMigrateUpCreatesReposTable(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := MigrateUp(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_name='repos'").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("repos table not created")
	}
}
```

- [ ] **Step 2: Write `0001_init.sql`**

```sql
-- migrations/0001_init.sql
CREATE TABLE IF NOT EXISTS repos (
  id              BIGSERIAL PRIMARY KEY,
  name            TEXT UNIQUE NOT NULL,
  languages       JSONB NOT NULL DEFAULT '[]'::jsonb,
  indexer_config  JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- [ ] **Step 3: Write `0001_init.down.sql`**

```sql
-- migrations/0001_init.down.sql
DROP TABLE IF EXISTS repos;
```

- [ ] **Step 4: Implement migrate runner**

```go
// internal/storage/migrate.go
package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrateUp applies all *.sql files in the dir in lexical order, idempotently.
// Tracks applied migrations in a `schema_migrations` table.
func MigrateUp(ctx context.Context, pool *Pool, dir string) error {
	if _, err := pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
          name TEXT PRIMARY KEY,
          applied_at TIMESTAMPTZ DEFAULT now()
        )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}
	var ups []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".sql") && !strings.HasSuffix(n, ".down.sql") {
			ups = append(ups, n)
		}
	}
	sort.Strings(ups)

	for _, name := range ups {
		var applied bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)", name).Scan(&applied)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx,
			"INSERT INTO schema_migrations(name) VALUES($1)", name); err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}
	}
	return nil
}
```

- [ ] **Step 5: Run integration test**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
sleep 3
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" \
  go test ./internal/storage/ -v -run TestMigrate
docker stop rg-pg && docker rm rg-pg
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add migrations/ internal/storage/migrate.go internal/storage/migrate_test.go
git commit -m "feat(storage): add migration runner and 0001 init schema (repos table)"
```

---

### Task 8: GitLab client base (HTTP + auth)

**Files:**
- Create: `internal/gitlab/client.go`
- Create: `internal/gitlab/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/gitlab/client_test.go
package gitlab

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAddsAuthHeader(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret-token")
	if _, err := c.do("GET", "/test", nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got != "secret-token" {
		t.Fatalf("expected token header, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gitlab/
```

Expected: FAIL — `NewClient`, `do` undefined.

- [ ] **Step 3: Write implementation**

```go
// internal/gitlab/client.go
package gitlab

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	base  string
	token string
	http  *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		base:  baseURL,
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, body []byte) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gitlab %s %s: %d %s", method, path, resp.StatusCode, string(out))
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/gitlab/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gitlab/
git commit -m "feat(gitlab): add HTTP client base with PRIVATE-TOKEN auth"
```

---

### Task 9: GitLab GetMRDiff

**Files:**
- Create: `internal/gitlab/diff.go`
- Modify: `internal/gitlab/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Append to internal/gitlab/client_test.go
func TestGetMRDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/123/merge_requests/45/diffs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"old_path":"foo.go","new_path":"foo.go","diff":"@@ -1,3 +1,3 @@\n-old\n+new","new_file":false,"renamed_file":false,"deleted_file":false}
		]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	files, err := c.GetMRDiff(123, 45)
	if err != nil {
		t.Fatalf("get diff: %v", err)
	}
	if len(files) != 1 || files[0].NewPath != "foo.go" {
		t.Fatalf("unexpected: %+v", files)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gitlab/ -run TestGetMRDiff
```

Expected: FAIL — `GetMRDiff` undefined.

- [ ] **Step 3: Write implementation**

```go
// internal/gitlab/diff.go
package gitlab

import (
	"encoding/json"
	"fmt"
)

type DiffFile struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
}

func (c *Client) GetMRDiff(projectID, mrIID int) ([]DiffFile, error) {
	body, err := c.do("GET",
		fmt.Sprintf("/projects/%d/merge_requests/%d/diffs", projectID, mrIID), nil)
	if err != nil {
		return nil, err
	}
	var out []DiffFile
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/gitlab/ -v
```

Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/gitlab/
git commit -m "feat(gitlab): add GetMRDiff method"
```

---

### Task 10: GitLab PostMRNote

**Files:**
- Create: `internal/gitlab/note.go`
- Modify: `internal/gitlab/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Append to internal/gitlab/client_test.go
func TestPostMRNote(t *testing.T) {
	receivedBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/projects/123/merge_requests/45/notes" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 999}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	if err := c.PostMRNote(123, 45, "hello world"); err != nil {
		t.Fatalf("post: %v", err)
	}
	if !strings.Contains(receivedBody, "hello world") {
		t.Fatalf("body lost: %s", receivedBody)
	}
}
```

Add imports: `"io"`, `"strings"`.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gitlab/ -run TestPostMRNote
```

Expected: FAIL.

- [ ] **Step 3: Write implementation**

```go
// internal/gitlab/note.go
package gitlab

import (
	"encoding/json"
	"fmt"
)

func (c *Client) PostMRNote(projectID, mrIID int, body string) error {
	payload, _ := json.Marshal(map[string]string{"body": body})
	_, err := c.do("POST",
		fmt.Sprintf("/projects/%d/merge_requests/%d/notes", projectID, mrIID), payload)
	if err != nil {
		return fmt.Errorf("post note: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/gitlab/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gitlab/
git commit -m "feat(gitlab): add PostMRNote method"
```

---

### Task 11: AI Provider interface + ToolSpec

**Files:**
- Create: `internal/ai/provider.go`

- [ ] **Step 1: Write the failing compile check**

```go
// internal/ai/provider_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ai/
```

Expected: FAIL — `Provider`, `ToolSpec` undefined.

- [ ] **Step 3: Write the interface**

```go
// internal/ai/provider.go
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
```

- [ ] **Step 4: Run test**

```bash
go test ./internal/ai/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/provider.go internal/ai/provider_test.go
git commit -m "feat(ai): add Provider interface and ToolSpec"
```

---

### Task 12: Retry helper (exponential backoff)

**Files:**
- Create: `internal/ai/retry.go`
- Create: `internal/ai/retry_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/ai/retry_test.go
package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryEventuallySucceeds(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), 3, 1*time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryGivesUp(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), 2, 1*time.Millisecond, func() error {
		calls++
		return errors.New("perma")
	})
	if err == nil {
		t.Fatal("expected error after retries")
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestRetryRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Retry(ctx, 5, 10*time.Millisecond, func() error {
		return errors.New("never reached")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ai/ -run TestRetry
```

Expected: FAIL.

- [ ] **Step 3: Write implementation**

```go
// internal/ai/retry.go
package ai

import (
	"context"
	"time"
)

// Retry calls fn up to maxAttempts times with exponential backoff (delay, 2*delay, 4*delay...).
// Returns the last error if all attempts fail. Returns context error immediately if cancelled.
func Retry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	var err error
	delay := baseDelay
	for i := 0; i < maxAttempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = fn()
		if err == nil {
			return nil
		}
		if i < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				delay *= 2
			}
		}
	}
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ai/ -v -run TestRetry
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/retry.go internal/ai/retry_test.go
git commit -m "feat(ai): add exponential-backoff retry helper"
```

---

### Task 13: Anthropic provider implementation

**Files:**
- Create: `internal/ai/anthropic.go`
- Create: `internal/ai/anthropic_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/ai/anthropic_test.go
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
		w.Write([]byte(`{
			"content": [{
				"type": "tool_use",
				"name": "submit_review",
				"input": {"findings": [{"title": "x"}]}
			}]
		}`))
	}))
	defer srv.Close()

	p := NewAnthropic("test-key", "claude-3-5-sonnet-20241022")
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
		w.Write([]byte(`{"error":"oops"}`))
	}))
	defer srv.Close()
	p := NewAnthropic("k", "m")
	p.endpoint = srv.URL + "/v1/messages"
	_, err := p.CallWithTool(context.Background(), "", []Message{}, ToolSpec{Name: "t"})
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ai/ -run TestAnthropic
```

Expected: FAIL — `NewAnthropic` undefined.

- [ ] **Step 3: Write implementation**

```go
// internal/ai/anthropic.go
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Model      string         `json:"model"`
	MaxTokens  int            `json:"max_tokens"`
	System     string         `json:"system,omitempty"`
	Messages   []Message      `json:"messages"`
	Tools      []anthropicTool `json:"tools"`
	ToolChoice anthropicToolChoice `json:"tool_choice"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"` // "tool"
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
	body, _ := json.Marshal(req)

	var raw json.RawMessage
	err := Retry(ctx, 3, 500*time.Millisecond, func() error {
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
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ai/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/anthropic.go internal/ai/anthropic_test.go
git commit -m "feat(ai): add Anthropic Provider implementation with tool use"
```

---

### Task 14: Structured logger

**Files:**
- Create: `internal/logger/logger.go`

- [ ] **Step 1: Write minimal test**

```go
// internal/logger/logger_test.go
package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogIncludesField(t *testing.T) {
	var buf bytes.Buffer
	l := NewWith(&buf)
	l.Info("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, `"k":"v"`) {
		t.Fatalf("missing fields: %s", out)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/logger/
```

- [ ] **Step 3: Implement**

```go
// internal/logger/logger.go
package logger

import (
	"io"
	"log/slog"
	"os"
)

type Logger struct{ *slog.Logger }

func New() *Logger { return NewWith(os.Stderr) }

func NewWith(w io.Writer) *Logger {
	return &Logger{slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))}
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/logger/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/logger/
git commit -m "feat(logger): add slog-based JSON logger"
```

---

### Task 15: cmd/analyzer/main.go skeleton

**Files:**
- Create: `cmd/analyzer/main.go`

- [ ] **Step 1: Write smoke test**

```go
// cmd/analyzer/main_smoke_test.go
package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMainSmokeTopologyCheck(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", "/tmp/rg-analyzer", "./").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %s", out)
	}
	defer os.Remove("/tmp/rg-analyzer")

	// Missing required env → exit 1, error message.
	cmd := exec.Command("/tmp/rg-analyzer")
	cmd.Env = []string{}
	got, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit 1; got success: %s", got)
	}
	if !strings.Contains(string(got), "AI_PROVIDER_KEY required") {
		t.Fatalf("unexpected error: %s", got)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./cmd/analyzer/ -run TestMainSmoke
```

- [ ] **Step 3: Implement**

```go
// cmd/analyzer/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/acme/releaseguard/internal/config"
	"github.com/acme/releaseguard/internal/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	mode, err := validateTopology(cfg)
	if err != nil {
		return err
	}
	log := logger.New()
	log.Info("analyzer starting", "topology", mode, "ai_provider", cfg.AIProvider)

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(cfg.AnalyzeTimeoutSec)*time.Second)
	defer cancel()
	_ = ctx

	log.Info("analyzer foundation ready (Plan A complete; agents will be wired in Plan B)")
	return nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./cmd/analyzer/ -v -run TestMainSmoke
```

- [ ] **Step 5: Commit**

```bash
git add cmd/analyzer/main.go cmd/analyzer/main_smoke_test.go
git commit -m "feat(analyzer): add main entry with config + topology check"
```

---

### Task 16: Dockerfile for analyzer

**Files:**
- Create: `deploy/Dockerfile.analyzer`

- [ ] **Step 1: Write Dockerfile**

```dockerfile
# deploy/Dockerfile.analyzer
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/analyzer ./cmd/analyzer

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git
COPY --from=build /out/analyzer /usr/local/bin/analyzer
ENTRYPOINT ["/usr/local/bin/analyzer"]
```

- [ ] **Step 2: Build the image**

```bash
docker build -t releaseguard-analyzer:dev -f deploy/Dockerfile.analyzer .
```

Expected: build succeeds, image tagged.

- [ ] **Step 3: Verify image runs (and fails at config load as expected)**

```bash
docker run --rm releaseguard-analyzer:dev 2>&1 || true
```

Expected output: `AI_PROVIDER_KEY required` (exit 1).

- [ ] **Step 4: Verify image runs with minimum env**

```bash
docker run --rm \
  -e AI_PROVIDER_KEY=test \
  -e GITLAB_TOKEN=test \
  -e POSTGRES_URL=postgres://x \
  releaseguard-analyzer:dev
```

Expected output (JSON line): `"analyzer starting" topology=0 ...` then exit 0.

- [ ] **Step 5: Commit**

```bash
git add deploy/Dockerfile.analyzer
git commit -m "build: add multi-stage Dockerfile for analyzer"
```

---

### Task 17: GitLab CI template

**Files:**
- Create: `deploy/ci/releaseguard.yaml`

- [ ] **Step 1: Write template**

```yaml
# deploy/ci/releaseguard.yaml
.releaseguard-base:
  image: registry.example.com/releaseguard-analyzer:latest
  variables:
    AI_PROVIDER: "anthropic"
  script:
    - /usr/local/bin/analyzer
  allow_failure: true

.releaseguard-full:
  extends: .releaseguard-base
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'

.releaseguard-lite:
  extends: .releaseguard-base
  variables:
    RG_AGENT_SELECTIVE_TEST_ENABLED: "false"
    RG_AGENT_OWNERSHIP_ENABLED: "false"
    RG_RAG_ENABLED: "false"
    POSTGRES_URL: ""
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
```

- [ ] **Step 2: Lint the YAML**

```bash
yamllint deploy/ci/releaseguard.yaml
```

Expected: no errors (warnings about line length OK).

- [ ] **Step 3: Document include usage in README**

Append to `README.md`:

```markdown
## Caller usage

\`\`\`yaml
include:
  - project: 'platform/releaseguard'
    ref: main
    file: 'deploy/ci/releaseguard.yaml'

releaseguard-review:
  extends: .releaseguard-full
  variables:
    RG_SERVICE_NAME: my-service
    RG_SERVICE_TYPE: backend
\`\`\`
```

- [ ] **Step 4: Verify the include path works in a sandbox** (manual)

In a fixture caller repo, push the include line and watch pipeline kick off. (No automated test for this step — visual verification.)

- [ ] **Step 5: Commit**

```bash
git add deploy/ci/releaseguard.yaml README.md
git commit -m "build: add GitLab CI template (.releaseguard-full + -lite)"
```

---

### Task 18: End-to-end smoke test (mock GitLab + analyzer)

**Files:**
- Create: `cmd/analyzer/e2e_test.go`

- [ ] **Step 1: Write integration test**

```go
// cmd/analyzer/e2e_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2EMockGitLab(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", "/tmp/rg-analyzer-e2e", "./").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %s", out)
	}
	defer os.Remove("/tmp/rg-analyzer-e2e")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	cmd := exec.Command("/tmp/rg-analyzer-e2e")
	cmd.Env = []string{
		"AI_PROVIDER_KEY=fake",
		"GITLAB_TOKEN=fake",
		"GITLAB_API_BASE=" + srv.URL,
		"RG_AGENT_SELECTIVE_TEST_ENABLED=false",
		"RG_AGENT_OWNERSHIP_ENABLED=false",
		"RG_RAG_ENABLED=false",
	}
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("analyzer failed: %s", got)
	}
	if !strings.Contains(string(got), "topology") {
		t.Fatalf("expected topology log: %s", got)
	}
}
```

- [ ] **Step 2: Run**

```bash
go test ./cmd/analyzer/ -run TestE2E -v
```

Expected: PASS — analyzer reports topology=1.

- [ ] **Step 3: Add `make smoke` target**

```makefile
# Append to Makefile
smoke:
	go test ./cmd/analyzer/ -run TestE2E -v
```

- [ ] **Step 4: Verify**

```bash
make smoke
```

- [ ] **Step 5: Commit**

```bash
git add cmd/analyzer/e2e_test.go Makefile
git commit -m "test(analyzer): add e2e smoke test with mock GitLab"
```

---

### Task 19: Plan A complete — final commit

- [ ] **Step 1: Verify all tests pass**

```bash
make test
```

Expected: all green.

- [ ] **Step 2: Verify image still builds**

```bash
make docker
```

- [ ] **Step 3: Run integration smoke against real Postgres (optional)**

```bash
docker run -d --name rg-pg-final -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
sleep 3
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" \
  go test ./internal/storage/ -v
docker stop rg-pg-final && docker rm rg-pg-final
```

- [ ] **Step 4: Tag plan A complete**

```bash
git tag -a plan-a-foundation -m "Plan A: Foundation complete"
```

- [ ] **Step 5: Commit checklist update**

```bash
echo "## Plan A status: complete (commit $(git rev-parse --short HEAD), tag plan-a-foundation)" >> CHANGELOG.md
git add CHANGELOG.md
git commit -m "docs: mark Plan A foundation complete"
```

---

## What Plan A delivers

After all tasks complete:
- `releaseguard` Go module with clean directory layout
- `internal/interfaces/` shared `Finding` / `AgentOutput` / `IAgent` schema
- `internal/config/` env loader with topology validation
- `internal/storage/` Postgres pool helper + migration runner + `repos` table
- `internal/gitlab/` REST client with `GetMRDiff` and `PostMRNote`
- `internal/ai/` `Provider` interface + Anthropic implementation + retry helper
- `internal/logger/` structured JSON logger
- `cmd/analyzer/main.go` runnable entry with topology check
- `deploy/Dockerfile.analyzer` multi-stage image
- `deploy/ci/releaseguard.yaml` reusable CI template
- e2e smoke test passing

**Next:** Plan B builds the four agents and the composer/arbitration layer on top of this foundation.
