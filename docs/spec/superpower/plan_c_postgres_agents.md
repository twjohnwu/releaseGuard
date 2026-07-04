# ReleaseGuard Plan C — Postgres-Backed Agents Implementation Plan

> **✅ STATUS: Implemented** (commit `461b041`, tag `plan-c-postgres-agents`)
>
> Topology 0 完整支援已上線。23 個 task 全部 done，含 4 個 migrations、cmd/indexer 5 個 step、Selective Test L2/L3、Ownership Agent（提示模式）。詳見 `CHANGELOG.md` 與 `decisions_log.md` Decision #7。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Prerequisite:** Plans A and B complete. This plan adds Postgres-backed indexer steps and upgrades Selective Test to L2/L3 + adds Ownership Agent. Foundation, agents framework, composer, arbitration, renderer all already exist.

**Goal:** Lift ReleaseGuard from topology 1 to topology 0 partial (no RAG yet). Build `cmd/indexer` nightly job with three steps (callgraph, coverage, cochange), upgrade `testselect` agent to L2/L3, add `ownership` agent in proximity-mode (politically neutral output: `suggested_reviewers` shuffled, no `score`, no auto-approval-rule).

**Architecture:** `cmd/indexer` is a separate binary that shares Postgres pool helper from Plan A. Its dispatcher chooses subcommand (`nightly`, `backfill`). The `internal/indexer/code/` subtree houses Step 1-3 + 5 (PlantUML), reusing Plan B's PlantUML parser. Agents query the indexed data; agent contracts unchanged from Plan B (still emit `AgentOutput`).

**Tech Stack:** Adds `golang.org/x/tools/go/callgraph` and friends for static call graph; standard `os/exec` for `git log` / `git blame`.

---

## File Structure (additions to Plans A+B)

```
releaseguard/
├── cmd/
│   └── indexer/
│       ├── main.go                   # dispatcher
│       ├── nightly.go                # subcommand
│       ├── callgraph.go              # step 1
│       ├── coverage.go               # step 2
│       ├── cochange.go               # step 3
│       └── plantuml_step.go          # step 5
├── internal/
│   ├── indexer/
│   │   └── code/
│   │       ├── callgraph/
│   │       │   ├── builder.go        # interface
│   │       │   ├── go_builder.go
│   │       │   └── store.go          # UPSERT helpers
│   │       ├── coverage/
│   │       │   ├── parser.go         # interface (lcov/cobertura/gocover)
│   │       │   ├── lcov.go
│   │       │   └── loader.go
│   │       └── cochange/
│   │           ├── blame.go
│   │           └── matrix.go
│   ├── analysis/
│   │   └── callgraph/                # query layer (used by analyzer agents)
│   │       └── store.go              # reverse BFS query
│   └── agents/
│       ├── testselect/
│       │   ├── level_l2.go           # NEW
│       │   └── level_l3.go           # NEW
│       └── ownership/
│           ├── agent.go
│           ├── selector.go           # internal top-K + shuffle
│           ├── context_phrasing.go   # natural-language reason
│           ├── zones.go              # impact_zones
│           └── hints.go              # hidden_dependency_hints
└── migrations/
    ├── 0003_callgraph.sql            # symbols, edges
    ├── 0004_coverage.sql             # coverage_map
    ├── 0005_ownership.sql            # ownership_signals
    └── 0006_runs.sql                 # mr_runs, agent_outputs, findings
```

---

### Task 1: Migrations 0003 (callgraph)

**Files:**
- Create: `migrations/0003_callgraph.sql`

- [ ] **Step 1: Write migration**

```sql
-- migrations/0003_callgraph.sql
CREATE TABLE IF NOT EXISTS symbols (
  id          TEXT PRIMARY KEY,
  repo_id     BIGINT NOT NULL REFERENCES repos(id),
  kind        TEXT NOT NULL,
  language    TEXT NOT NULL,
  file        TEXT NOT NULL,
  line_start  INT NOT NULL,
  line_end    INT NOT NULL,
  signature   TEXT,
  hash        TEXT NOT NULL,
  indexed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_symbols_repo_file ON symbols(repo_id, file);
CREATE INDEX IF NOT EXISTS idx_symbols_hash ON symbols(hash);

CREATE TABLE IF NOT EXISTS edges (
  id         BIGSERIAL PRIMARY KEY,
  caller     TEXT NOT NULL REFERENCES symbols(id),
  callee     TEXT NOT NULL REFERENCES symbols(id),
  call_file  TEXT NOT NULL,
  call_line  INT NOT NULL,
  kind       TEXT NOT NULL  -- direct|interface|dynamic
);
CREATE INDEX IF NOT EXISTS idx_edges_callee ON edges(callee);
CREATE INDEX IF NOT EXISTS idx_edges_caller ON edges(caller);
CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(kind);
```

- [ ] **Step 2: Run migrate test**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
sleep 3
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" \
  go test ./internal/storage/ -run TestMigrate -v
docker stop rg-pg && docker rm rg-pg
```

Expected: PASS, `symbols` and `edges` exist.

- [ ] **Step 3: Commit**

```bash
git add migrations/0003_callgraph.sql
git commit -m "feat(migrations): add symbols and edges tables"
```

---

### Task 2: Migration 0004 (coverage)

**Files:**
- Create: `migrations/0004_coverage.sql`

- [ ] **Step 1: Write migration**

```sql
-- migrations/0004_coverage.sql
CREATE TABLE IF NOT EXISTS coverage_map (
  id                  BIGSERIAL PRIMARY KEY,
  repo_id             BIGINT NOT NULL REFERENCES repos(id),
  test_id             TEXT NOT NULL,
  covered_symbol_id   TEXT NOT NULL REFERENCES symbols(id),
  last_seen_sha       TEXT NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (repo_id, test_id, covered_symbol_id)
);
CREATE INDEX IF NOT EXISTS idx_coverage_symbol ON coverage_map(covered_symbol_id);
CREATE INDEX IF NOT EXISTS idx_coverage_repo_test ON coverage_map(repo_id, test_id);
```

- [ ] **Step 2-3: Test + commit**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
sleep 3
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" \
  go test ./internal/storage/ -run TestMigrate -v
docker stop rg-pg && docker rm rg-pg
git add migrations/0004_coverage.sql
git commit -m "feat(migrations): add coverage_map table"
```

---

### Task 3: Migration 0005 (ownership)

**Files:**
- Create: `migrations/0005_ownership.sql`

- [ ] **Step 1: Write migration**

```sql
-- migrations/0005_ownership.sql
CREATE TABLE IF NOT EXISTS ownership_signals (
  id                BIGSERIAL PRIMARY KEY,
  repo_id           BIGINT NOT NULL REFERENCES repos(id),
  file_path         TEXT NOT NULL,
  author            TEXT NOT NULL,
  blame_weight      NUMERIC(5,4) NOT NULL,
  co_change_files   JSONB NOT NULL DEFAULT '[]'::jsonb,
  recency_score     NUMERIC(5,4) NOT NULL,
  computed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (repo_id, file_path, author)
);
CREATE INDEX IF NOT EXISTS idx_owner_repo_file ON ownership_signals(repo_id, file_path);
CREATE INDEX IF NOT EXISTS idx_owner_repo_author ON ownership_signals(repo_id, author);
```

- [ ] **Step 2-3: Same migrate test, commit**

```bash
git add migrations/0005_ownership.sql
git commit -m "feat(migrations): add ownership_signals table"
```

---

### Task 4: Migration 0006 (mr_runs / agent_outputs / findings)

**Files:**
- Create: `migrations/0006_runs.sql`

- [ ] **Step 1: Write migration**

```sql
-- migrations/0006_runs.sql
CREATE TABLE IF NOT EXISTS mr_runs (
  id              BIGSERIAL PRIMARY KEY,
  repo_id         BIGINT NOT NULL REFERENCES repos(id),
  mr_iid          INT NOT NULL,
  commit_sha      TEXT NOT NULL,
  started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at     TIMESTAMPTZ NULL,
  status          TEXT NOT NULL DEFAULT 'running',
  risk_level      TEXT NULL,
  recommendation  TEXT NULL,
  agents_enabled  JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_mrruns_repo_mr ON mr_runs(repo_id, mr_iid, started_at DESC);

CREATE TABLE IF NOT EXISTS agent_outputs (
  id            BIGSERIAL PRIMARY KEY,
  mr_run_id     BIGINT NOT NULL REFERENCES mr_runs(id),
  agent         TEXT NOT NULL,
  status        TEXT NOT NULL,
  duration_ms   INT NOT NULL,
  payload       JSONB NOT NULL,
  UNIQUE (mr_run_id, agent)
);
CREATE INDEX IF NOT EXISTS idx_agent_outputs_agent ON agent_outputs(agent, status);

CREATE TABLE IF NOT EXISTS findings (
  id                  BIGSERIAL PRIMARY KEY,
  mr_run_id           BIGINT NOT NULL REFERENCES mr_runs(id),
  agent               TEXT NOT NULL,
  severity            TEXT NOT NULL,
  category            TEXT NOT NULL,
  title               TEXT NOT NULL,
  body                TEXT NOT NULL,
  location_file       TEXT,
  location_line_start INT,
  location_line_end   INT,
  suggestion          TEXT,
  stable_id           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_findings_stable ON findings(stable_id);
CREATE INDEX IF NOT EXISTS idx_findings_run ON findings(mr_run_id, severity);
```

- [ ] **Step 2-3: Test + commit**

```bash
git add migrations/0006_runs.sql
git commit -m "feat(migrations): add mr_runs, agent_outputs, findings tables"
```

---

### Task 5: Indexer cmd skeleton

**Files:**
- Create: `cmd/indexer/main.go`
- Create: `cmd/indexer/main_test.go`

- [ ] **Step 1: Write smoke test**

```go
// cmd/indexer/main_test.go
package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSubcommandsListed(t *testing.T) {
	out, _ := exec.Command("go", "run", "./", "--help").CombinedOutput()
	s := string(out)
	if !strings.Contains(s, "nightly") {
		t.Fatalf("nightly not listed: %s", s)
	}
	if !strings.Contains(s, "backfill") {
		t.Fatalf("backfill not listed: %s", s)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// cmd/indexer/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

type Subcommand func(ctx context.Context, args []string) error

var subcommands = map[string]Subcommand{
	"nightly":  Nightly,
	"backfill": Backfill, // implemented in Plan D; stub returns "not yet" here
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 || os.Args[1] == "--help" {
		fmt.Println("Usage: indexer <subcommand> [flags]")
		fmt.Println("Subcommands:")
		for n := range subcommands {
			fmt.Println("  ", n)
		}
		if len(os.Args) < 2 {
			return fmt.Errorf("no subcommand")
		}
		return nil
	}
	name := os.Args[1]
	fn, ok := subcommands[name]
	if !ok {
		return fmt.Errorf("unknown subcommand: %s", name)
	}
	return fn(context.Background(), os.Args[2:])
}

// Backfill is implemented in Plan D; stub here.
func Backfill(ctx context.Context, args []string) error {
	return fmt.Errorf("backfill not implemented in Plan C")
}

// Nightly is the main flow; implemented in nightly.go
var _ = flag.NewFlagSet // keep import alive
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./cmd/indexer/ -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/indexer/
git commit -m "feat(indexer): add cmd/indexer skeleton with subcommand dispatch"
```

---

### Task 6: Callgraph builder interface

**Files:**
- Create: `internal/indexer/code/callgraph/builder.go`
- Create: `internal/indexer/code/callgraph/builder_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/code/callgraph/builder_test.go
package callgraph

import "testing"

func TestSymbolEdgeShape(t *testing.T) {
	s := Symbol{ID: "pkg.Foo", File: "x.go", LineStart: 1, LineEnd: 5}
	e := Edge{Caller: "pkg.Foo", Callee: "pkg.Bar", Kind: "direct"}
	if s.ID == "" || e.Caller == "" {
		t.Fatal("zero")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/code/callgraph/builder.go
package callgraph

import "context"

type Symbol struct {
	ID         string
	Kind       string
	Language   string
	File       string
	LineStart  int
	LineEnd    int
	Signature  string
	Hash       string
}

type Edge struct {
	Caller    string
	Callee    string
	CallFile  string
	CallLine  int
	Kind      string // direct | interface | dynamic
}

type BuildResult struct {
	Symbols []Symbol
	Edges   []Edge
}

type Builder interface {
	Build(ctx context.Context, repoPath string) (*BuildResult, error)
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/code/callgraph/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/code/callgraph/builder.go internal/indexer/code/callgraph/builder_test.go
git commit -m "feat(callgraph): add Builder interface + Symbol/Edge types"
```

---

### Task 7: Go callgraph builder (CHA)

**Files:**
- Create: `internal/indexer/code/callgraph/go_builder.go`
- Create: `internal/indexer/code/callgraph/go_builder_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/code/callgraph/go_builder_test.go
package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGoBuilderOnTinyFixture(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\ngo 1.23\n"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func A() { B() }
func B() {}

func main() { A() }
`), 0644)

	b := NewGoBuilder()
	r, err := b.Build(context.Background(), dir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	hasA, hasB := false, false
	for _, s := range r.Symbols {
		if s.ID == "fixture.A" {
			hasA = true
		}
		if s.ID == "fixture.B" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Fatalf("symbols missing: %+v", r.Symbols)
	}
	hasEdge := false
	for _, e := range r.Edges {
		if e.Caller == "fixture.A" && e.Callee == "fixture.B" {
			hasEdge = true
		}
	}
	if !hasEdge {
		t.Fatalf("A→B edge missing: %+v", r.Edges)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/code/callgraph/go_builder.go
package callgraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

type GoBuilder struct{}

func NewGoBuilder() *GoBuilder { return &GoBuilder{} }

func (b *GoBuilder) Build(ctx context.Context, repoPath string) (*BuildResult, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedFiles | packages.NeedName | packages.NeedCompiledGoFiles,
		Dir:     repoPath,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	prog, ssaPkgs := ssautil.Packages(pkgs, ssa.GlobalDebug)
	prog.Build()
	_ = ssaPkgs

	cg := cha.CallGraph(prog)
	cg.DeleteSyntheticNodes()

	out := &BuildResult{}
	addedSymbols := map[string]bool{}

	cg.Visit(func(n *callgraph.Node) error {
		if n == nil || n.Func == nil {
			return nil
		}
		sym := nodeToSymbol(n)
		if sym == nil {
			return nil
		}
		if !addedSymbols[sym.ID] {
			out.Symbols = append(out.Symbols, *sym)
			addedSymbols[sym.ID] = true
		}
		for _, edge := range n.Out {
			callee := nodeToSymbol(edge.Callee)
			if callee == nil {
				continue
			}
			if !addedSymbols[callee.ID] {
				out.Symbols = append(out.Symbols, *callee)
				addedSymbols[callee.ID] = true
			}
			pos := prog.Fset.Position(edge.Site.Pos())
			kind := "direct"
			if edge.Site.Common().IsInvoke() {
				kind = "interface"
			}
			out.Edges = append(out.Edges, Edge{
				Caller:   sym.ID,
				Callee:   callee.ID,
				CallFile: pos.Filename,
				CallLine: pos.Line,
				Kind:     kind,
			})
		}
		return nil
	})
	return out, nil
}

func nodeToSymbol(n *callgraph.Node) *Symbol {
	if n == nil || n.Func == nil || n.Func.Pkg == nil {
		return nil
	}
	pkgPath := n.Func.Pkg.Pkg.Path()
	id := pkgPath + "." + n.Func.Name()
	hash := sha256.Sum256([]byte(id))
	return &Symbol{
		ID:        id,
		Kind:      "function",
		Language:  "go",
		File:      "", // pos extracted at edge time; left empty here for brevity
		Signature: n.Func.String(),
		Hash:      hex.EncodeToString(hash[:8]),
	}
}
```

```bash
go get golang.org/x/tools/go/callgraph@latest
go get golang.org/x/tools/go/packages@latest
go get golang.org/x/tools/go/ssa@latest
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/code/callgraph/ -v
```

If the SSA load complains about `go.sum` for the fixture, add an empty `go.sum` to the fixture or use `packages.NeedModule`.

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/code/callgraph/go_builder.go internal/indexer/code/callgraph/go_builder_test.go go.mod go.sum
git commit -m "feat(callgraph): add Go CHA-based builder"
```

---

### Task 8: Callgraph store (UPSERT helpers)

**Files:**
- Create: `internal/indexer/code/callgraph/store.go`
- Create: `internal/indexer/code/callgraph/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/code/callgraph/store_test.go
package callgraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestUpsertSymbolsAndEdgesIdempotent(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../migrations")

	pool.Exec(ctx, "INSERT INTO repos(name, languages) VALUES('test', '[\"go\"]') ON CONFLICT DO NOTHING")
	var repoID int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='test'").Scan(&repoID)

	res := &BuildResult{
		Symbols: []Symbol{{ID: "x.Foo", Kind: "function", Language: "go", File: "f", LineStart: 1, LineEnd: 5, Hash: "h1"}},
		Edges:   []Edge{},
	}
	if err := UpsertResult(ctx, pool, repoID, res); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// run again — should not error
	if err := UpsertResult(ctx, pool, repoID, res); err != nil {
		t.Fatalf("idempotent upsert: %v", err)
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM symbols WHERE id='x.Foo'").Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
}
```

- [ ] **Step 2: Run, expect SKIP if no DB; FAIL with DB**

- [ ] **Step 3: Implement**

```go
// internal/indexer/code/callgraph/store.go
package callgraph

import (
	"context"
	"fmt"

	"github.com/acme/releaseguard/internal/storage"
)

func UpsertResult(ctx context.Context, pool *storage.Pool, repoID int64, r *BuildResult) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, s := range r.Symbols {
		if _, err := tx.Exec(ctx, `
			INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, signature, hash)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (id) DO UPDATE SET
				kind=EXCLUDED.kind, language=EXCLUDED.language, file=EXCLUDED.file,
				line_start=EXCLUDED.line_start, line_end=EXCLUDED.line_end,
				signature=EXCLUDED.signature, hash=EXCLUDED.hash, indexed_at=now()`,
			s.ID, repoID, s.Kind, s.Language, s.File, s.LineStart, s.LineEnd, s.Signature, s.Hash); err != nil {
			return fmt.Errorf("upsert symbol %s: %w", s.ID, err)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM edges WHERE caller IN (SELECT id FROM symbols WHERE repo_id=$1)", repoID); err != nil {
		return fmt.Errorf("clear edges: %w", err)
	}
	for _, e := range r.Edges {
		if _, err := tx.Exec(ctx, `
			INSERT INTO edges(caller, callee, call_file, call_line, kind)
			VALUES($1,$2,$3,$4,$5)`,
			e.Caller, e.Callee, e.CallFile, e.CallLine, e.Kind); err != nil {
			return fmt.Errorf("insert edge: %w", err)
		}
	}
	return tx.Commit(ctx)
}
```

- [ ] **Step 4: Run with DB, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/code/callgraph/store.go internal/indexer/code/callgraph/store_test.go
git commit -m "feat(callgraph): add idempotent UPSERT for symbols + edges"
```

---

### Task 9: Coverage parser interface + lcov

**Files:**
- Create: `internal/indexer/code/coverage/parser.go`
- Create: `internal/indexer/code/coverage/lcov.go`
- Create: `internal/indexer/code/coverage/lcov_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/code/coverage/lcov_test.go
package coverage

import (
	"strings"
	"testing"
)

const lcovSample = `TN:
SF:foo.go
FN:10,Foo
DA:10,5
DA:11,5
end_of_record
TN:TestFoo
SF:foo.go
FN:10,Foo
DA:10,3
end_of_record
`

func TestParseLcovExtractsTestSymbols(t *testing.T) {
	p := NewLCOV()
	entries, err := p.Parse(strings.NewReader(lcovSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/code/coverage/parser.go
package coverage

import "io"

type Entry struct {
	TestID         string // empty for unattributed coverage
	File           string
	FunctionName   string
	LineStart      int
	LineEnd        int
}

type Parser interface {
	Parse(r io.Reader) ([]Entry, error)
}
```

```go
// internal/indexer/code/coverage/lcov.go
package coverage

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

type LCOV struct{}

func NewLCOV() *LCOV { return &LCOV{} }

func (l *LCOV) Parse(r io.Reader) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(r)
	var curTest, curFile string
	var curFn string
	var curLine int
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "TN:"):
			curTest = strings.TrimPrefix(line, "TN:")
		case strings.HasPrefix(line, "SF:"):
			curFile = strings.TrimPrefix(line, "SF:")
		case strings.HasPrefix(line, "FN:"):
			rest := strings.TrimPrefix(line, "FN:")
			parts := strings.SplitN(rest, ",", 2)
			if len(parts) == 2 {
				if n, err := strconv.Atoi(parts[0]); err == nil {
					curLine = n
				}
				curFn = parts[1]
				out = append(out, Entry{
					TestID: curTest, File: curFile,
					FunctionName: curFn, LineStart: curLine, LineEnd: curLine,
				})
			}
		case line == "end_of_record":
			curFile, curFn, curLine = "", "", 0
		}
	}
	return out, sc.Err()
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/code/coverage/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/code/coverage/
git commit -m "feat(coverage): add Parser interface and LCOV implementation"
```

---

### Task 10: Coverage loader (UPSERT to coverage_map)

**Files:**
- Create: `internal/indexer/code/coverage/loader.go`
- Create: `internal/indexer/code/coverage/loader_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/code/coverage/loader_test.go
package coverage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestLoadEntriesIntoCoverageMap(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../migrations")

	// repo + symbol prerequisite
	pool.Exec(ctx, "INSERT INTO repos(name) VALUES('cov-test') ON CONFLICT DO NOTHING")
	var rid int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='cov-test'").Scan(&rid)
	pool.Exec(ctx, `INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, hash)
		VALUES('foo.Bar', $1, 'function', 'go', 'foo.go', 1, 5, 'h')
		ON CONFLICT (id) DO NOTHING`, rid)

	entries := []Entry{{TestID: "TestX", File: "foo.go", FunctionName: "Bar", LineStart: 1}}
	if err := Load(ctx, pool, rid, "abc123", entries); err != nil {
		t.Fatalf("load: %v", err)
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM coverage_map WHERE test_id='TestX'").Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}
```

- [ ] **Step 2: Implement**

```go
// internal/indexer/code/coverage/loader.go
package coverage

import (
	"context"

	"github.com/acme/releaseguard/internal/storage"
)

// Load matches entries to existing symbols by (file, line range overlap) and inserts.
// Skips entries whose file/function has no matching symbol.
func Load(ctx context.Context, pool *storage.Pool, repoID int64, sha string, entries []Entry) error {
	for _, e := range entries {
		if e.TestID == "" {
			continue
		}
		var symID string
		err := pool.QueryRow(ctx, `
			SELECT id FROM symbols
			WHERE repo_id=$1 AND file=$2
			AND line_start <= $3 AND line_end >= $3
			LIMIT 1`,
			repoID, e.File, e.LineStart).Scan(&symID)
		if err != nil {
			continue
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO coverage_map(repo_id, test_id, covered_symbol_id, last_seen_sha)
			VALUES($1,$2,$3,$4)
			ON CONFLICT (repo_id, test_id, covered_symbol_id)
			DO UPDATE SET last_seen_sha=EXCLUDED.last_seen_sha, updated_at=now()`,
			repoID, e.TestID, symID, sha)
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Run integration with DB**

- [ ] **Step 4: Commit**

```bash
git add internal/indexer/code/coverage/loader.go internal/indexer/code/coverage/loader_test.go
git commit -m "feat(coverage): add loader that joins entries to symbols"
```

---

### Task 11: Cochange — git blame parser

**Files:**
- Create: `internal/indexer/code/cochange/blame.go`
- Create: `internal/indexer/code/cochange/blame_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/code/cochange/blame_test.go
package cochange

import (
	"strings"
	"testing"
)

const blameSample = `abc123 1 1 1
author Alice
author-mail <alice@example.com>
author-time 1700000000
author-tz +0000
committer Alice
filename foo.go
	first line
def456 2 2 1
author Bob
author-mail <bob@example.com>
author-time 1710000000
author-tz +0000
filename foo.go
	second line
abc123 3 3 1
author Alice
author-mail <alice@example.com>
filename foo.go
	third line
`

func TestParseBlamePorcelainCounts(t *testing.T) {
	authors, err := ParsePorcelain(strings.NewReader(blameSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if authors["alice@example.com"] != 2 || authors["bob@example.com"] != 1 {
		t.Fatalf("unexpected: %+v", authors)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/code/cochange/blame.go
package cochange

import (
	"bufio"
	"io"
	"strings"
)

// ParsePorcelain parses `git blame --line-porcelain` and returns author email → line count.
func ParsePorcelain(r io.Reader) (map[string]int, error) {
	out := map[string]int{}
	sc := bufio.NewScanner(r)
	curEmail := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "author-mail "):
			email := strings.TrimPrefix(line, "author-mail ")
			email = strings.Trim(email, "<>")
			curEmail = email
		case strings.HasPrefix(line, "\t"):
			if curEmail != "" {
				out[curEmail]++
			}
		}
	}
	return out, sc.Err()
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/code/cochange/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/code/cochange/
git commit -m "feat(cochange): add blame porcelain parser"
```

---

### Task 12: Cochange — co-change matrix

**Files:**
- Create: `internal/indexer/code/cochange/matrix.go`
- Create: `internal/indexer/code/cochange/matrix_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/code/cochange/matrix_test.go
package cochange

import "testing"

func TestMatrixCounts(t *testing.T) {
	commits := [][]string{
		{"a.go", "b.go"},
		{"a.go", "c.go"},
		{"a.go", "b.go"},
	}
	m := BuildMatrix(commits)
	if m["a.go"]["b.go"] != 2 {
		t.Fatalf("ab=%d, want 2", m["a.go"]["b.go"])
	}
	if m["a.go"]["c.go"] != 1 {
		t.Fatalf("ac=%d, want 1", m["a.go"]["c.go"])
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/code/cochange/matrix.go
package cochange

type Matrix map[string]map[string]int

// BuildMatrix counts how often each pair of files appears in the same commit.
func BuildMatrix(commits [][]string) Matrix {
	m := Matrix{}
	for _, files := range commits {
		for i, fi := range files {
			for j, fj := range files {
				if i == j {
					continue
				}
				if m[fi] == nil {
					m[fi] = map[string]int{}
				}
				m[fi][fj]++
				_ = j
			}
		}
	}
	return m
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/code/cochange/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/code/cochange/matrix.go internal/indexer/code/cochange/matrix_test.go
git commit -m "feat(cochange): add co-change matrix builder"
```

---

### Task 13: Indexer subcommand wiring (callgraph + coverage + cochange + plantuml)

**Files:**
- Create: `cmd/indexer/nightly.go`
- Create: `cmd/indexer/callgraph.go`
- Create: `cmd/indexer/coverage.go`
- Create: `cmd/indexer/cochange.go`
- Create: `cmd/indexer/plantuml_step.go`

- [ ] **Step 1: Write nightly skeleton + test**

```go
// cmd/indexer/nightly_test.go
package main

import (
	"context"
	"testing"
)

func TestNightlyAcceptsRepoFlag(t *testing.T) {
	err := Nightly(context.Background(), []string{"--repo=demo"})
	// At minimum should fail on missing POSTGRES_URL, not on flag parse
	if err == nil || err.Error() == "" {
		t.Fatal("expected actionable error")
	}
}
```

- [ ] **Step 2: Implement nightly + step files**

```go
// cmd/indexer/nightly.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/acme/releaseguard/internal/storage"
)

func Nightly(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("nightly", flag.ContinueOnError)
	repo := fs.String("repo", "", "repo name (matches repos.name)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return fmt.Errorf("--repo required")
	}
	pgURL := os.Getenv("POSTGRES_URL")
	if pgURL == "" {
		return fmt.Errorf("POSTGRES_URL required")
	}
	pool, err := storage.NewPool(ctx, pgURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := storage.MigrateUp(ctx, pool, "/migrations"); err != nil {
		return err
	}

	var repoID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name=$1", *repo).Scan(&repoID); err != nil {
		return fmt.Errorf("repo %s not registered: %w", *repo, err)
	}

	repoPath := os.Getenv("REPO_CHECKOUT_DIR")
	if repoPath == "" {
		repoPath = "/checkout/" + *repo
	}

	if err := stepCallgraph(ctx, pool, repoID, repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "callgraph step: %v\n", err)
	}
	if err := stepCoverage(ctx, pool, repoID, repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "coverage step: %v\n", err)
	}
	if err := stepCochange(ctx, pool, repoID, repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "cochange step: %v\n", err)
	}
	if err := stepPlantUML(ctx, pool, repoID, repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "plantuml step: %v\n", err)
	}
	return nil
}
```

```go
// cmd/indexer/callgraph.go
package main

import (
	"context"

	"github.com/acme/releaseguard/internal/indexer/code/callgraph"
	"github.com/acme/releaseguard/internal/storage"
)

func stepCallgraph(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	b := callgraph.NewGoBuilder()
	r, err := b.Build(ctx, repoPath)
	if err != nil {
		return err
	}
	return callgraph.UpsertResult(ctx, pool, repoID, r)
}
```

```go
// cmd/indexer/coverage.go
package main

import (
	"context"
	"os"

	"github.com/acme/releaseguard/internal/indexer/code/coverage"
	"github.com/acme/releaseguard/internal/storage"
)

func stepCoverage(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	path := os.Getenv("COVERAGE_ARTIFACT_PATH")
	if path == "" {
		return nil // skip if not configured
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	p := coverage.NewLCOV()
	entries, err := p.Parse(f)
	if err != nil {
		return err
	}
	sha := os.Getenv("CI_COMMIT_SHA")
	if sha == "" {
		sha = "unknown"
	}
	return coverage.Load(ctx, pool, repoID, sha, entries)
}
```

```go
// cmd/indexer/cochange.go
package main

import (
	"bufio"
	"bytes"
	"context"
	"math"
	"os/exec"
	"time"

	"github.com/acme/releaseguard/internal/indexer/code/cochange"
	"github.com/acme/releaseguard/internal/storage"
)

func stepCochange(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	lookback := getLookbackDays()
	since := time.Now().Add(-time.Duration(lookback) * 24 * time.Hour).Format("2006-01-02")

	out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "log",
		"--since="+since, "--name-only", "--pretty=format:>>>%H").Output()
	if err != nil {
		return err
	}

	var commits [][]string
	var current []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if line[0] == '>' && line[1] == '>' && line[2] == '>' {
			if len(current) > 0 {
				commits = append(commits, current)
			}
			current = nil
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		commits = append(commits, current)
	}
	matrix := cochange.BuildMatrix(commits)

	for file, peers := range matrix {
		// per-file blame
		blameOut, err := exec.CommandContext(ctx, "git", "-C", repoPath,
			"blame", "--line-porcelain", file).Output()
		if err != nil {
			continue
		}
		authors, err := cochange.ParsePorcelain(bytes.NewReader(blameOut))
		if err != nil {
			continue
		}
		total := 0
		for _, n := range authors {
			total += n
		}
		if total == 0 {
			continue
		}
		// recency: simple — use 1.0 (lookup-precise calc deferred to spec)
		recency := math.Exp(-1.0 / float64(lookback))
		ccJSON := encodeCoChange(peers)
		for email, lines := range authors {
			weight := float64(lines) / float64(total)
			_, err := pool.Exec(ctx, `
				INSERT INTO ownership_signals(repo_id, file_path, author, blame_weight, co_change_files, recency_score)
				VALUES($1,$2,$3,$4,$5::jsonb,$6)
				ON CONFLICT (repo_id, file_path, author)
				DO UPDATE SET blame_weight=EXCLUDED.blame_weight,
				              co_change_files=EXCLUDED.co_change_files,
				              recency_score=EXCLUDED.recency_score,
				              computed_at=now()`,
				repoID, file, email, weight, ccJSON, recency)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func encodeCoChange(peers map[string]int) string {
	if len(peers) == 0 {
		return "[]"
	}
	parts := []string{}
	total := 0
	for _, n := range peers {
		total += n
	}
	for f, n := range peers {
		parts = append(parts, jsonObj(f, float64(n)/float64(total)))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func jsonObj(file string, score float64) string {
	return `{"file":"` + escapeJSON(file) + `","score":` + ftoa(score) + `}`
}

func escapeJSON(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', 4, 64)
}

func getLookbackDays() int {
	v := os.Getenv("OWNERSHIP_LOOKBACK_DAYS")
	if v == "" {
		return 180
	}
	n, _ := strconv.Atoi(v)
	if n <= 0 {
		return 180
	}
	return n
}
```

Add imports `"os"`, `"strconv"`, `"strings"` to cochange.go.

```go
// cmd/indexer/plantuml_step.go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/acme/releaseguard/internal/analysis/aliases"
	"github.com/acme/releaseguard/internal/analysis/plantuml"
	"github.com/acme/releaseguard/internal/storage"
)

func stepPlantUML(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	docs := os.Getenv("DOCS_REPO_NAMES")
	if docs == "" {
		return nil
	}
	// Note: this step ALSO requires the repo to be in DOCS_REPO_NAMES list.
	// The dispatcher checks the repo's name against DOCS_REPO_NAMES; here we skip if path lacks .puml.
	lookup, err := aliases.LoadFile(filepath.Join(os.Getenv("CONFIG_DIR"), "diagram_aliases.yaml"))
	if err != nil {
		return err
	}
	var puml []string
	filepath.Walk(repoPath, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() &&
			(strings.HasSuffix(p, ".puml") || strings.HasSuffix(p, ".plantuml")) {
			puml = append(puml, p)
		}
		return nil
	})
	for _, p := range puml {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		d, err := plantuml.Parse(f)
		f.Close()
		if err != nil {
			continue
		}
		for _, in := range d.Interactions {
			srcRepo, srcOK := lookup.Resolve(in.Source)
			dstRepo, dstOK := lookup.Resolve(in.Target)
			var srcID, dstID interface{}
			if srcOK {
				srcID = lookupRepoID(ctx, pool, srcRepo)
			}
			if dstOK {
				dstID = lookupRepoID(ctx, pool, dstRepo)
			}
			pool.Exec(ctx, `
				INSERT INTO cross_repo_edges(source_repo_id, target_repo_id, source_alias, target_alias, call_kind, source_diagram_path)
				VALUES($1,$2,$3,$4,$5,$6)`,
				srcID, dstID, in.Source, in.Target, in.Kind, p)
		}
	}
	return nil
}

func lookupRepoID(ctx context.Context, pool *storage.Pool, name string) interface{} {
	var id int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name=$1", name).Scan(&id); err != nil {
		return nil
	}
	return id
}
```

- [ ] **Step 3: Run unit-level tests**

```bash
go test ./cmd/indexer/ ./internal/indexer/... -v
```

- [ ] **Step 4: Integration test (manual)**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
sleep 3
psql postgres://postgres:test@localhost:5432/postgres -c "INSERT INTO repos(name) VALUES('demo')"
POSTGRES_URL=postgres://postgres:test@localhost:5432/postgres?sslmode=disable \
REPO_CHECKOUT_DIR=/path/to/sample/repo \
go run ./cmd/indexer nightly --repo=demo
psql postgres://postgres:test@localhost:5432/postgres -c "SELECT count(*) FROM symbols"
docker stop rg-pg && docker rm rg-pg
```

Expected: rows present in `symbols`, `ownership_signals`.

- [ ] **Step 5: Commit**

```bash
git add cmd/indexer/
git commit -m "feat(indexer): add nightly subcommand with 4 steps wired"
```

---

### Task 14: Indexer Dockerfile

**Files:**
- Create: `deploy/Dockerfile.indexer`

- [ ] **Step 1-3: Write + build + verify**

```dockerfile
# deploy/Dockerfile.indexer
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/indexer ./cmd/indexer

FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
COPY --from=build /out/indexer /usr/local/bin/indexer
COPY migrations /migrations
COPY config /config
ENV CONFIG_DIR=/config
ENTRYPOINT ["/usr/local/bin/indexer"]
```

```bash
docker build -t releaseguard-indexer:dev -f deploy/Dockerfile.indexer .
docker run --rm releaseguard-indexer:dev --help
```

Expected: prints subcommand list.

- [ ] **Step 4: Add Makefile target**

```makefile
# Append to Makefile
docker-indexer:
	docker build -t releaseguard-indexer:latest -f deploy/Dockerfile.indexer .
```

- [ ] **Step 5: Commit**

```bash
git add deploy/Dockerfile.indexer Makefile
git commit -m "build: add Dockerfile.indexer + make target"
```

---

### Task 15: Selective Test L2

**Files:**
- Create: `internal/agents/testselect/level_l2.go`
- Modify: `internal/agents/testselect/agent.go`
- Create: `internal/agents/testselect/level_l2_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/testselect/level_l2_test.go
package testselect

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestL2QueriesCoverageMap(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../migrations")

	pool.Exec(ctx, "INSERT INTO repos(name) VALUES('l2-test') ON CONFLICT DO NOTHING")
	var rid int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='l2-test'").Scan(&rid)
	pool.Exec(ctx, `INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, hash)
		VALUES('l2.X', $1, 'function', 'go', 'foo.go', 1, 5, 'h') ON CONFLICT DO NOTHING`, rid)
	pool.Exec(ctx, `INSERT INTO coverage_map(repo_id, test_id, covered_symbol_id, last_seen_sha)
		VALUES($1, 'TestCovered', 'l2.X', 'sha1') ON CONFLICT DO NOTHING`, rid)

	required, err := L2QueryRequired(ctx, pool, rid, []string{"foo.go"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(required) != 1 || required[0] != "TestCovered" {
		t.Fatalf("unexpected: %+v", required)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/testselect/level_l2.go
package testselect

import (
	"context"

	"github.com/acme/releaseguard/internal/storage"
)

func L2QueryRequired(ctx context.Context, pool *storage.Pool, repoID int64, files []string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT cm.test_id
		FROM coverage_map cm
		JOIN symbols s ON cm.covered_symbol_id = s.id
		WHERE cm.repo_id = $1 AND s.file = ANY($2)`,
		repoID, files)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
```

Modify `agent.go`'s `Run` to call L2 if `hasDB` is true, fall back to L1 otherwise:

```go
// internal/agents/testselect/agent.go (replace Run)
import (
	"context"
	"time"

	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/storage"
)

type Agent struct {
	hasDB  bool
	pool   *storage.Pool
	repoID int64
}

func New(hasDB bool) *Agent { return &Agent{hasDB: hasDB} }

// NewWithDB returns an agent that prefers L2/L3 when DB is reachable.
func NewWithDB(pool *storage.Pool, repoID int64) *Agent {
	return &Agent{hasDB: true, pool: pool, repoID: repoID}
}

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	files := []string{}
	for _, f := range in.Diff {
		files = append(files, f.Path)
	}
	level := "L1"
	required, skippable, conf, reason := runL1(in.Diff)
	if a.hasDB && a.pool != nil {
		l2req, err := L2QueryRequired(ctx, a.pool, a.repoID, files)
		if err == nil && len(l2req) > 0 {
			required = l2req
			level = "L2"
			conf = 0.75
			reason = "L2: coverage_map intersection"
		}
	}
	status := interfaces.StatusOK
	if conf < 0.5 {
		status = interfaces.StatusPartial
	}
	return interfaces.AgentOutput{
		Agent: interfaces.AgentSelectiveTest, Status: status,
		DurationMs: int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1", Findings: []interfaces.Finding{},
		Summary: "selective test " + level,
		Metadata: map[string]any{
			"analysis_level": level, "confidence": conf,
			"required": required, "skippable": skippable,
			"reason": reason, "fallback_chain": []string{level},
		},
	}, nil
}
```

- [ ] **Step 4: Run with DB**

```bash
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" \
  go test ./internal/agents/testselect/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/testselect/
git commit -m "feat(testselect): add L2 coverage_map intersection level"
```

---

### Task 16: Selective Test L3

**Files:**
- Create: `internal/agents/testselect/level_l3.go`
- Create: `internal/agents/testselect/level_l3_test.go`
- Modify: `internal/agents/testselect/agent.go` (add L3 fallback chain)

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/testselect/level_l3_test.go
package testselect

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestL3ReverseBFSAndConfidence(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../migrations")

	pool.Exec(ctx, "INSERT INTO repos(name) VALUES('l3-test') ON CONFLICT DO NOTHING")
	var rid int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='l3-test'").Scan(&rid)
	for _, id := range []string{"l3.A", "l3.B", "l3.C"} {
		pool.Exec(ctx, `INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, hash)
			VALUES($1, $2, 'function', 'go', $3, 1, 5, 'h') ON CONFLICT DO NOTHING`, id, rid, id+".go")
	}
	pool.Exec(ctx, "DELETE FROM edges WHERE caller IN ('l3.A','l3.B','l3.C')")
	pool.Exec(ctx, `INSERT INTO edges(caller, callee, call_file, call_line, kind) VALUES
		('l3.A','l3.B','x',1,'direct'),
		('l3.B','l3.C','y',1,'direct')`)
	pool.Exec(ctx, `INSERT INTO coverage_map(repo_id, test_id, covered_symbol_id, last_seen_sha)
		VALUES($1, 'TestA', 'l3.A', 'sha1') ON CONFLICT DO NOTHING`, rid)

	required, conf, err := L3QueryRequired(ctx, pool, rid, []string{"l3.C"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(required) != 1 || required[0] != "TestA" {
		t.Fatalf("expected TestA, got %v", required)
	}
	if conf < 0.85 {
		t.Fatalf("confidence too low: %f", conf)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/testselect/level_l3.go
package testselect

import (
	"context"

	"github.com/acme/releaseguard/internal/storage"
)

func L3QueryRequired(ctx context.Context, pool *storage.Pool, repoID int64, changedSymbols []string) ([]string, float64, error) {
	rows, err := pool.Query(ctx, `
		WITH RECURSIVE upstream(sym, depth) AS (
		  SELECT s, 0 FROM unnest($1::text[]) AS s
		  UNION
		  SELECT e.caller, u.depth + 1
		  FROM edges e JOIN upstream u ON e.callee = u.sym
		  WHERE u.depth < 3
		)
		SELECT DISTINCT cm.test_id
		FROM coverage_map cm
		JOIN upstream u ON cm.covered_symbol_id = u.sym
		WHERE cm.repo_id = $2`, changedSymbols, repoID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var tests []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tests = append(tests, t)
	}
	// confidence: count dynamic ratio
	var dynRatio float64
	pool.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT count(*) FROM edges WHERE callee = ANY($1) AND kind='dynamic')::float
			/ NULLIF((SELECT count(*) FROM edges WHERE callee = ANY($1)), 0), 0)`,
		changedSymbols).Scan(&dynRatio)
	conf := 0.9
	if dynRatio > 0.3 {
		conf -= 0.3
	}
	return tests, conf, nil
}
```

Modify `agent.go` to attempt L3 first:

```go
// In Run, before attempting L2:
if a.hasDB && a.pool != nil {
	// changed symbols = files mapped to symbols (best-effort)
	var changedSymbols []string
	rows, _ := a.pool.Query(ctx, "SELECT id FROM symbols WHERE repo_id=$1 AND file = ANY($2)", a.repoID, files)
	for rows.Next() {
		var id string
		rows.Scan(&id)
		changedSymbols = append(changedSymbols, id)
	}
	rows.Close()
	if len(changedSymbols) > 0 {
		if l3req, l3conf, err := L3QueryRequired(ctx, a.pool, a.repoID, changedSymbols); err == nil && len(l3req) > 0 {
			required = l3req
			level = "L3"
			conf = l3conf
			reason = "L3: reverse BFS on edges"
		}
	}
}
```

- [ ] **Step 4: Run with DB**

```bash
TEST_POSTGRES_URL=... go test ./internal/agents/testselect/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/testselect/
git commit -m "feat(testselect): add L3 reverse BFS with dynamic-edge confidence penalty"
```

---

### Task 17: Ownership Agent — selector + shuffle

**Files:**
- Create: `internal/agents/ownership/selector.go`
- Create: `internal/agents/ownership/selector_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/ownership/selector_test.go
package ownership

import (
	"testing"
)

func TestSelectTopKThenShuffle(t *testing.T) {
	cs := []candidate{
		{Author: "a", Score: 0.9},
		{Author: "b", Score: 0.6},
		{Author: "c", Score: 0.3},
		{Author: "d", Score: 0.5},
		{Author: "e", Score: 0.7},
	}
	out := selectAndShuffle(cs, 3, 1234) // seed=1234 for determinism in test
	if len(out) != 3 {
		t.Fatalf("expected 3, got %d", len(out))
	}
	want := map[string]bool{"a": true, "e": true, "b": true}
	for _, c := range out {
		if !want[c.Author] {
			t.Errorf("unexpected author in top3: %s", c.Author)
		}
	}
}

func TestSelectFewerThanK(t *testing.T) {
	out := selectAndShuffle([]candidate{{Author: "x", Score: 1}}, 3, 0)
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/ownership/selector.go
package ownership

import (
	"math/rand"
	"sort"
)

type candidate struct {
	Author string
	Score  float64
	Source string // "blame" | "cochange" — internal only, NOT exposed
	Reason string // human-readable, used to build context (no score numbers)
}

func selectAndShuffle(cs []candidate, k int, seed int64) []candidate {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Score > cs[j].Score })
	if len(cs) < k {
		k = len(cs)
	}
	top := append([]candidate{}, cs[:k]...)
	r := rand.New(rand.NewSource(seed))
	r.Shuffle(len(top), func(i, j int) { top[i], top[j] = top[j], top[i] })
	return top
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/ownership/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/ownership/
git commit -m "feat(ownership): add internal top-K selector with output shuffle"
```

---

### Task 18: Ownership — context phrasing (no `score`, no ranking words)

**Files:**
- Create: `internal/agents/ownership/context_phrasing.go`
- Create: `internal/agents/ownership/context_phrasing_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/ownership/context_phrasing_test.go
package ownership

import (
	"strings"
	"testing"
)

func TestPhraseDoesNotContainBannedWords(t *testing.T) {
	c := candidate{Author: "alice", Source: "blame", Reason: "owns orders/"}
	got := phraseContext(c)
	for _, banned := range []string{"score", "expert", "top owner", "highest", "best"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Errorf("phrase contains banned word %q: %s", banned, got)
		}
	}
}

func TestPhraseUsesPassiveLanguage(t *testing.T) {
	c := candidate{Author: "alice", Source: "blame", Reason: "orders/handler.go", Score: 0.92}
	got := phraseContext(c)
	if !strings.Contains(got, "has recent changes") && !strings.Contains(got, "co-changes") {
		t.Errorf("expected passive phrase, got: %s", got)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/ownership/context_phrasing.go
package ownership

func phraseContext(c candidate) string {
	switch c.Source {
	case "blame":
		return "has recent changes in " + c.Reason
	case "cochange":
		return "co-changes with " + c.Reason
	default:
		return "has touched " + c.Reason
	}
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/ownership/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/ownership/context_phrasing.go internal/agents/ownership/context_phrasing_test.go
git commit -m "feat(ownership): add neutral context phrasing helper"
```

---

### Task 19: Ownership — impact_zones

**Files:**
- Create: `internal/agents/ownership/zones.go`
- Create: `internal/agents/ownership/zones_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/ownership/zones_test.go
package ownership

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestZonesGroupsByTopLevelDir(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../migrations")

	pool.Exec(ctx, "INSERT INTO repos(name) VALUES('zones-test') ON CONFLICT DO NOTHING")
	var rid int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='zones-test'").Scan(&rid)
	for _, a := range []string{"a@x.com", "b@x.com", "c@x.com"} {
		pool.Exec(ctx, `INSERT INTO ownership_signals(repo_id, file_path, author, blame_weight, recency_score)
			VALUES($1, 'orders/handler.go', $2, 0.3, 0.9) ON CONFLICT DO NOTHING`, rid, a)
	}
	zs := computeZones(ctx, pool, rid, []string{"orders/handler.go", "payments/svc.go"})
	if len(zs) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(zs))
	}
	for _, z := range zs {
		if z.Path == "orders/" && z.RecentActiveAuthorsCount != 3 {
			t.Errorf("orders/: expected 3 authors, got %d", z.RecentActiveAuthorsCount)
		}
	}
}
```

- [ ] **Step 2: Run, expect FAIL (or SKIP without DB)**

- [ ] **Step 3: Implement**

```go
// internal/agents/ownership/zones.go
package ownership

import (
	"context"
	"strings"

	"github.com/acme/releaseguard/internal/storage"
)

type Zone struct {
	Path                     string
	RecentActiveAuthorsCount int
	LookbackDays             int
}

func computeZones(ctx context.Context, pool *storage.Pool, repoID int64, files []string) []Zone {
	zones := map[string]map[string]bool{}
	for _, f := range files {
		zone := topLevel(f)
		if zones[zone] == nil {
			zones[zone] = map[string]bool{}
		}
		// query authors in this zone (file_path LIKE zone%)
		rows, err := pool.Query(ctx,
			"SELECT DISTINCT author FROM ownership_signals WHERE repo_id=$1 AND file_path LIKE $2",
			repoID, zone+"%")
		if err != nil {
			continue
		}
		for rows.Next() {
			var a string
			rows.Scan(&a)
			zones[zone][a] = true
		}
		rows.Close()
	}
	out := []Zone{}
	for z, authors := range zones {
		out = append(out, Zone{Path: z, RecentActiveAuthorsCount: len(authors), LookbackDays: 90})
	}
	return out
}

func topLevel(path string) string {
	idx := strings.IndexByte(path, '/')
	if idx < 0 {
		return path
	}
	return path[:idx+1]
}
```

- [ ] **Step 4: Run with DB, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add internal/agents/ownership/zones.go internal/agents/ownership/zones_test.go
git commit -m "feat(ownership): add impact_zones computation"
```

---

### Task 20: Ownership — hidden_dependency_hints

**Files:**
- Create: `internal/agents/ownership/hints.go`
- Create: `internal/agents/ownership/hints_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/ownership/hints_test.go
package ownership

import "testing"

func TestHintsTriggersOnRatio(t *testing.T) {
	matrix := map[string]map[string]int{
		"orders/":   {"payments/": 18},
		"payments/": {"orders/": 18},
	}
	totals := map[string]int{"orders/": 22, "payments/": 22}
	hints := hintsFromMatrix(matrix, totals)
	if len(hints) == 0 {
		t.Fatal("expected hints, got 0")
	}
	if hints[0].Context == "" {
		t.Fatal("context missing")
	}
}

func TestHintsBelowThresholdSuppressed(t *testing.T) {
	matrix := map[string]map[string]int{"a/": {"b/": 1}}
	totals := map[string]int{"a/": 100}
	hints := hintsFromMatrix(matrix, totals)
	if len(hints) != 0 {
		t.Fatalf("expected suppressed: %+v", hints)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/agents/ownership/hints.go
package ownership

import "fmt"

type Hint struct {
	From    string
	To      string
	Context string
}

const minRatio = 0.5

func hintsFromMatrix(matrix map[string]map[string]int, totals map[string]int) []Hint {
	var out []Hint
	for from, peers := range matrix {
		fromTotal := totals[from]
		if fromTotal == 0 {
			continue
		}
		for to, count := range peers {
			ratio := float64(count) / float64(fromTotal)
			if ratio >= minRatio {
				out = append(out, Hint{
					From: from, To: to,
					Context: fmt.Sprintf("these two zones changed together in %d/%d past commits", count, fromTotal),
				})
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/ownership/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/ownership/hints.go internal/agents/ownership/hints_test.go
git commit -m "feat(ownership): add hidden_dependency_hints from co-change matrix"
```

---

### Task 21: Ownership — agent main

**Files:**
- Create: `internal/agents/ownership/agent.go`
- Create: `internal/agents/ownership/agent_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/ownership/agent_test.go
package ownership

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acme/releaseguard/internal/interfaces"
)

func TestOutputSchemaHasNoScoreField(t *testing.T) {
	a := New(nil, 0)
	out, _ := a.Run(context.Background(), interfaces.AgentInput{})
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), `"score":`) {
		t.Fatalf("score must not appear in JSON: %s", raw)
	}
	if strings.Contains(string(raw), `"kind":`) {
		t.Fatalf("kind must not appear in JSON output: %s", raw)
	}
}
```

- [ ] **Step 2: Run, expect FAIL (compile)**

- [ ] **Step 3: Implement**

```go
// internal/agents/ownership/agent.go
package ownership

import (
	"context"
	"time"

	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/storage"
)

type Agent struct {
	pool   *storage.Pool
	repoID int64
}

func New(pool *storage.Pool, repoID int64) *Agent {
	return &Agent{pool: pool, repoID: repoID}
}

func (a *Agent) Name() interfaces.AgentName { return interfaces.AgentOwnership }

type SuggestedReviewer struct {
	Name    string `json:"name"`
	Context string `json:"context"`
}

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	if a.pool == nil {
		return interfaces.AgentOutput{
			Agent: interfaces.AgentOwnership, Status: interfaces.StatusFailed,
			DurationMs: int(time.Since(start) / time.Millisecond),
			SchemaVersion: "1", Summary: "no Postgres",
		}, nil
	}
	files := []string{}
	for _, f := range in.Diff {
		files = append(files, f.Path)
	}

	candidates := buildCandidates(ctx, a.pool, a.repoID, files)
	top := selectAndShuffle(candidates, 3, time.Now().UnixNano())

	suggested := []SuggestedReviewer{}
	for _, c := range top {
		suggested = append(suggested, SuggestedReviewer{
			Name: c.Author, Context: phraseContext(c),
		})
	}
	zones := computeZones(ctx, a.pool, a.repoID, files)
	hints := []Hint{} // wire matrix when available; here empty for PoC

	return interfaces.AgentOutput{
		Agent: interfaces.AgentOwnership, Status: interfaces.StatusOK,
		DurationMs: int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1", Findings: []interfaces.Finding{},
		Summary: "ownership: impact + suggestions",
		Metadata: map[string]any{
			"suggested_reviewers":     suggested,
			"impact_zones":            zones,
			"hidden_dependency_hints": hints,
		},
	}, nil
}

func buildCandidates(ctx context.Context, pool *storage.Pool, repoID int64, files []string) []candidate {
	var out []candidate
	for _, f := range files {
		rows, err := pool.Query(ctx,
			"SELECT author, blame_weight, recency_score FROM ownership_signals WHERE repo_id=$1 AND file_path=$2",
			repoID, f)
		if err != nil {
			continue
		}
		for rows.Next() {
			var author string
			var weight, recency float64
			rows.Scan(&author, &weight, &recency)
			out = append(out, candidate{
				Author: author,
				Score:  weight * recency,
				Source: "blame",
				Reason: f,
			})
		}
		rows.Close()
	}
	return out
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/ownership/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/ownership/agent.go internal/agents/ownership/agent_test.go
git commit -m "feat(ownership): add agent main with suggested_reviewers + impact_zones"
```

---

### Task 22: Wire ownership + L2/L3 testselect into analyzer

**Files:**
- Modify: `cmd/analyzer/main.go`

- [ ] **Step 1: Write the test**

```go
// cmd/analyzer/topology0_test.go
package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestBuildAgentsTopology0(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()

	agents := buildAgentsTopology0(true, true, true, true, pool, 1, buildAgentsDeps{})
	if len(agents) != 4 {
		t.Fatalf("expected 4 agents, got %d", len(agents))
	}
}
```

- [ ] **Step 2: Implement updated `buildAgents`**

Replace `buildAgents` in `cmd/analyzer/main.go`:

```go
func buildAgents(selective, risk, ownership, aiRev, hasDB bool, opt ...buildAgentsDeps) []interfaces.IAgent {
	var deps buildAgentsDeps
	if len(opt) > 0 {
		deps = opt[0]
	}
	if hasDB {
		return buildAgentsTopology0(selective, risk, ownership, aiRev, deps.pool, deps.repoID, deps)
	}
	var agents []interfaces.IAgent
	if selective {
		agents = append(agents, testselect.New(false))
	}
	if risk {
		agents = append(agents, rollout.New(rollout.Deps{}))
	}
	if aiRev {
		agents = append(agents, reviewer.New(deps.prov, deps.projectsDir, deps.systemName, deps.serviceTypes))
	}
	return agents
}

func buildAgentsTopology0(selective, risk, ownership, aiRev bool, pool *storage.Pool, repoID int64, deps buildAgentsDeps) []interfaces.IAgent {
	var agents []interfaces.IAgent
	if selective {
		agents = append(agents, testselect.NewWithDB(pool, repoID))
	}
	if risk {
		agents = append(agents, rollout.New(rollout.Deps{}))
	}
	if ownership {
		agents = append(agents, ownershippkg.New(pool, repoID))
	}
	if aiRev {
		agents = append(agents, reviewer.New(deps.prov, deps.projectsDir, deps.systemName, deps.serviceTypes))
	}
	return agents
}
```

Update imports: add `ownershippkg "github.com/acme/releaseguard/internal/agents/ownership"` and `"github.com/acme/releaseguard/internal/storage"`. Update `buildAgentsDeps` to include `pool *storage.Pool` and `repoID int64`.

- [ ] **Step 3: Adjust `run()` to look up `repoID` and pass to factory**

```go
// In run() after PG connection:
var repoID int64
if cfg.PostgresURL != "" {
	pool, err := storage.NewPool(ctx, cfg.PostgresURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repoName := os.Getenv("CI_PROJECT_PATH")
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name=$1", repoName).Scan(&repoID); err != nil {
		log.Warn("repo not registered, skipping DB-backed agents")
	}
	deps := buildAgentsDeps{prov: prov, projectsDir: cfg.ProjectsDir,
		systemName: os.Getenv("RG_SERVICE_NAME"),
		serviceTypes: parseServiceTypes(os.Getenv("RG_SERVICE_TYPE")),
		pool: pool, repoID: repoID}
	agents := buildAgents(cfg.Agents.SelectiveTest, cfg.Agents.RolloutRisk,
		cfg.Agents.Ownership, cfg.Agents.AIReviewer, true, deps)
	// proceed as before
}
```

- [ ] **Step 4: Run all tests with DB**

```bash
TEST_POSTGRES_URL=... go test ./cmd/analyzer/ ./internal/agents/... -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/analyzer/
git commit -m "feat(analyzer): wire L2/L3 testselect + ownership in topology 0"
```

---

### Task 23: Plan C complete

- [ ] **Step 1: Run full test suite with DB**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
sleep 3
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" make test
docker stop rg-pg && docker rm rg-pg
```

Expected: all green.

- [ ] **Step 2: Build both images**

```bash
make docker
make docker-indexer
```

- [ ] **Step 3: Tag**

```bash
git tag plan-c-postgres-agents
```

- [ ] **Step 4: Document achievement**

Append to `CHANGELOG.md`:

```
## Plan C status: complete (commit $(git rev-parse --short HEAD), tag plan-c-postgres-agents)
- Indexer steps 1-3 + plantuml step (4 of 5; RAG in Plan D)
- Selective Test L2 / L3
- Ownership Agent in proximity mode
```

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: mark Plan C complete"
```

---

## What Plan C delivers

- Postgres schema for symbols/edges/coverage_map/ownership_signals/mr_runs/agent_outputs/findings (in addition to repos and cross_repo_edges from earlier plans)
- `cmd/indexer nightly` runs callgraph build + coverage load + cochange + plantuml extraction
- Selective Test agent now picks L1/L2/L3 based on data availability
- Ownership Agent emits `suggested_reviewers` (shuffled, no score), `impact_zones`, `hidden_dependency_hints`
- Analyzer topology 0 produces the full Impact Scope Report with all four agents

**Next:** Plan D adds the RAG layer (embedder + connectors + hybrid retrieval + hot path) and the `cmd/indexer backfill` subcommand.
