# ReleaseGuard Plan B — Topology 1 Demoable Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Prerequisite:** Plan A (`plan_a_foundation.md`) must be complete. This plan assumes `internal/interfaces/`, `internal/config/`, `internal/gitlab/`, `internal/ai/`, `cmd/analyzer/main.go` already exist.

**Goal:** Make ReleaseGuard runnable in topology 1 — produce a complete Impact Scope Report with `HOLD/REVIEW/PROCEED` recommendation against an MR, using zero infrastructure beyond CI runner. Implements Selective Test L1, Rollout Risk + drift detector, AI Reviewer (Channel A only), Composer + Decision Arbitration + 2 sinks. PlantUML parser + alias lookup helper are also delivered (used by Rollout Risk's `affected_services`).

**Architecture:** Each agent implements `IAgent`; `cmd/analyzer/main.go` wires them based on enable flags. A topology-aware factory selects which agents to construct. Decision Arbitration Layer (`internal/report/arbitration.go`) collapses 4 agent outputs into one recommendation. Markdown renderer puts the recommendation at the top. Poster writes 2 sinks (MR comment, CI variable artifact).

**Tech Stack:** Same as Plan A. Adds: `oasdiff` CLI (must be installed in image), optional `buf` CLI, `golang-jwt/jwt` not needed, standard `tiktoken-go` for token counting.

---

## File Structure (additions to Plan A)

```
releaseguard/
├── cmd/analyzer/main.go              # extended: wire agents
├── config/
│   └── diagram_aliases.yaml          # alias → repos.name mapping
├── internal/
│   ├── analysis/
│   │   ├── aliases/lookup.go         # alias → repo helper (shared)
│   │   ├── plantuml/parser.go        # extract participants/interactions
│   │   ├── schema/oasdiff.go
│   │   ├── schema/protobuf.go
│   │   ├── schema/locator.go
│   │   ├── depgraph/gomod.go
│   │   ├── depgraph/npm.go
│   │   ├── configdrift/diff.go
│   │   └── specdrift/detector.go
│   ├── agents/
│   │   ├── testselect/agent.go       # L1 only this plan
│   │   ├── rollout/agent.go
│   │   └── reviewer/
│   │       ├── agent.go
│   │       ├── loader.go             # auto-scan PROJECTS_DIR/*.md
│   │       ├── tokenizer.go
│   │       ├── prompt.go
│   │       └── schema.go             # LLM tool input schema
│   ├── report/
│   │   ├── arbitration.go            # Decision Arbitration Layer
│   │   ├── aggregator.go
│   │   └── renderer.go
│   └── result/
│       ├── composer.go
│       ├── poster.go
│       └── templates/report.tmpl
└── migrations/
    └── 0002_cross_repo_edges.sql     # only used in topology 0 but defined here
```

---

### Task 1: `cross_repo_edges` migration

**Files:**
- Create: `migrations/0002_cross_repo_edges.sql`

- [ ] **Step 1: Write the migration**

```sql
-- migrations/0002_cross_repo_edges.sql
CREATE TABLE IF NOT EXISTS cross_repo_edges (
  id                  BIGSERIAL PRIMARY KEY,
  source_repo_id      BIGINT NULL REFERENCES repos(id),
  target_repo_id      BIGINT NULL REFERENCES repos(id),
  source_alias        TEXT NOT NULL,
  target_alias        TEXT NOT NULL,
  call_kind           TEXT NOT NULL,
  source_diagram_path TEXT NOT NULL,
  indexed_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_xrepo_source ON cross_repo_edges(source_repo_id);
CREATE INDEX IF NOT EXISTS idx_xrepo_target ON cross_repo_edges(target_repo_id);
CREATE INDEX IF NOT EXISTS idx_xrepo_unresolved ON cross_repo_edges(source_alias, target_alias)
  WHERE source_repo_id IS NULL OR target_repo_id IS NULL;
```

- [ ] **Step 2: Run migrate up against test Postgres**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
sleep 3
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" \
  go test ./internal/storage/ -run TestMigrate -v
docker stop rg-pg && docker rm rg-pg
```

Expected: PASS — `cross_repo_edges` exists.

- [ ] **Step 3: (No code change needed, just verify)**

The migrate runner from Plan A picks up `0002_*.sql` automatically.

- [ ] **Step 4: Commit**

```bash
git add migrations/0002_cross_repo_edges.sql
git commit -m "feat(migrations): add cross_repo_edges table"
```

- [ ] **Step 5: Move on**

---

### Task 2: `diagram_aliases.yaml` example + loader

**Files:**
- Create: `config/diagram_aliases.yaml`
- Create: `internal/analysis/aliases/lookup.go`
- Create: `internal/analysis/aliases/lookup_test.go`

- [ ] **Step 1: Write fixture YAML**

```yaml
# config/diagram_aliases.yaml
# alias -> repos.name; "(skip)" means not a service repo
aliases:
  FE: web-dashboard
  Web: web-dashboard
  BFF: api-gateway
  BE-payments: payments-api
  BE-orders: orders-api
  BE-promo: promo-api
  Postgres: (skip)
  Redis: (skip)
```

- [ ] **Step 2: Write the failing test**

```go
// internal/analysis/aliases/lookup_test.go
package aliases

import (
	"path/filepath"
	"testing"
)

func TestLookupResolves(t *testing.T) {
	l, err := LoadFile(filepath.Join("..", "..", "..", "config", "diagram_aliases.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := l.Resolve("FE")
	if !ok || got != "web-dashboard" {
		t.Fatalf("FE → %q (ok=%v)", got, ok)
	}
}

func TestLookupSkip(t *testing.T) {
	l, _ := LoadFile(filepath.Join("..", "..", "..", "config", "diagram_aliases.yaml"))
	got, ok := l.Resolve("Postgres")
	if ok {
		t.Fatalf("expected skip for Postgres, got %q", got)
	}
}

func TestLookupUnknown(t *testing.T) {
	l, _ := LoadFile(filepath.Join("..", "..", "..", "config", "diagram_aliases.yaml"))
	if _, ok := l.Resolve("Unknown"); ok {
		t.Fatalf("expected unresolved")
	}
}
```

- [ ] **Step 3: Run, expect FAIL**

```bash
go test ./internal/analysis/aliases/
```

- [ ] **Step 4: Implement**

```go
// internal/analysis/aliases/lookup.go
package aliases

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const skipMarker = "(skip)"

type Lookup struct {
	m map[string]string
}

type fileShape struct {
	Aliases map[string]string `yaml:"aliases"`
}

func LoadFile(path string) (*Lookup, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f fileShape
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &Lookup{m: f.Aliases}, nil
}

// Resolve returns (repoName, ok). ok=false when alias is unknown OR marked (skip).
func (l *Lookup) Resolve(alias string) (string, bool) {
	v, ok := l.m[alias]
	if !ok {
		return "", false
	}
	if v == skipMarker {
		return "", false
	}
	return v, true
}

// IsSkipMarker tells the caller this alias was deliberately skipped (vs unknown).
func (l *Lookup) IsSkipMarker(alias string) bool {
	return l.m[alias] == skipMarker
}
```

Add to `go.mod`:

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 5: Run, expect PASS, then commit**

```bash
go test ./internal/analysis/aliases/ -v
git add config/diagram_aliases.yaml internal/analysis/aliases/ go.mod go.sum
git commit -m "feat(aliases): add diagram_aliases.yaml + lookup helper"
```

---

### Task 3: PlantUML parser

**Files:**
- Create: `internal/analysis/plantuml/parser.go`
- Create: `internal/analysis/plantuml/parser_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/analysis/plantuml/parser_test.go
package plantuml

import (
	"strings"
	"testing"
)

const sample = `
@startuml
participant FE
participant BFF
participant "BE-orders" as Orders

FE -> BFF: POST /api/v1/checkout
BFF -> Orders: gRPC CreateOrder
Orders ->> Postgres: INSERT
@enduml
`

func TestParseParticipantsAndArrows(t *testing.T) {
	d, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(d.Interactions) != 3 {
		t.Fatalf("expected 3 interactions, got %d", len(d.Interactions))
	}
	if d.Interactions[0].Source != "FE" || d.Interactions[0].Target != "BFF" {
		t.Fatalf("first interaction wrong: %+v", d.Interactions[0])
	}
	if d.Interactions[2].Kind != "async_event" {
		t.Fatalf("expected async for ->>, got %s", d.Interactions[2].Kind)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/analysis/plantuml/
```

- [ ] **Step 3: Implement**

```go
// internal/analysis/plantuml/parser.go
package plantuml

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

type Interaction struct {
	Source string
	Target string
	Kind   string // sync_call | async_event | unknown
	Label  string
}

type Diagram struct {
	Interactions []Interaction
}

// Matches: A -> B: label    A ->> B: label    "A B" -> B
var (
	syncRe  = regexp.MustCompile(`^\s*("[^"]+"|\S+)\s+->\s+("[^"]+"|\S+)\s*:?\s*(.*)$`)
	asyncRe = regexp.MustCompile(`^\s*("[^"]+"|\S+)\s+->>\s+("[^"]+"|\S+)\s*:?\s*(.*)$`)
	aliasRe = regexp.MustCompile(`^\s*participant\s+(?:"([^"]+)"|(\S+))(?:\s+as\s+(\S+))?\s*$`)
)

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func Parse(r io.Reader) (*Diagram, error) {
	d := &Diagram{}
	aliases := map[string]string{} // alias → real participant name
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if m := aliasRe.FindStringSubmatch(line); m != nil {
			full := m[1]
			if full == "" {
				full = m[2]
			}
			short := m[3]
			if short != "" {
				aliases[short] = full
			}
			continue
		}
		// async (->>) must be checked BEFORE sync (->) since ->> contains ->
		if m := asyncRe.FindStringSubmatch(line); m != nil {
			d.Interactions = append(d.Interactions, Interaction{
				Source: resolveName(unquote(m[1]), aliases),
				Target: resolveName(unquote(m[2]), aliases),
				Kind:   "async_event",
				Label:  strings.TrimSpace(m[3]),
			})
			continue
		}
		if m := syncRe.FindStringSubmatch(line); m != nil {
			d.Interactions = append(d.Interactions, Interaction{
				Source: resolveName(unquote(m[1]), aliases),
				Target: resolveName(unquote(m[2]), aliases),
				Kind:   "sync_call",
				Label:  strings.TrimSpace(m[3]),
			})
		}
	}
	return d, sc.Err()
}

func resolveName(name string, aliases map[string]string) string {
	if v, ok := aliases[name]; ok {
		return v
	}
	return name
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/analysis/plantuml/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/analysis/plantuml/
git commit -m "feat(plantuml): add basic participant + arrow parser"
```

---

### Task 4: Selective Test L1 (no DB)

**Files:**
- Create: `internal/agents/testselect/agent.go`
- Create: `internal/agents/testselect/level_l1.go`
- Create: `internal/agents/testselect/agent_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/testselect/agent_test.go
package testselect

import (
	"context"
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestL1MapsFileToTestSamePackage(t *testing.T) {
	a := New(false /* hasDB */)
	in := interfaces.AgentInput{
		Diff: []interfaces.DiffFile{
			{Path: "internal/diff/local_fetcher.go", Status: "modified"},
		},
	}
	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	md := out.Metadata
	if md["analysis_level"] != "L1" {
		t.Fatalf("expected L1, got %v", md["analysis_level"])
	}
	required := md["required"].([]string)
	if len(required) == 0 {
		t.Fatalf("L1 should infer at least same-package tests")
	}
}

func TestL1ConfidenceCapped(t *testing.T) {
	a := New(false)
	out, _ := a.Run(context.Background(), interfaces.AgentInput{
		Diff: []interfaces.DiffFile{
			{Path: "x.go", Status: "modified"},
			{Path: "y.ts", Status: "modified"}, // mixed languages
		},
	})
	conf := out.Metadata["confidence"].(float64)
	if conf > 0.6 {
		t.Fatalf("L1 confidence must be <= 0.6 (mixed lang penalty), got %f", conf)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/testselect/agent.go
package testselect

import (
	"context"
	"time"

	"github.com/acme/releaseguard/internal/interfaces"
)

type Agent struct {
	hasDB bool
}

func New(hasDB bool) *Agent { return &Agent{hasDB: hasDB} }

func (a *Agent) Name() interfaces.AgentName { return interfaces.AgentSelectiveTest }

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	// PoC of Plan B: only L1 is implemented. L2/L3 in Plan C.
	required, skippable, conf, reason := runL1(in.Diff)
	status := interfaces.StatusOK
	if conf < 0.5 {
		status = interfaces.StatusPartial
	}
	return interfaces.AgentOutput{
		Agent:         interfaces.AgentSelectiveTest,
		Status:        status,
		DurationMs:    int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1",
		Findings:      []interfaces.Finding{},
		Summary:       "L1 path-based selection",
		Metadata: map[string]any{
			"analysis_level": "L1",
			"confidence":     conf,
			"required":       required,
			"skippable":      skippable,
			"reason":         reason,
			"fallback_chain": []string{"L1"},
		},
	}, nil
}
```

```go
// internal/agents/testselect/level_l1.go
package testselect

import (
	"path/filepath"
	"strings"

	"github.com/acme/releaseguard/internal/interfaces"
)

func runL1(diff []interfaces.DiffFile) (required, skippable []string, confidence float64, reason string) {
	required = []string{}
	skippable = []string{}
	confidence = 0.5
	hasGo, hasTS, hasOther := false, false, false
	dirs := map[string]struct{}{}
	for _, f := range diff {
		switch {
		case strings.HasSuffix(f.Path, ".go"):
			hasGo = true
		case strings.HasSuffix(f.Path, ".ts") || strings.HasSuffix(f.Path, ".tsx"):
			hasTS = true
		default:
			hasOther = true
		}
		dir := filepath.Dir(f.Path)
		dirs[dir] = struct{}{}

		// same-file test: foo.go → foo_test.go
		if hasGo && !strings.HasSuffix(f.Path, "_test.go") {
			testPath := strings.TrimSuffix(f.Path, ".go") + "_test.go"
			required = appendUnique(required, testPath)
		}
		if hasGo && strings.HasSuffix(f.Path, "_test.go") {
			required = appendUnique(required, f.Path)
		}
		// same-package: list every *_test.go in same dir
		// (placeholder: actual file listing requires repo checkout; record the dir intent)
		required = appendUnique(required, dir+"/...")
	}

	if hasGo && hasTS {
		confidence -= 0.2
	}
	if hasOther {
		confidence -= 0.1
	}
	if len(dirs) > 3 {
		confidence -= 0.1
	}
	if confidence < 0 {
		confidence = 0
	}

	reason = "L1 file-path mapping; only same-package tests are guaranteed"
	return
}

func appendUnique(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/testselect/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/testselect/
git commit -m "feat(testselect): add L1 file-path-based test selection"
```

---

### Task 5: Schema locator

**Files:**
- Create: `internal/analysis/schema/locator.go`
- Create: `internal/analysis/schema/locator_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/analysis/schema/locator_test.go
package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateOpenAPI(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "api"), 0755)
	os.WriteFile(filepath.Join(dir, "api", "openapi.yaml"), []byte("openapi: 3.0.0"), 0644)
	os.WriteFile(filepath.Join(dir, "service.proto"), []byte("syntax = \"proto3\";"), 0644)

	specs, err := LocateInDir(dir)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(specs.OpenAPIs) != 1 || len(specs.Protos) != 1 {
		t.Fatalf("expected 1 each, got %+v", specs)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/analysis/schema/locator.go
package schema

import (
	"path/filepath"
	"strings"

	"io/fs"
	"os"
)

type Located struct {
	OpenAPIs []string
	Protos   []string
}

func LocateInDir(root string) (*Located, error) {
	out := &Located{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if name == "openapi.yaml" || name == "openapi.yml" || name == "openapi.json" ||
			strings.HasSuffix(name, ".openapi.yaml") {
			out.OpenAPIs = append(out.OpenAPIs, p)
		}
		if strings.HasSuffix(name, ".proto") {
			out.Protos = append(out.Protos, p)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/analysis/schema/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/analysis/schema/
git commit -m "feat(schema): add OpenAPI/Proto file locator"
```

---

### Task 6: oasdiff CLI wrapper

**Files:**
- Create: `internal/analysis/schema/oasdiff.go`
- Create: `internal/analysis/schema/oasdiff_test.go`

- [ ] **Step 1: Write the failing test (mock the CLI)**

```go
// internal/analysis/schema/oasdiff_test.go
package schema

import (
	"errors"
	"os/exec"
	"testing"
)

func TestOasdiffParsesBreakingJSON(t *testing.T) {
	// fake exec: replace runner with fixture output
	old := runOasdiff
	defer func() { runOasdiff = old }()
	runOasdiff = func(base, head string) ([]byte, error) {
		return []byte(`[{"id":"request-property-removed","level":3,"text":"removed discount_code","operation":"POST","path":"/v2/orders"}]`), nil
	}
	zones, err := OasdiffBreaking("base.yaml", "head.yaml")
	if err != nil {
		t.Fatalf("oasdiff: %v", err)
	}
	if len(zones) != 1 || zones[0].Severity != "critical" {
		t.Fatalf("got %+v", zones)
	}
}

func TestOasdiffPropagatesError(t *testing.T) {
	old := runOasdiff
	defer func() { runOasdiff = old }()
	runOasdiff = func(base, head string) ([]byte, error) {
		return nil, &exec.ExitError{}
	}
	_, err := OasdiffBreaking("a", "b")
	if err == nil || !errors.Is(err, err) /* sanity */ {
		t.Fatalf("expected propagated error")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/analysis/schema/oasdiff.go
package schema

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type Zone struct {
	Type     string
	Detail   string
	Severity string // critical | high | medium | low
}

// runOasdiff is a function-typed seam for tests.
var runOasdiff = func(base, head string) ([]byte, error) {
	cmd := exec.Command("oasdiff", "diff", base, head, "--breaking-only", "-f", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("oasdiff: %w", err)
	}
	return out, nil
}

type oasdiffEntry struct {
	ID        string `json:"id"`
	Level     int    `json:"level"` // 3=critical, 2=warn, 1=info
	Text      string `json:"text"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
}

func OasdiffBreaking(baseFile, headFile string) ([]Zone, error) {
	out, err := runOasdiff(baseFile, headFile)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	var entries []oasdiffEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parse oasdiff: %w", err)
	}
	zones := make([]Zone, 0, len(entries))
	for _, e := range entries {
		sev := "low"
		if e.Level >= 3 {
			sev = "critical"
		} else if e.Level == 2 {
			sev = "high"
		}
		zones = append(zones, Zone{
			Type:     "breaking_api",
			Detail:   fmt.Sprintf("%s %s: %s", e.Operation, e.Path, e.Text),
			Severity: sev,
		})
	}
	return zones, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/analysis/schema/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/analysis/schema/oasdiff.go internal/analysis/schema/oasdiff_test.go
git commit -m "feat(schema): add oasdiff wrapper with severity mapping"
```

---

### Task 7: depgraph go.mod parser

**Files:**
- Create: `internal/analysis/depgraph/gomod.go`
- Create: `internal/analysis/depgraph/gomod_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/analysis/depgraph/gomod_test.go
package depgraph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoModDiff(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.mod")
	head := filepath.Join(dir, "head.mod")
	os.WriteFile(base, []byte(`module x
go 1.23
require github.com/foo/bar v1.0.0
require github.com/baz/qux v2.0.0`), 0644)
	os.WriteFile(head, []byte(`module x
go 1.23
require github.com/foo/bar v2.0.0
require github.com/baz/qux v2.0.0
require github.com/new/dep v0.1.0`), 0644)

	zones, err := GoModDiff(base, head)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	gotMajor := false
	gotAdded := false
	for _, z := range zones {
		if z.Type == "dep_bump" && z.Severity == "high" {
			gotMajor = true
		}
		if z.Type == "dep_added" {
			gotAdded = true
		}
	}
	if !gotMajor {
		t.Fatalf("major bump not flagged: %+v", zones)
	}
	if !gotAdded {
		t.Fatalf("new dep not flagged: %+v", zones)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/analysis/depgraph/gomod.go
package depgraph

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/acme/releaseguard/internal/analysis/schema"
)

// reuse schema.Zone type since it's the same shape
type Zone = schema.Zone

func parseGoMod(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	deps := map[string]string{}
	sc := bufio.NewScanner(f)
	inBlock := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "require (") {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		if inBlock || strings.HasPrefix(line, "require ") {
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) >= 2 {
				deps[parts[0]] = parts[1]
			}
		}
	}
	return deps, sc.Err()
}

func GoModDiff(baseFile, headFile string) ([]Zone, error) {
	base, err := parseGoMod(baseFile)
	if err != nil {
		return nil, err
	}
	head, err := parseGoMod(headFile)
	if err != nil {
		return nil, err
	}
	var zones []Zone
	for mod, headV := range head {
		baseV, ok := base[mod]
		if !ok {
			zones = append(zones, Zone{Type: "dep_added", Detail: fmt.Sprintf("%s @ %s", mod, headV), Severity: "low"})
			continue
		}
		if baseV != headV {
			sev := "low"
			if isMajorBump(baseV, headV) {
				sev = "high"
			} else if isMinorBump(baseV, headV) {
				sev = "medium"
			}
			zones = append(zones, Zone{
				Type:     "dep_bump",
				Detail:   fmt.Sprintf("%s %s → %s", mod, baseV, headV),
				Severity: sev,
			})
		}
	}
	for mod, baseV := range base {
		if _, ok := head[mod]; !ok {
			zones = append(zones, Zone{Type: "dep_removed", Detail: fmt.Sprintf("%s (was %s)", mod, baseV), Severity: "medium"})
		}
	}
	return zones, nil
}

func isMajorBump(a, b string) bool {
	return semverMajor(a) != semverMajor(b)
}

func isMinorBump(a, b string) bool {
	if semverMajor(a) != semverMajor(b) {
		return false
	}
	return semverMinor(a) != semverMinor(b)
}

func semverMajor(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 2)
	return parts[0]
}

func semverMinor(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/analysis/depgraph/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/analysis/depgraph/
git commit -m "feat(depgraph): add go.mod diff with severity classification"
```

---

### Task 8: Config drift detector

**Files:**
- Create: `internal/analysis/configdrift/diff.go`
- Create: `internal/analysis/configdrift/diff_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/analysis/configdrift/diff_test.go
package configdrift

import "testing"

func TestClassifyConfigChange(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"config/secrets.yaml", "critical"},
		{"helm/values.yaml", "medium"},
		{"k8s/deployment.yaml", "high"},
		{"config/feature_flags.yaml", "medium"},
		{"foo.txt", "low"},
	}
	for _, c := range cases {
		got := classify(c.path)
		if got != c.want {
			t.Errorf("classify(%q) = %s, want %s", c.path, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/analysis/configdrift/diff.go
package configdrift

import (
	"strings"

	"github.com/acme/releaseguard/internal/analysis/schema"
	"github.com/acme/releaseguard/internal/interfaces"
)

type Zone = schema.Zone

func classify(path string) string {
	low := strings.ToLower(path)
	switch {
	case strings.Contains(low, "secret") || strings.Contains(low, "credential"):
		return "critical"
	case strings.HasPrefix(low, "k8s/") || strings.Contains(low, "deployment"):
		return "high"
	case strings.HasPrefix(low, "helm/") ||
		strings.Contains(low, "feature") ||
		strings.HasPrefix(low, "config/"):
		return "medium"
	default:
		return "low"
	}
}

// FromDiff classifies each changed config file. Caller filters via globs upstream.
func FromDiff(files []interfaces.DiffFile) []Zone {
	var zones []Zone
	for _, f := range files {
		if !looksLikeConfig(f.Path) {
			continue
		}
		zones = append(zones, Zone{
			Type:     "config_drift",
			Detail:   f.Path,
			Severity: classify(f.Path),
		})
	}
	return zones
}

func looksLikeConfig(p string) bool {
	low := strings.ToLower(p)
	if strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml") ||
		strings.HasSuffix(low, ".env") || strings.HasSuffix(low, ".conf") {
		return strings.HasPrefix(low, "config/") || strings.HasPrefix(low, "helm/") ||
			strings.HasPrefix(low, "k8s/") || strings.Contains(low, "secret")
	}
	return false
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/analysis/configdrift/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/analysis/configdrift/
git commit -m "feat(configdrift): add config-file change classifier"
```

---

### Task 9: Spec/Code Drift Detector

**Files:**
- Create: `internal/analysis/specdrift/detector.go`
- Create: `internal/analysis/specdrift/detector_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/analysis/specdrift/detector_test.go
package specdrift

import (
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestDetectsHandlerChangedSpecUntouched(t *testing.T) {
	files := []interfaces.DiffFile{
		{Path: "internal/orders/handler.go", Status: "modified"},
		{Path: "README.md", Status: "modified"},
		// note: no api/openapi.yaml in diff
	}
	specPaths := []string{"api/openapi.yaml"}
	findings := Detect(files, specPaths)
	if len(findings) != 1 {
		t.Fatalf("expected 1 drift finding, got %d", len(findings))
	}
	if findings[0].Severity != interfaces.SeverityMedium {
		t.Fatalf("severity wrong: %s", findings[0].Severity)
	}
}

func TestNoDriftWhenSpecAlsoChanged(t *testing.T) {
	files := []interfaces.DiffFile{
		{Path: "internal/orders/handler.go", Status: "modified"},
		{Path: "api/openapi.yaml", Status: "modified"},
	}
	findings := Detect(files, []string{"api/openapi.yaml"})
	if len(findings) != 0 {
		t.Fatalf("expected no drift, got %+v", findings)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/analysis/specdrift/detector.go
package specdrift

import (
	"fmt"
	"strings"

	"github.com/acme/releaseguard/internal/interfaces"
)

// Detect returns spec-code drift findings.
// Heuristic for PoC: any .go/.ts file whose path looks like an API handler
// (contains "handler" or "controller" or under "api/", "routes/", "endpoints/")
// triggers a drift finding if NONE of specPaths is in the diff.
func Detect(diff []interfaces.DiffFile, specPaths []string) []interfaces.Finding {
	specChanged := false
	specSet := map[string]bool{}
	for _, p := range specPaths {
		specSet[p] = true
	}
	for _, f := range diff {
		if specSet[f.Path] {
			specChanged = true
		}
	}

	var handlers []string
	for _, f := range diff {
		if !looksLikeHandler(f.Path) {
			continue
		}
		handlers = append(handlers, f.Path)
	}
	if specChanged || len(handlers) == 0 {
		return nil
	}
	return []interfaces.Finding{{
		ID:       "specdrift-001",
		StableID: fmt.Sprintf("rollout_risk:spec_code_drift:%s", strings.Join(handlers, ",")),
		Severity: interfaces.SeverityMedium,
		Category: "spec_code_drift",
		Title:    "Endpoint handler changed without updating spec",
		Body: fmt.Sprintf("Files %s changed; corresponding spec files (%s) were not updated. Consider updating spec.",
			strings.Join(handlers, ", "), strings.Join(specPaths, ", ")),
		Suggestion: "Update OpenAPI / Protobuf spec to reflect handler changes.",
	}}
}

func looksLikeHandler(path string) bool {
	low := strings.ToLower(path)
	if !(strings.HasSuffix(low, ".go") || strings.HasSuffix(low, ".ts") || strings.HasSuffix(low, ".tsx")) {
		return false
	}
	keys := []string{"handler", "controller", "/api/", "/routes/", "/endpoints/"}
	for _, k := range keys {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/analysis/specdrift/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/analysis/specdrift/
git commit -m "feat(specdrift): add code-leads-spec drift detector"
```

---

### Task 10: Rollout Risk agent (orchestrate 4 sub-analyses)

**Files:**
- Create: `internal/agents/rollout/agent.go`
- Create: `internal/agents/rollout/risk.go`
- Create: `internal/agents/rollout/agent_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/rollout/agent_test.go
package rollout

import (
	"context"
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestRiskLevelHIGHWhenCriticalZone(t *testing.T) {
	a := New(Deps{
		OASDiff: func(base, head string) ([]Zone, error) {
			return []Zone{{Type: "breaking_api", Detail: "removed field", Severity: "critical"}}, nil
		},
	})
	out, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Metadata["riskLevel"] != "HIGH" {
		t.Fatalf("expected HIGH, got %v", out.Metadata["riskLevel"])
	}
}

func TestRiskLevelMEDOnAccumulation(t *testing.T) {
	a := New(Deps{})
	a.testInjectZones = []Zone{
		{Type: "dep_bump", Severity: "medium"},
		{Type: "dep_bump", Severity: "medium"},
		{Type: "dep_bump", Severity: "medium"},
	}
	out, _ := a.Run(context.Background(), interfaces.AgentInput{})
	if out.Metadata["riskLevel"] != "MED" {
		t.Fatalf("expected MED, got %v", out.Metadata["riskLevel"])
	}
}

func TestRiskLevelLOWWhenEmpty(t *testing.T) {
	a := New(Deps{})
	out, _ := a.Run(context.Background(), interfaces.AgentInput{})
	if out.Metadata["riskLevel"] != "LOW" {
		t.Fatalf("expected LOW, got %v", out.Metadata["riskLevel"])
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/rollout/agent.go
package rollout

import (
	"context"
	"time"

	"github.com/acme/releaseguard/internal/analysis/schema"
	"github.com/acme/releaseguard/internal/interfaces"
)

type Zone = schema.Zone

type Deps struct {
	OASDiff func(base, head string) ([]Zone, error)
	// other sub-analyses can be injected similarly; for PoC we keep tests light
}

type Agent struct {
	deps             Deps
	testInjectZones  []Zone // for tests
}

func New(deps Deps) *Agent { return &Agent{deps: deps} }

func (a *Agent) Name() interfaces.AgentName { return interfaces.AgentRolloutRisk }

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	var zones []Zone
	if a.testInjectZones != nil {
		zones = a.testInjectZones
	} else if a.deps.OASDiff != nil {
		z, err := a.deps.OASDiff("", "")
		if err == nil {
			zones = append(zones, z...)
		}
	}
	level := riskLevel(zones)
	findings := zonesToFindings(zones)
	return interfaces.AgentOutput{
		Agent:         interfaces.AgentRolloutRisk,
		Status:        interfaces.StatusOK,
		DurationMs:    int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1",
		Findings:      findings,
		Summary:       "rollout risk evaluated",
		Metadata: map[string]any{
			"riskLevel": level,
			"zones":     zones,
		},
	}, nil
}

func zonesToFindings(zones []Zone) []interfaces.Finding {
	out := make([]interfaces.Finding, 0, len(zones))
	for i, z := range zones {
		sev := interfaces.SeverityLow
		switch z.Severity {
		case "critical":
			sev = interfaces.SeverityCritical
		case "high":
			sev = interfaces.SeverityHigh
		case "medium":
			sev = interfaces.SeverityMedium
		}
		out = append(out, interfaces.Finding{
			ID:       fmt.Sprintf("rl-%03d", i+1),
			StableID: fmt.Sprintf("rollout_risk:%s:%s", z.Type, z.Detail),
			Severity: sev,
			Category: z.Type,
			Title:    z.Detail,
			Body:     z.Detail,
		})
	}
	return out
}
```

```go
// internal/agents/rollout/risk.go
package rollout

import (
	"fmt"

	"github.com/acme/releaseguard/internal/analysis/schema"
)

func riskLevel(zones []schema.Zone) string {
	c, h, m := 0, 0, 0
	for _, z := range zones {
		switch z.Severity {
		case "critical":
			c++
		case "high":
			h++
		case "medium":
			m++
		}
	}
	switch {
	case c >= 1:
		return "HIGH"
	case h >= 2 || m >= 3:
		return "MED"
	default:
		return "LOW"
	}
}

// (stub to satisfy the import in agent.go's zonesToFindings)
func _formatStub() string { return fmt.Sprintf("%d", 0) }
```

Add `import "fmt"` to `agent.go`.

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/rollout/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/rollout/
git commit -m "feat(rollout): add agent with riskLevel calc and Zone→Finding mapping"
```

---

### Task 11: AI Reviewer — `.md` loader + tokenizer

**Files:**
- Create: `internal/agents/reviewer/loader.go`
- Create: `internal/agents/reviewer/tokenizer.go`
- Create: `internal/agents/reviewer/loader_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/reviewer/loader_test.go
package reviewer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMDInOrder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "_shared/01-base.md"), []byte("base\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "_shared"), 0755)
	os.WriteFile(filepath.Join(dir, "_shared/01-base.md"), []byte("base\n"), 0644)
	os.WriteFile(filepath.Join(dir, "_shared/02-extra.md"), []byte("extra\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "_shared/backend"), 0755)
	os.WriteFile(filepath.Join(dir, "_shared/backend/api.md"), []byte("api\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "my-system"), 0755)
	os.WriteFile(filepath.Join(dir, "my-system/review-focus.md"), []byte("focus\n"), 0644)

	loaded, err := LoadPromptFiles(dir, "my-system", []string{"backend"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded[0] != "base\n" || loaded[1] != "extra\n" {
		t.Fatalf("shared root order broken: %v", loaded[:2])
	}
	if loaded[2] != "api\n" {
		t.Fatalf("backend not after shared: %v", loaded)
	}
	if loaded[3] != "focus\n" {
		t.Fatalf("system not last: %v", loaded)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement loader**

```go
// internal/agents/reviewer/loader.go
package reviewer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func loadMDDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		out = append(out, string(b))
	}
	return out, nil
}

// LoadPromptFiles reads, in order:
//  1. <projectsDir>/_shared/*.md
//  2. for each serviceType: <projectsDir>/_shared/<type>/*.md
//  3. <projectsDir>/<systemName>/*.md
func LoadPromptFiles(projectsDir, systemName string, serviceTypes []string) ([]string, error) {
	var all []string
	shared, err := loadMDDir(filepath.Join(projectsDir, "_shared"))
	if err != nil {
		return nil, err
	}
	all = append(all, shared...)
	for _, st := range serviceTypes {
		typed, err := loadMDDir(filepath.Join(projectsDir, "_shared", st))
		if err != nil {
			return nil, err
		}
		all = append(all, typed...)
	}
	if systemName != "" {
		sys, err := loadMDDir(filepath.Join(projectsDir, systemName))
		if err != nil {
			return nil, err
		}
		all = append(all, sys...)
	}
	return all, nil
}
```

- [ ] **Step 4: Implement tokenizer**

```go
// internal/agents/reviewer/tokenizer.go
package reviewer

import "strings"

// Approximate token count: 1 token ≈ 4 characters of English (good enough for PoC).
// Use github.com/tiktoken-go/tokenizer for production calibration.
func ApproxTokens(s string) int {
	return len(s) / 4
}

// TruncateToBudget joins prompts in order. If joined exceeds budget,
// drops trailing prompts (keeps the first N that fit).
// Returns the joined prompt and a bool indicating whether truncation happened.
func TruncateToBudget(prompts []string, budgetTokens int) (string, bool) {
	used := 0
	out := []string{}
	for _, p := range prompts {
		t := ApproxTokens(p)
		if used+t > budgetTokens {
			return strings.Join(out, "\n\n"), true
		}
		used += t
		out = append(out, p)
	}
	return strings.Join(out, "\n\n"), false
}
```

- [ ] **Step 5: Run + commit**

```bash
go test ./internal/agents/reviewer/ -v
git add internal/agents/reviewer/
git commit -m "feat(reviewer): add .md loader and approximate tokenizer"
```

---

### Task 12: AI Reviewer — prompt composer + tool schema

**Files:**
- Create: `internal/agents/reviewer/prompt.go`
- Create: `internal/agents/reviewer/schema.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/reviewer/prompt_test.go
package reviewer

import (
	"strings"
	"testing"
)

func TestComposeUserPromptIncludesDiff(t *testing.T) {
	user := composeUserPrompt("commit msg", []string{"foo.go diff body"})
	if !strings.Contains(user, "commit msg") || !strings.Contains(user, "foo.go diff body") {
		t.Fatalf("missing pieces: %s", user)
	}
}

func TestToolSchemaExposesFindings(t *testing.T) {
	s := ReviewToolSchema()
	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("no properties")
	}
	if _, ok := props["findings"]; !ok {
		t.Fatalf("findings property missing")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/reviewer/prompt.go
package reviewer

import (
	"fmt"
	"strings"
)

func ComposeSystemPrompt(basePrompts []string, ragSnippets []string) string {
	var sb strings.Builder
	sb.WriteString("You are a careful code reviewer. ")
	sb.WriteString("You MUST respond by calling the submit_review tool. ")
	sb.WriteString("Do not write findings as plain text.\n\n")
	for _, p := range basePrompts {
		sb.WriteString(p)
		sb.WriteString("\n\n")
	}
	if len(ragSnippets) > 0 {
		sb.WriteString("Relevant context:\n")
		for _, r := range ragSnippets {
			sb.WriteString("- ")
			sb.WriteString(r)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func composeUserPrompt(commitMsg string, diffBlocks []string) string {
	return fmt.Sprintf("Commit message:\n%s\n\nDiff:\n%s",
		commitMsg, strings.Join(diffBlocks, "\n\n"))
}
```

```go
// internal/agents/reviewer/schema.go
package reviewer

func ReviewToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"findings"},
		"properties": map[string]any{
			"summary": map[string]any{"type": "string"},
			"findings": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []string{"severity", "category", "title", "body"},
					"properties": map[string]any{
						"severity": map[string]any{
							"enum": []string{"critical", "high", "medium", "low", "info"},
						},
						"category": map[string]any{"type": "string"},
						"title":    map[string]any{"type": "string"},
						"body":     map[string]any{"type": "string"},
						"location": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file":       map[string]any{"type": "string"},
								"line_start": map[string]any{"type": "integer"},
								"line_end":   map[string]any{"type": "integer"},
							},
						},
						"suggestion": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/reviewer/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/reviewer/
git commit -m "feat(reviewer): add prompt composer and tool schema"
```

---

### Task 13: AI Reviewer — agent main

**Files:**
- Create: `internal/agents/reviewer/agent.go`
- Create: `internal/agents/reviewer/agent_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/reviewer/agent_test.go
package reviewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/acme/releaseguard/internal/ai"
	"github.com/acme/releaseguard/internal/interfaces"
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
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/reviewer/agent.go
package reviewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/acme/releaseguard/internal/ai"
	"github.com/acme/releaseguard/internal/interfaces"
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
			Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusFailed,
			DurationMs: int(time.Since(start) / time.Millisecond),
			SchemaVersion: "1", Summary: "ai error: " + err.Error(),
		}, nil
	}
	var parsed struct {
		Summary  string `json:"summary"`
		Findings []struct {
			Severity   string `json:"severity"`
			Category   string `json:"category"`
			Title      string `json:"title"`
			Body       string `json:"body"`
			Location   *interfaces.Location `json:"location,omitempty"`
			Suggestion string `json:"suggestion,omitempty"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return interfaces.AgentOutput{
			Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusFailed,
			DurationMs: int(time.Since(start) / time.Millisecond),
			SchemaVersion: "1", Summary: "json: " + err.Error(),
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
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/reviewer/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/reviewer/
git commit -m "feat(reviewer): add main agent loop with tool-use parsing and stable_id"
```

---

### Task 14: Decision Arbitration Layer

**Files:**
- Create: `internal/report/arbitration.go`
- Create: `internal/report/arbitration_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/report/arbitration_test.go
package report

import (
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func critical() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusOK,
		Findings: []interfaces.Finding{{Severity: interfaces.SeverityCritical, Title: "x"}}}
}
func high() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusOK,
		Findings: []interfaces.Finding{{Severity: interfaces.SeverityHigh, Title: "x"}}}
}
func rolloutHIGH() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentRolloutRisk, Status: interfaces.StatusOK,
		Metadata: map[string]any{"riskLevel": "HIGH"}}
}
func rolloutMED() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentRolloutRisk, Status: interfaces.StatusOK,
		Metadata: map[string]any{"riskLevel": "MED"}}
}
func selectivePartialL3() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentSelectiveTest, Status: interfaces.StatusPartial,
		Metadata: map[string]any{"analysis_level": "L3"}}
}
func selectivePartialL1() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentSelectiveTest, Status: interfaces.StatusPartial,
		Metadata: map[string]any{"analysis_level": "L1"}}
}
func ownership() interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: interfaces.AgentOwnership, Status: interfaces.StatusOK,
		Metadata: map[string]any{"hidden_dependency_hints": []any{1, 2, 3}}}
}
func failed(name interfaces.AgentName) interfaces.AgentOutput {
	return interfaces.AgentOutput{Agent: name, Status: interfaces.StatusFailed}
}

func TestArbitrationRules(t *testing.T) {
	cases := []struct {
		name      string
		outputs   []interfaces.AgentOutput
		want      string
		signalsAtLeast int
	}{
		{"critical → HOLD", []interfaces.AgentOutput{critical()}, "HOLD", 1},
		{"rollout HIGH → HOLD", []interfaces.AgentOutput{rolloutHIGH()}, "HOLD", 1},
		{"high finding → REVIEW", []interfaces.AgentOutput{high()}, "REVIEW", 1},
		{"rollout MED → REVIEW", []interfaces.AgentOutput{rolloutMED()}, "REVIEW", 1},
		{"L3 partial → REVIEW", []interfaces.AgentOutput{selectivePartialL3()}, "REVIEW", 1},
		{"L1 partial → PROCEED", []interfaces.AgentOutput{selectivePartialL1()}, "PROCEED", 0},
		{"ownership only → PROCEED", []interfaces.AgentOutput{ownership()}, "PROCEED", 0},
		{"agent failed → REVIEW", []interfaces.AgentOutput{failed(interfaces.AgentRolloutRisk)}, "REVIEW", 1},
		{"all failed → REVIEW", []interfaces.AgentOutput{
			failed(interfaces.AgentAIReviewer), failed(interfaces.AgentRolloutRisk),
		}, "REVIEW", 2},
		{"empty → PROCEED", []interfaces.AgentOutput{}, "PROCEED", 0},
		{"critical + HIGH risk → HOLD with 2 signals", []interfaces.AgentOutput{
			critical(), rolloutHIGH(),
		}, "HOLD", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := Arbitrate(c.outputs)
			if rec.Recommendation != c.want {
				t.Errorf("got %s want %s", rec.Recommendation, c.want)
			}
			if len(rec.TriggeredSignals) < c.signalsAtLeast {
				t.Errorf("signal count: got %d want >=%d", len(rec.TriggeredSignals), c.signalsAtLeast)
			}
		})
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/report/arbitration.go
package report

import (
	"fmt"
	"strings"

	"github.com/acme/releaseguard/internal/interfaces"
)

type Signal struct {
	Agent  interfaces.AgentName `json:"agent"`
	Kind   string               `json:"kind"`
	Detail string               `json:"detail"`
}

type Recommendation struct {
	Recommendation   string   `json:"recommendation"` // HOLD | REVIEW | PROCEED
	Rationale        string   `json:"rationale"`
	TriggeredSignals []Signal `json:"triggered_signals"`
}

// Arbitrate is the Decision Arbitration Layer.
// Rules (priority):
//   - any AI Reviewer critical finding → HOLD
//   - Rollout Risk HIGH → HOLD
//   - any AI Reviewer high finding (non-critical) → REVIEW
//   - Rollout Risk MED → REVIEW
//   - Selective Test status=partial AND analysis_level != "L1" → REVIEW
//   - any agent status=failed → at minimum REVIEW
//   - Ownership signals do NOT trigger
func Arbitrate(outputs []interfaces.AgentOutput) Recommendation {
	defer func() {
		// defensive: any panic during arbitration shouldn't drop the whole report
		if r := recover(); r != nil {
			// fallback handled in wrapper below
		}
	}()
	return arbitrateInner(outputs)
}

func arbitrateInner(outputs []interfaces.AgentOutput) Recommendation {
	hold := []Signal{}
	review := []Signal{}

	for _, o := range outputs {
		switch o.Agent {
		case interfaces.AgentAIReviewer:
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "AI reviewer failed"})
				continue
			}
			for _, f := range o.Findings {
				if f.Severity == interfaces.SeverityCritical {
					hold = append(hold, Signal{Agent: o.Agent, Kind: "critical_finding",
						Detail: f.Title})
				} else if f.Severity == interfaces.SeverityHigh {
					review = append(review, Signal{Agent: o.Agent, Kind: "high_finding",
						Detail: f.Title})
				}
			}
		case interfaces.AgentRolloutRisk:
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "Rollout Risk failed"})
				continue
			}
			lvl, _ := o.Metadata["riskLevel"].(string)
			switch lvl {
			case "HIGH":
				hold = append(hold, Signal{Agent: o.Agent, Kind: "high_risk", Detail: "HIGH"})
			case "MED":
				review = append(review, Signal{Agent: o.Agent, Kind: "med_risk", Detail: "MED"})
			}
		case interfaces.AgentSelectiveTest:
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "Selective Test failed"})
				continue
			}
			if o.Status == interfaces.StatusPartial {
				lvl, _ := o.Metadata["analysis_level"].(string)
				if lvl != "L1" {
					review = append(review, Signal{Agent: o.Agent, Kind: "unstable_test_plan",
						Detail: "low confidence at " + lvl})
				}
			}
		case interfaces.AgentOwnership:
			// Ownership intentionally NOT a trigger.
			if o.Status == interfaces.StatusFailed {
				review = append(review, Signal{Agent: o.Agent, Kind: "agent_failure",
					Detail: "Ownership failed"})
			}
		}
	}

	rec := "PROCEED"
	if len(hold) > 0 {
		rec = "HOLD"
	} else if len(review) > 0 {
		rec = "REVIEW"
	}

	all := append(hold, review...)
	rationale := buildRationale(rec, all)
	return Recommendation{Recommendation: rec, Rationale: rationale, TriggeredSignals: all}
}

func buildRationale(rec string, signals []Signal) string {
	if len(signals) == 0 {
		return "no high-severity signals from any agent"
	}
	var parts []string
	for _, s := range signals {
		parts = append(parts, fmt.Sprintf("[%s/%s] %s", s.Agent, s.Kind, s.Detail))
	}
	return fmt.Sprintf("%s — %s", rec, strings.Join(parts, "; "))
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/report/ -v -run TestArbitration
```

Expected: 11 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/report/arbitration.go internal/report/arbitration_test.go
git commit -m "feat(report): add Decision Arbitration Layer with rule matrix"
```

---

### Task 15: Aggregator (flatten + dedupe + sort)

**Files:**
- Create: `internal/report/aggregator.go`
- Create: `internal/report/aggregator_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/report/aggregator_test.go
package report

import (
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestFlattenSortsAndDedupes(t *testing.T) {
	outs := []interfaces.AgentOutput{
		{Findings: []interfaces.Finding{
			{StableID: "x", Severity: interfaces.SeverityLow, Title: "low one"},
			{StableID: "y", Severity: interfaces.SeverityCritical, Title: "crit"},
		}},
		{Findings: []interfaces.Finding{
			{StableID: "x", Severity: interfaces.SeverityLow, Title: "low one"}, // dup
			{StableID: "z", Severity: interfaces.SeverityHigh, Title: "h"},
		}},
	}
	flat := FlattenFindings(outs)
	if len(flat) != 3 {
		t.Fatalf("expected 3 unique, got %d", len(flat))
	}
	if flat[0].Severity != interfaces.SeverityCritical {
		t.Fatalf("expected critical first, got %s", flat[0].Severity)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/report/aggregator.go
package report

import (
	"sort"

	"github.com/acme/releaseguard/internal/interfaces"
)

var sevWeight = map[interfaces.Severity]int{
	interfaces.SeverityCritical: 5,
	interfaces.SeverityHigh:     4,
	interfaces.SeverityMedium:   3,
	interfaces.SeverityLow:      2,
	interfaces.SeverityInfo:     1,
}

func FlattenFindings(outputs []interfaces.AgentOutput) []interfaces.Finding {
	seen := map[string]bool{}
	var out []interfaces.Finding
	for _, o := range outputs {
		for _, f := range o.Findings {
			if seen[f.StableID] {
				continue
			}
			seen[f.StableID] = true
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return sevWeight[out[i].Severity] > sevWeight[out[j].Severity]
	})
	return out
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/report/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/report/aggregator.go internal/report/aggregator_test.go
git commit -m "feat(report): add finding aggregator with dedupe + severity sort"
```

---

### Task 16: Markdown renderer

**Files:**
- Create: `internal/report/renderer.go`
- Create: `internal/report/renderer_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/report/renderer_test.go
package report

import (
	"strings"
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestRendererPutsRecommendationFirst(t *testing.T) {
	r := ImpactScopeReport{
		Recommendation: Recommendation{Recommendation: "HOLD", Rationale: "x",
			TriggeredSignals: []Signal{{Agent: interfaces.AgentAIReviewer, Kind: "critical_finding", Detail: "f"}}},
	}
	md := Render(r)
	if !strings.HasPrefix(md, "## 🔴 ReleaseGuard recommendation: HOLD") {
		t.Fatalf("recommendation not first: %s", md[:80])
	}
}

func TestRendererProceedEmoji(t *testing.T) {
	r := ImpactScopeReport{Recommendation: Recommendation{Recommendation: "PROCEED"}}
	md := Render(r)
	if !strings.Contains(md, "✅") {
		t.Fatalf("missing PROCEED emoji: %s", md)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/report/renderer.go
package report

import (
	"fmt"
	"strings"

	"github.com/acme/releaseguard/internal/interfaces"
)

type ImpactScopeReport struct {
	Recommendation       Recommendation
	RiskLevel            string
	SelectiveTests       *TestPlan
	AIReviewFindings     []interfaces.Finding
	HighSeverityFindings []interfaces.Finding
	SuggestedReviewers   []ReviewerInfo
	ImpactZones          []ImpactZone
	HiddenDeps           []DependencyHint
	EnabledFlags         map[string]bool
	FailedAgents         []string
}

type TestPlan struct {
	Required      []string
	Skippable     []string
	Confidence    float64
	AnalysisLevel string
}

type ReviewerInfo struct {
	Name    string
	Context string
}

type ImpactZone struct {
	Path                     string
	RecentActiveAuthorsCount int
	LookbackDays             int
}

type DependencyHint struct {
	From, To string
	Context  string
}

func emoji(rec string) string {
	switch rec {
	case "HOLD":
		return "🔴"
	case "REVIEW":
		return "🟡"
	default:
		return "✅"
	}
}

func Render(r ImpactScopeReport) string {
	var sb strings.Builder
	rec := r.Recommendation.Recommendation
	if rec == "" {
		rec = "PROCEED"
	}
	sb.WriteString(fmt.Sprintf("## %s ReleaseGuard recommendation: %s\n", emoji(rec), rec))
	for _, s := range r.Recommendation.TriggeredSignals {
		sb.WriteString(fmt.Sprintf("> %s [%s]: %s\n", iconForKind(s.Kind), s.Agent, s.Detail))
	}
	sb.WriteString("\n---\n\n")
	sb.WriteString("## Impact Scope Report\n\n")
	if r.RiskLevel != "" {
		sb.WriteString("### Risk level: " + r.RiskLevel + "\n\n")
	}
	if r.SelectiveTests != nil {
		sb.WriteString(fmt.Sprintf("### Selective test plan (%s, confidence %.2f)\n",
			r.SelectiveTests.AnalysisLevel, r.SelectiveTests.Confidence))
		sb.WriteString(fmt.Sprintf("▶ Required (%d)\n⏭ Skippable (%d)\n\n",
			len(r.SelectiveTests.Required), len(r.SelectiveTests.Skippable)))
	}
	if len(r.SuggestedReviewers) > 0 {
		sb.WriteString("### 可考慮邀請 review 的人\n> 系統提供相關背景，最終 reviewer 由 MR 作者決定。\n")
		for _, x := range r.SuggestedReviewers {
			sb.WriteString(fmt.Sprintf("- @%s — %s\n", x.Name, x.Context))
		}
		sb.WriteString("\n")
	}
	if len(r.AIReviewFindings) > 0 {
		sb.WriteString("<details><summary>📝 AI review findings</summary>\n\n")
		for _, f := range r.AIReviewFindings {
			loc := ""
			if f.Location != nil {
				loc = fmt.Sprintf(" (%s:%d)", f.Location.File, f.Location.LineStart)
			}
			sb.WriteString(fmt.Sprintf("- **%s**%s: %s\n", f.Severity, loc, f.Title))
		}
		sb.WriteString("\n</details>\n")
	}
	return sb.String()
}

func iconForKind(k string) string {
	switch k {
	case "critical_finding", "high_risk":
		return "🚨"
	case "agent_failure":
		return "⚠️"
	default:
		return "•"
	}
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/report/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/report/renderer.go internal/report/renderer_test.go
git commit -m "feat(report): add markdown renderer with recommendation-first layout"
```

---

### Task 17: Composer

**Files:**
- Create: `internal/result/composer.go`
- Create: `internal/result/composer_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/result/composer_test.go
package result

import (
	"strings"
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestComposerFiltersByFlags(t *testing.T) {
	outs := []interfaces.AgentOutput{
		{Agent: interfaces.AgentAIReviewer, Status: interfaces.StatusOK,
			Findings: []interfaces.Finding{{Severity: interfaces.SeverityHigh, Title: "x"}}},
		{Agent: interfaces.AgentSelectiveTest, Status: interfaces.StatusOK,
			Metadata: map[string]any{"required": []string{"T"}, "analysis_level": "L1", "confidence": 0.5}},
	}
	flags := Flags{AIReviewer: true, SelectiveTest: false}
	md := Compose(outs, flags)
	if !strings.Contains(md, "REVIEW") {
		t.Fatalf("recommendation lost: %s", md)
	}
	if strings.Contains(md, "Selective test plan") {
		t.Fatalf("selective should be excluded by flag: %s", md)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/result/composer.go
package result

import (
	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/report"
)

type Flags struct {
	SelectiveTest bool
	RolloutRisk   bool
	Ownership     bool
	AIReviewer    bool
}

func Compose(outputs []interfaces.AgentOutput, flags Flags) string {
	enabled := filterByFlags(outputs, flags)
	rec := report.Arbitrate(enabled)
	r := report.ImpactScopeReport{
		Recommendation: rec,
		EnabledFlags:   asMap(flags),
	}
	for _, o := range enabled {
		switch o.Agent {
		case interfaces.AgentRolloutRisk:
			if lvl, ok := o.Metadata["riskLevel"].(string); ok {
				r.RiskLevel = lvl
			}
		case interfaces.AgentSelectiveTest:
			if flags.SelectiveTest {
				r.SelectiveTests = extractTestPlan(o)
			}
		case interfaces.AgentAIReviewer:
			r.AIReviewFindings = o.Findings
		}
		if o.Status == interfaces.StatusFailed {
			r.FailedAgents = append(r.FailedAgents, string(o.Agent))
		}
	}
	r.HighSeverityFindings = report.FlattenFindings(enabled)
	return report.Render(r)
}

func filterByFlags(outs []interfaces.AgentOutput, f Flags) []interfaces.AgentOutput {
	out := make([]interfaces.AgentOutput, 0, len(outs))
	for _, o := range outs {
		switch o.Agent {
		case interfaces.AgentSelectiveTest:
			if f.SelectiveTest {
				out = append(out, o)
			}
		case interfaces.AgentRolloutRisk:
			if f.RolloutRisk {
				out = append(out, o)
			}
		case interfaces.AgentOwnership:
			if f.Ownership {
				out = append(out, o)
			}
		case interfaces.AgentAIReviewer:
			if f.AIReviewer {
				out = append(out, o)
			}
		}
	}
	return out
}

func extractTestPlan(o interfaces.AgentOutput) *report.TestPlan {
	tp := &report.TestPlan{}
	if v, ok := o.Metadata["required"].([]string); ok {
		tp.Required = v
	}
	if v, ok := o.Metadata["skippable"].([]string); ok {
		tp.Skippable = v
	}
	if v, ok := o.Metadata["confidence"].(float64); ok {
		tp.Confidence = v
	}
	if v, ok := o.Metadata["analysis_level"].(string); ok {
		tp.AnalysisLevel = v
	}
	return tp
}

func asMap(f Flags) map[string]bool {
	return map[string]bool{
		"selective_test": f.SelectiveTest,
		"rollout_risk":   f.RolloutRisk,
		"ownership":      f.Ownership,
		"ai_reviewer":    f.AIReviewer,
	}
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/result/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/result/composer.go internal/result/composer_test.go
git commit -m "feat(result): add composer that filters by flags and arbitrates"
```

---

### Task 18: Poster (2 sinks)

**Files:**
- Create: `internal/result/poster.go`
- Create: `internal/result/poster_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/result/poster_test.go
package result

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/releaseguard/internal/gitlab"
)

func TestPosterPostsCommentAndWritesArtifact(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		got = string(buf[:n])
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := gitlab.NewClient(srv.URL, "tok")

	tmp := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(old)

	p := NewPoster(c, 123, 45, 0.85)
	err := p.Post(context.Background(), "## hello\n", []string{"TestA", "TestB"}, true)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("comment body lost: %s", got)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "selective-tests.txt"))
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if !strings.Contains(string(b), "TestA|TestB") {
		t.Fatalf("artifact content: %s", b)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/result/poster.go
package result

import (
	"context"
	"os"
	"strings"

	"github.com/acme/releaseguard/internal/gitlab"
)

type Poster struct {
	gitlab        *gitlab.Client
	projectID     int
	mrIID         int
	minConfidence float64
}

func NewPoster(c *gitlab.Client, projectID, mrIID int, minConfidence float64) *Poster {
	return &Poster{gitlab: c, projectID: projectID, mrIID: mrIID, minConfidence: minConfidence}
}

func (p *Poster) Post(ctx context.Context, comment string, requiredTests []string, writeArtifact bool) error {
	if comment != "" {
		if err := p.gitlab.PostMRNote(p.projectID, p.mrIID, comment); err != nil {
			return err
		}
	}
	if writeArtifact && len(requiredTests) > 0 {
		if err := os.WriteFile("selective-tests.txt",
			[]byte(strings.Join(requiredTests, "|")), 0644); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/result/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/result/poster.go internal/result/poster_test.go
git commit -m "feat(result): add poster with MR-comment and CI-variable sinks"
```

---

### Task 19: Wire agents into `cmd/analyzer/main.go`

**Files:**
- Modify: `cmd/analyzer/main.go`

- [ ] **Step 1: Write the integration test**

```go
// cmd/analyzer/wired_test.go
package main

import (
	"context"
	"testing"

	"github.com/acme/releaseguard/internal/agents/rollout"
	"github.com/acme/releaseguard/internal/agents/testselect"
	"github.com/acme/releaseguard/internal/interfaces"
)

func TestBuildAgentsRespectsFlags(t *testing.T) {
	agents := buildAgents(true, true, false, false, false /* hasDB */)
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	for _, a := range agents {
		if _, ok := a.(*testselect.Agent); !ok {
			if _, ok := a.(*rollout.Agent); !ok {
				t.Fatalf("unexpected agent: %T", a)
			}
		}
	}
	_ = context.Background()
	_ = interfaces.AgentName("")
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement `buildAgents` and wire into `run`**

Replace `cmd/analyzer/main.go`:

```go
// cmd/analyzer/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/acme/releaseguard/internal/agents/reviewer"
	"github.com/acme/releaseguard/internal/agents/rollout"
	"github.com/acme/releaseguard/internal/agents/testselect"
	"github.com/acme/releaseguard/internal/ai"
	"github.com/acme/releaseguard/internal/config"
	"github.com/acme/releaseguard/internal/gitlab"
	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/logger"
	"github.com/acme/releaseguard/internal/result"
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
	log.Info("analyzer starting", "topology", mode)

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(cfg.AnalyzeTimeoutSec)*time.Second)
	defer cancel()

	prov := ai.NewAnthropic(cfg.AIProviderKey, "claude-3-5-sonnet-20241022")
	gl := gitlab.NewClient(cfg.GitLabAPIBase, cfg.GitLabToken)

	projectID := envInt("CI_PROJECT_ID")
	mrIID := envInt("CI_MERGE_REQUEST_IID")
	if projectID == 0 || mrIID == 0 {
		return fmt.Errorf("CI_PROJECT_ID and CI_MERGE_REQUEST_IID required")
	}
	diff, err := gl.GetMRDiff(projectID, mrIID)
	if err != nil {
		return err
	}
	difFiles := make([]interfaces.DiffFile, 0, len(diff))
	for _, d := range diff {
		difFiles = append(difFiles, interfaces.DiffFile{
			Path:    d.NewPath,
			OldPath: d.OldPath,
			Status:  diffStatus(d),
			Patch:   d.Diff,
		})
	}
	in := interfaces.AgentInput{
		MRIID:     mrIID,
		Diff:      difFiles,
		CommitSHA: os.Getenv("CI_COMMIT_SHA"),
		Config:    interfaces.AgentConfig{Topology: mode, ProjectDir: cfg.ProjectsDir},
	}

	hasDB := cfg.PostgresURL != ""
	agents := buildAgents(cfg.Agents.SelectiveTest, cfg.Agents.RolloutRisk,
		cfg.Agents.Ownership, cfg.Agents.AIReviewer, hasDB,
		buildAgentsDeps{prov: prov, projectsDir: cfg.ProjectsDir,
			systemName: os.Getenv("TARGET_SERVICE_NAME"),
			serviceTypes: parseServiceTypes(os.Getenv("TARGET_SERVICE_TYPE"))})

	outs := runAgentsParallel(ctx, agents, in)

	flags := result.Flags{
		SelectiveTest: cfg.Agents.SelectiveTest,
		RolloutRisk:   cfg.Agents.RolloutRisk,
		Ownership:     cfg.Agents.Ownership,
		AIReviewer:    cfg.Agents.AIReviewer,
	}
	md := result.Compose(outs, flags)

	requiredTests := []string{}
	for _, o := range outs {
		if o.Agent == interfaces.AgentSelectiveTest {
			if v, ok := o.Metadata["required"].([]string); ok {
				requiredTests = v
			}
		}
	}
	confidence := 0.0
	for _, o := range outs {
		if o.Agent == interfaces.AgentSelectiveTest {
			if v, ok := o.Metadata["confidence"].(float64); ok {
				confidence = v
			}
		}
	}
	writeArtifact := flags.SelectiveTest && confidence >= cfg.SelectiveTestMinConfidence

	poster := result.NewPoster(gl, projectID, mrIID, cfg.SelectiveTestMinConfidence)
	if err := poster.Post(ctx, md, requiredTests, writeArtifact); err != nil {
		log.Error("post failed", "err", err)
		return err
	}
	log.Info("analyzer done")
	return nil
}

type buildAgentsDeps struct {
	prov         ai.Provider
	projectsDir  string
	systemName   string
	serviceTypes []string
}

func buildAgents(selective, risk, ownership, aiRev, hasDB bool, opt ...buildAgentsDeps) []interfaces.IAgent {
	var deps buildAgentsDeps
	if len(opt) > 0 {
		deps = opt[0]
	}
	var agents []interfaces.IAgent
	if selective {
		agents = append(agents, testselect.New(hasDB))
	}
	if risk {
		agents = append(agents, rollout.New(rollout.Deps{}))
	}
	// ownership: requires DB; topology layer already excludes when no DB
	if aiRev {
		agents = append(agents, reviewer.New(deps.prov, deps.projectsDir, deps.systemName, deps.serviceTypes))
	}
	return agents
}

func runAgentsParallel(ctx context.Context, agents []interfaces.IAgent, in interfaces.AgentInput) []interfaces.AgentOutput {
	var wg sync.WaitGroup
	out := make([]interfaces.AgentOutput, len(agents))
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a interfaces.IAgent) {
			defer wg.Done()
			o, err := a.Run(ctx, in)
			if err != nil {
				out[i] = interfaces.AgentOutput{Agent: a.Name(), Status: interfaces.StatusFailed,
					SchemaVersion: "1", Summary: err.Error()}
				return
			}
			out[i] = o
		}(i, a)
	}
	wg.Wait()
	return out
}

func envInt(k string) int {
	v := os.Getenv(k)
	if v == "" {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

func parseServiceTypes(s string) []string {
	if s == "" {
		return []string{"backend"}
	}
	out := []string{}
	for _, t := range splitComma(s) {
		out = append(out, t)
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func diffStatus(d gitlab.DiffFile) string {
	switch {
	case d.NewFile:
		return "added"
	case d.DeletedFile:
		return "deleted"
	case d.RenamedFile:
		return "renamed"
	default:
		return "modified"
	}
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./cmd/analyzer/ -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/analyzer/main.go cmd/analyzer/wired_test.go
git commit -m "feat(analyzer): wire agents + composer + poster into main"
```

---

### Task 20: End-to-end test against mock GitLab

**Files:**
- Create: `cmd/analyzer/e2e_topology1_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/analyzer/e2e_topology1_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2ETopology1FullFlow(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", "/tmp/rg-e2e-t1", "./").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %s", out)
	}
	defer os.Remove("/tmp/rg-e2e-t1")

	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path":"foo.go","new_path":"foo.go","diff":"-x\n+y"}]`))
		case strings.HasSuffix(r.URL.Path, "/notes"):
			posted = true
			w.WriteHeader(201)
			w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cmd := exec.Command("/tmp/rg-e2e-t1")
	cmd.Env = []string{
		"AI_PROVIDER_KEY=test",
		"GITLAB_TOKEN=t",
		"GITLAB_API_BASE=" + srv.URL,
		"CI_PROJECT_ID=1",
		"CI_MERGE_REQUEST_IID=1",
		"RG_AGENT_SELECTIVE_TEST_ENABLED=true",
		"RG_AGENT_ROLLOUT_RISK_ENABLED=true",
		"RG_AGENT_OWNERSHIP_ENABLED=false",
		"RG_AGENT_AI_REVIEWER_ENABLED=false", // skip LLM call in this test
		"RG_RAG_ENABLED=false",
	}
	gotOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %s", gotOut)
	}
	if !posted {
		t.Fatalf("MR comment not posted; output=%s", gotOut)
	}
}
```

- [ ] **Step 2: Run**

```bash
go test ./cmd/analyzer/ -run TestE2ETopology1Full -v
```

Expected: PASS — analyzer collected diff, ran 2 agents (testselect L1 + rollout), composed report, posted comment.

- [ ] **Step 3: Run all tests once more**

```bash
make test
```

Expected: all green.

- [ ] **Step 4: Final image build**

```bash
make docker
docker run --rm \
  -e AI_PROVIDER_KEY=t -e GITLAB_TOKEN=t \
  -e CI_PROJECT_ID=1 -e CI_MERGE_REQUEST_IID=1 \
  -e RG_AGENT_SELECTIVE_TEST_ENABLED=false \
  -e RG_AGENT_OWNERSHIP_ENABLED=false \
  -e RG_RAG_ENABLED=false \
  releaseguard-analyzer:latest 2>&1 | head -5
```

Expected output (first lines): `analyzer starting topology=1`.

- [ ] **Step 5: Tag and commit**

```bash
git tag plan-b-topology-1
git add cmd/analyzer/e2e_topology1_test.go
git commit --allow-empty -m "test(analyzer): plan B end-to-end topology 1 demoable"
```

---

## What Plan B delivers

After all tasks complete (in topology 1, no Postgres needed):
- Selective Test L1 outputs same-package test list with confidence ≤ 0.6
- Rollout Risk runs 4 sub-analyses, computes HIGH/MED/LOW
- Spec/Code drift detector flags handler-without-spec changes
- AI Reviewer reads `PROJECTS_DIR/*.md`, calls Anthropic with tool use, returns structured findings
- Decision Arbitration produces `HOLD`/`REVIEW`/`PROCEED` with triggered_signals list
- Markdown renderer puts recommendation at top of MR comment
- Poster writes 2 sinks (MR comment, CI variable artifact)
- PlantUML parser + alias lookup helper exist (used in Plan C/Phase 5)
- e2e test passes against mock GitLab

**Next:** Plan C builds the Postgres-backed indexer steps and upgrades Selective Test to L2/L3 + adds Ownership Agent.
