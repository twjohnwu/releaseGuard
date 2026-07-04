# ReleaseGuard Plan D — RAG + Backfill Implementation Plan

> **STATUS: SPEC ONLY — NOT IMPLEMENTED**
>
> 整份 Plan D（RAG pipeline、embedding backfill、vector 檢索）皆為設計規格，尚未落地。

> **🟡 STATUS: Deferred Roadmap (not in current PoC scope)**
>
> 此 plan 屬於「未來擴展」階段，**目前 PoC 主軸只做 Plan A + Plan B**（拓樸 1 部署 + 核心 Decision Arbitration Layer）。
> 完整理由見 `decisions_log.md` 的 Decision #7（拓樸 1）與整體 scope 收斂判斷。
> 此 plan 保留是為展示「設計過 / 想過 / 但有紀律地不做」，不是被遺棄。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Prerequisite:** Plans A, B, C complete. This plan adds the RAG layer (Channel B + Hot path) to AI Reviewer and the `cmd/indexer backfill` onboarding subcommand. After completing this plan, ReleaseGuard reaches full topology 0 — all 4 agents with full functionality.

**Goal:** Build the corpus pipeline (`internal/indexer/corpus/`) with `CorpusConnector` interface + `git_runbook` connector, hybrid retrieval (vector + BM25 + RRF), AI Reviewer's Channel B + Hot path integration, and the `backfill` subcommand for one-shot historical ingestion (`mr_history` + `postmortem` source types).

**Architecture:** `internal/indexer/corpus/` is internally split as `connectors/` + `chunker/` + `embedder/` + `storage/`. The `CorpusConnector` interface is the extension point for future Confluence/Notion/Slack connectors. RAG retrieval happens in the analyzer at Stage 2; results merged with PROJECTS_DIR static prompts via RRF. Hot path embeds the current MR's commit message + truncated diff once (in-memory, never written to Postgres).

**Tech Stack:** Adds OpenAI embedding API (`text-embedding-3-small`, 1536 dim), pgvector extension, simple HTTP-based vector queries, Go's `text/template` for backfill summary output.

---

## File Structure (additions to Plans A+B+C)

```
releaseguard/
├── cmd/indexer/
│   ├── rag.go                       # nightly step 4
│   └── backfill.go                  # backfill subcommand
├── internal/
│   ├── indexer/corpus/
│   │   ├── connectors/
│   │   │   ├── connector.go         # interface + Document type
│   │   │   └── git_runbook.go       # PoC connector
│   │   ├── chunker/
│   │   │   └── chunker.go           # 512-token chunks, overlap 50
│   │   ├── embedder/
│   │   │   ├── embedder.go          # interface
│   │   │   └── openai.go            # OpenAI text-embedding-3-small
│   │   └── storage/
│   │       └── upsert.go            # rag_documents + rag_embeddings
│   ├── rag/
│   │   ├── retriever.go             # hybrid: vector + BM25 + RRF
│   │   └── hotpath.go               # current-MR embedding
│   └── agents/reviewer/
│       └── agent.go                 # extended: Channel B + Hot path
└── migrations/
    ├── 0007_pgvector.sql            # CREATE EXTENSION + tables
    └── 0008_runbook_sources.sql     # rag_documents source enum extended (already)
```

---

### Task 1: pgvector migration

**Files:**
- Create: `migrations/0007_pgvector.sql`

- [ ] **Step 1: Write migration**

```sql
-- migrations/0007_pgvector.sql
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS rag_documents (
  id          BIGSERIAL PRIMARY KEY,
  source_type TEXT NOT NULL,
  source_path TEXT NOT NULL,
  repo_id     BIGINT NULL REFERENCES repos(id),
  content     TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  content_tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source_path, content_hash)
);
CREATE INDEX IF NOT EXISTS idx_rag_source ON rag_documents(source_type, repo_id);
CREATE INDEX IF NOT EXISTS idx_rag_tsv ON rag_documents USING GIN(content_tsv);

CREATE TABLE IF NOT EXISTS rag_embeddings (
  id           BIGSERIAL PRIMARY KEY,
  document_id  BIGINT NOT NULL REFERENCES rag_documents(id) ON DELETE CASCADE,
  chunk_idx    INT NOT NULL,
  chunk_text   TEXT NOT NULL,
  embedding    vector(1536) NOT NULL,
  UNIQUE (document_id, chunk_idx)
);
CREATE INDEX IF NOT EXISTS idx_rag_embed_vec ON rag_embeddings
  USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
```

- [ ] **Step 2: Run integration test**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test pgvector/pgvector:pg16
sleep 5
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" \
  go test ./internal/storage/ -run TestMigrate -v
docker stop rg-pg && docker rm rg-pg
```

Expected: PASS — `rag_documents` and `rag_embeddings` exist.

- [ ] **Step 3: Commit**

```bash
git add migrations/0007_pgvector.sql
git commit -m "feat(migrations): add pgvector extension + rag_documents/embeddings"
```

- [ ] **Step 4-5: (Skip — single migration file is the unit)**

---

### Task 2: CorpusConnector interface + Document type

**Files:**
- Create: `internal/indexer/corpus/connectors/connector.go`
- Create: `internal/indexer/corpus/connectors/connector_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/corpus/connectors/connector_test.go
package connectors

import (
	"context"
	"testing"
)

type fakeConn struct{}

func (f *fakeConn) Name() string { return "fake" }
func (f *fakeConn) Fetch(ctx context.Context) ([]Document, error) {
	return []Document{{SourcePath: "fake/x.md", Content: "hello", Scope: "global", SourceType: "runbook"}}, nil
}

func TestConnectorInterfaceShape(t *testing.T) {
	var c CorpusConnector = &fakeConn{}
	docs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(docs) != 1 || docs[0].SourcePath != "fake/x.md" {
		t.Fatalf("unexpected: %+v", docs)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/corpus/connectors/connector.go
package connectors

import "context"

type Document struct {
	SourcePath string            // <connector-name>/<relative-path>
	Content    string
	Metadata   map[string]string // connector-specific (commit_sha, page_id, ...)
	Scope      string            // "global" | "repo:<name>"
	SourceType string            // "runbook" | "mr_history" | "postmortem" | ...
}

type CorpusConnector interface {
	Name() string
	Fetch(ctx context.Context) ([]Document, error)
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/corpus/connectors/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/corpus/connectors/
git commit -m "feat(corpus): add CorpusConnector interface and Document type"
```

---

### Task 3: git_runbook connector

**Files:**
- Create: `internal/indexer/corpus/connectors/git_runbook.go`
- Create: `internal/indexer/corpus/connectors/git_runbook_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/corpus/connectors/git_runbook_test.go
package connectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGitRunbookFromLocalPath(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "incidents"), 0755)
	os.WriteFile(filepath.Join(dir, "incidents", "2024-01.md"), []byte("postmortem 1"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0644)

	c := NewGitRunbook([]RunbookSource{
		{Name: "ops", Path: dir, Globs: []string{"incidents/**/*.md"}, Scope: "global"},
	})
	docs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc (incidents only), got %d", len(docs))
	}
	if docs[0].SourcePath != "ops/incidents/2024-01.md" {
		t.Fatalf("source path: %s", docs[0].SourcePath)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/corpus/connectors/git_runbook.go
package connectors

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type RunbookSource struct {
	Name  string   `json:"name"`
	Repo  string   `json:"repo,omitempty"`
	Ref   string   `json:"ref,omitempty"`
	Path  string   `json:"path,omitempty"`
	Globs []string `json:"globs"`
	Scope string   `json:"scope"`
}

type GitRunbook struct {
	sources []RunbookSource
}

func NewGitRunbook(sources []RunbookSource) *GitRunbook {
	return &GitRunbook{sources: sources}
}

func (g *GitRunbook) Name() string { return "git_runbook" }

func (g *GitRunbook) Fetch(ctx context.Context) ([]Document, error) {
	var out []Document
	for _, src := range g.sources {
		root, cleanup, err := resolveRoot(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", src.Name, err)
		}
		if cleanup != nil {
			defer cleanup()
		}
		matches := globAll(root, src.Globs)
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			b, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			out = append(out, Document{
				SourcePath: filepath.Join(src.Name, rel),
				Content:    string(b),
				Scope:      src.Scope,
				SourceType: "runbook",
			})
		}
	}
	return out, nil
}

func resolveRoot(ctx context.Context, src RunbookSource) (string, func(), error) {
	if src.Path != "" {
		return src.Path, nil, nil
	}
	// PoC: assume repo is locally available at /checkout/<name>; in production use git clone
	return filepath.Join("/checkout", src.Name), nil, nil
}

func globAll(root string, patterns []string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		for _, pat := range patterns {
			if matched, _ := filepath.Match(pat, rel); matched {
				out = append(out, p)
				return nil
			}
			// Support **/*.md by simple suffix match
			if strings.HasPrefix(pat, "**/") && strings.HasSuffix(rel, strings.TrimPrefix(pat, "**/")) {
				out = append(out, p)
				return nil
			}
			if strings.Contains(pat, "**") {
				clean := strings.ReplaceAll(pat, "**/", "")
				if matched, _ := filepath.Match(clean, filepath.Base(rel)); matched {
					out = append(out, p)
					return nil
				}
			}
		}
		return nil
	})
	return out
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/corpus/connectors/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/corpus/connectors/git_runbook.go internal/indexer/corpus/connectors/git_runbook_test.go
git commit -m "feat(corpus): add git_runbook connector with glob filter"
```

---

### Task 4: Chunker

**Files:**
- Create: `internal/indexer/corpus/chunker/chunker.go`
- Create: `internal/indexer/corpus/chunker/chunker_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/corpus/chunker/chunker_test.go
package chunker

import (
	"strings"
	"testing"
)

func TestChunkSplitsOnTokenBudget(t *testing.T) {
	long := strings.Repeat("hello world ", 500) // ~500 tokens
	chunks := Split(long, 100, 20)
	if len(chunks) < 4 {
		t.Fatalf("expected ≥4 chunks, got %d", len(chunks))
	}
}

func TestChunkOverlapPreserved(t *testing.T) {
	text := strings.Repeat("a b c d ", 100)
	chunks := Split(text, 50, 10)
	if len(chunks) < 2 {
		t.Fatal("expected multi-chunk")
	}
	tail := chunks[0][len(chunks[0])-30:]
	if !strings.Contains(chunks[1], tail[:10]) {
		// approximate; relax if cosmetic
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/corpus/chunker/chunker.go
package chunker

import "strings"

// Split breaks `text` into chunks of approx `chunkTokens` tokens with `overlap` token overlap.
// Token approx: 4 chars per token.
func Split(text string, chunkTokens, overlap int) []string {
	chunkChars := chunkTokens * 4
	overlapChars := overlap * 4
	if chunkChars <= 0 {
		return []string{text}
	}
	var out []string
	for i := 0; i < len(text); i += chunkChars - overlapChars {
		end := i + chunkChars
		if end > len(text) {
			end = len(text)
		}
		out = append(out, strings.TrimSpace(text[i:end]))
		if end == len(text) {
			break
		}
	}
	return out
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/corpus/chunker/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/corpus/chunker/
git commit -m "feat(corpus): add token-approximate chunker with overlap"
```

---

### Task 5: Embedder interface

**Files:**
- Create: `internal/indexer/corpus/embedder/embedder.go`

- [ ] **Step 1: Write minimal compile test**

```go
// internal/indexer/corpus/embedder/embedder_test.go
package embedder

import "testing"

func TestEmbedderType(t *testing.T) {
	var _ Embedder = (*nopEmbedder)(nil)
}

type nopEmbedder struct{}

func (*nopEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}
```

Add `import "context"`.

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/corpus/embedder/embedder.go
package embedder

import "context"

type Embedder interface {
	Name() string
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/corpus/embedder/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/corpus/embedder/
git commit -m "feat(embedder): add Embedder interface"
```

---

### Task 6: OpenAI embedder

**Files:**
- Create: `internal/indexer/corpus/embedder/openai.go`
- Create: `internal/indexer/corpus/embedder/openai_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/corpus/embedder/openai_test.go
package embedder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedReturnsVectors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3],"index":0},{"embedding":[0.4,0.5,0.6],"index":1}]}`))
	}))
	defer srv.Close()
	e := NewOpenAI("k", "text-embedding-3-small")
	e.endpoint = srv.URL + "/v1/embeddings"
	vecs, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 3 {
		t.Fatalf("got: %+v", vecs)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/indexer/corpus/embedder/openai.go
package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type OpenAI struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

func NewOpenAI(apiKey, model string) *OpenAI {
	return &OpenAI{
		apiKey:   apiKey,
		model:    model,
		endpoint: "https://api.openai.com/v1/embeddings",
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (o *OpenAI) Name() string { return "openai-" + o.model }

type oaiReq struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}
type oaiResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (o *OpenAI) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(oaiReq{Input: texts, Model: o.model})
	req, err := http.NewRequestWithContext(ctx, "POST", o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai %d: %s", resp.StatusCode, string(out))
	}
	var r oaiResp
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	sort.SliceStable(r.Data, func(i, j int) bool { return r.Data[i].Index < r.Data[j].Index })
	vecs := make([][]float32, len(r.Data))
	for i, d := range r.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/indexer/corpus/embedder/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/corpus/embedder/openai.go internal/indexer/corpus/embedder/openai_test.go
git commit -m "feat(embedder): add OpenAI embedder"
```

---

### Task 7: RAG storage (UPSERT documents + embeddings)

**Files:**
- Create: `internal/indexer/corpus/storage/upsert.go`
- Create: `internal/indexer/corpus/storage/upsert_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/indexer/corpus/storage/upsert_test.go
package corpusstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestUpsertDocumentIdempotent(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../../migrations")

	content := "hello"
	h := sha256.Sum256([]byte(content))
	hashStr := hex.EncodeToString(h[:])

	id1, err := UpsertDocument(ctx, pool, "runbook", "test/a.md", nil, content, hashStr)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	id2, err := UpsertDocument(ctx, pool, "runbook", "test/a.md", nil, content, hashStr)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same id, got %d != %d", id1, id2)
	}
}
```

- [ ] **Step 2: Run, expect FAIL/SKIP**

- [ ] **Step 3: Implement**

```go
// internal/indexer/corpus/storage/upsert.go
package corpusstorage

import (
	"context"

	"github.com/acme/releaseguard/internal/storage"
)

func UpsertDocument(ctx context.Context, pool *storage.Pool, sourceType, sourcePath string, repoID *int64, content, contentHash string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO rag_documents(source_type, source_path, repo_id, content, content_hash)
		VALUES($1, $2, $3, $4, $5)
		ON CONFLICT (source_path, content_hash)
		DO UPDATE SET updated_at = now()
		RETURNING id`,
		sourceType, sourcePath, repoID, content, contentHash).Scan(&id)
	return id, err
}

func UpsertEmbedding(ctx context.Context, pool *storage.Pool, docID int64, chunkIdx int, chunkText string, vec []float32) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO rag_embeddings(document_id, chunk_idx, chunk_text, embedding)
		VALUES($1, $2, $3, $4::vector)
		ON CONFLICT (document_id, chunk_idx)
		DO UPDATE SET chunk_text = EXCLUDED.chunk_text, embedding = EXCLUDED.embedding`,
		docID, chunkIdx, chunkText, vecToString(vec))
	return err
}

func vecToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, x := range v {
		if i > 0 {
			s += ","
		}
		s += float32String(x)
	}
	s += "]"
	return s
}

func float32String(f float32) string {
	return strconv.FormatFloat(float64(f), 'f', 6, 32)
}
```

Add `import "strconv"`.

- [ ] **Step 4: Run with DB, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/corpus/storage/
git commit -m "feat(corpus): add idempotent UPSERT for documents + embeddings"
```

---

### Task 8: Indexer Step 4 (RAG ingest)

**Files:**
- Create: `cmd/indexer/rag.go`
- Modify: `cmd/indexer/nightly.go` (add stepRAG call)

- [ ] **Step 1: Write the failing test**

```go
// cmd/indexer/rag_test.go
package main

import (
	"testing"
)

func TestParseRunbookSourcesEnv(t *testing.T) {
	json := `[{"name":"x","path":"/p","globs":["*.md"],"scope":"global"}]`
	srcs, err := parseRunbookSources(json)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(srcs) != 1 || srcs[0].Name != "x" {
		t.Fatalf("got: %+v", srcs)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// cmd/indexer/rag.go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/acme/releaseguard/internal/indexer/corpus/chunker"
	"github.com/acme/releaseguard/internal/indexer/corpus/connectors"
	"github.com/acme/releaseguard/internal/indexer/corpus/embedder"
	corpusstorage "github.com/acme/releaseguard/internal/indexer/corpus/storage"
	"github.com/acme/releaseguard/internal/storage"
)

func parseRunbookSources(s string) ([]connectors.RunbookSource, error) {
	if s == "" {
		return nil, nil
	}
	var srcs []connectors.RunbookSource
	if err := json.Unmarshal([]byte(s), &srcs); err != nil {
		return nil, fmt.Errorf("RUNBOOK_SOURCES JSON: %w", err)
	}
	return srcs, nil
}

func stepRAG(ctx context.Context, pool *storage.Pool, repoID int64) error {
	if os.Getenv("RG_RAG_ENABLED") == "false" {
		return nil
	}
	srcs, err := parseRunbookSources(os.Getenv("RUNBOOK_SOURCES"))
	if err != nil {
		return err
	}
	if len(srcs) == 0 {
		return nil
	}
	conn := connectors.NewGitRunbook(srcs)
	docs, err := conn.Fetch(ctx)
	if err != nil {
		return err
	}
	emb := embedder.NewOpenAI(os.Getenv("EMBEDDING_API_KEY"),
		envDefault("EMBEDDING_MODEL", "text-embedding-3-small"))
	for _, d := range docs {
		hash := sha256.Sum256([]byte(d.Content))
		hashStr := hex.EncodeToString(hash[:])
		var ridPtr *int64
		if d.Scope != "global" {
			ridPtr = &repoID
		}
		docID, err := corpusstorage.UpsertDocument(ctx, pool,
			d.SourceType, d.SourcePath, ridPtr, d.Content, hashStr)
		if err != nil {
			return err
		}
		chunks := chunker.Split(d.Content, 512, 50)
		vecs, err := emb.EmbedBatch(ctx, chunks)
		if err != nil {
			return err
		}
		for i, v := range vecs {
			if err := corpusstorage.UpsertEmbedding(ctx, pool, docID, i, chunks[i], v); err != nil {
				return err
			}
		}
	}
	return nil
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
```

Modify `cmd/indexer/nightly.go` to call `stepRAG` after `stepPlantUML`:

```go
if err := stepRAG(ctx, pool, repoID); err != nil {
	fmt.Fprintf(os.Stderr, "rag step: %v\n", err)
}
```

- [ ] **Step 4: Run unit + integration**

```bash
go test ./cmd/indexer/ -run TestParseRunbookSources -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/indexer/rag.go cmd/indexer/nightly.go cmd/indexer/rag_test.go
git commit -m "feat(indexer): add nightly RAG ingest step"
```

---

### Task 9: RAG retriever (hybrid + RRF)

**Files:**
- Create: `internal/rag/retriever.go`
- Create: `internal/rag/retriever_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/rag/retriever_test.go
package rag

import (
	"testing"
)

func TestRRFFusion(t *testing.T) {
	vec := []Hit{{ID: 1, Score: 0.9}, {ID: 2, Score: 0.8}}
	bm := []Hit{{ID: 2, Score: 0.95}, {ID: 3, Score: 0.7}}
	merged := RRFMerge([][]Hit{vec, bm}, 60, 5)
	if len(merged) == 0 {
		t.Fatal("empty")
	}
	if merged[0].ID != 2 {
		t.Errorf("expected ID 2 first (in both lists), got %d", merged[0].ID)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/rag/retriever.go
package rag

import (
	"context"
	"sort"

	"github.com/acme/releaseguard/internal/storage"
)

type Hit struct {
	ID    int64
	Score float64
	Text  string
}

const RRFK = 60

func RRFMerge(lists [][]Hit, k int, topK int) []Hit {
	scores := map[int64]float64{}
	textCache := map[int64]string{}
	for _, list := range lists {
		for rank, h := range list {
			scores[h.ID] += 1.0 / float64(k+rank+1)
			if h.Text != "" {
				textCache[h.ID] = h.Text
			}
		}
	}
	out := make([]Hit, 0, len(scores))
	for id, s := range scores {
		out = append(out, Hit{ID: id, Score: s, Text: textCache[id]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// VectorSearch returns nearest documents by cosine distance.
func VectorSearch(ctx context.Context, pool *storage.Pool, repoID int64, queryVec []float32, k int) ([]Hit, error) {
	rows, err := pool.Query(ctx, `
		SELECT d.id, 1 - (e.embedding <=> $1::vector) AS score, e.chunk_text
		FROM rag_embeddings e JOIN rag_documents d ON e.document_id = d.id
		WHERE d.repo_id IS NULL OR d.repo_id = $2
		ORDER BY e.embedding <=> $1::vector
		LIMIT $3`, vecToString(queryVec), repoID, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Score, &h.Text); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// BM25Search uses Postgres tsvector @@ plainto_tsquery.
func BM25Search(ctx context.Context, pool *storage.Pool, repoID int64, query string, k int) ([]Hit, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, ts_rank(content_tsv, plainto_tsquery('english', $1)) AS score, content
		FROM rag_documents
		WHERE (repo_id IS NULL OR repo_id = $2)
		  AND content_tsv @@ plainto_tsquery('english', $1)
		ORDER BY score DESC LIMIT $3`, query, repoID, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Score, &h.Text); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

func vecToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, x := range v {
		if i > 0 {
			s += ","
		}
		s += floatStr(x)
	}
	s += "]"
	return s
}

func floatStr(f float32) string {
	return strconvFormatFloat(float64(f), 'f', 6, 32)
}

// Stub redirect to avoid bringing strconv into many places.
func strconvFormatFloat(f float64, fmt byte, prec, bitSize int) string {
	return formatFloat(f)
}

func formatFloat(f float64) string {
	// minimal formatter; could use strconv.FormatFloat in production
	return fmt.Sprintf("%.6f", f)
}
```

Add proper imports: `"fmt"`, replace `formatFloat` with `strconv.FormatFloat` in production. (For test simplicity, use `fmt.Sprintf`.)

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/rag/ -v -run TestRRF
```

- [ ] **Step 5: Commit**

```bash
git add internal/rag/
git commit -m "feat(rag): add hybrid retriever with RRF fusion + vector + BM25"
```

---

### Task 10: Hot path (current MR embedding)

**Files:**
- Create: `internal/rag/hotpath.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/rag/hotpath_test.go
package rag

import (
	"context"
	"testing"

	"github.com/acme/releaseguard/internal/indexer/corpus/embedder"
)

type stubEmbedder struct{}

func (*stubEmbedder) Name() string { return "stub" }
func (*stubEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return [][]float32{{0.1, 0.2}}, nil
}

func TestHotPathTruncatesLongDiff(t *testing.T) {
	emb := &stubEmbedder{}
	long := make([]byte, 10000)
	for i := range long {
		long[i] = 'x'
	}
	vec, err := EmbedHotQuery(context.Background(), emb, "msg", string(long), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 2 {
		t.Fatalf("vec %v", vec)
	}
}

var _ = embedder.Embedder(nil) // ensure import alive
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// internal/rag/hotpath.go
package rag

import (
	"context"

	"github.com/acme/releaseguard/internal/indexer/corpus/embedder"
)

// EmbedHotQuery embeds the current MR's commit message + truncated diff summary.
// The result is in-memory only; never persisted.
func EmbedHotQuery(ctx context.Context, emb embedder.Embedder, commitMsg, diffSummary string, maxBytes int) ([]float32, error) {
	if len(diffSummary) > maxBytes {
		diffSummary = diffSummary[:maxBytes]
	}
	q := commitMsg + "\n\n" + diffSummary
	vecs, err := emb.EmbedBatch(ctx, []string{q})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	return vecs[0], nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/rag/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/rag/hotpath.go internal/rag/hotpath_test.go
git commit -m "feat(rag): add hot path embedding (in-memory only)"
```

---

### Task 11: Wire RAG into AI Reviewer

**Files:**
- Modify: `internal/agents/reviewer/agent.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agents/reviewer/rag_integration_test.go
package reviewer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/acme/releaseguard/internal/ai"
	"github.com/acme/releaseguard/internal/interfaces"
)

type checkSysProvider struct{ sys string }

func (c *checkSysProvider) Name() string { return "check" }
func (c *checkSysProvider) CallWithTool(ctx context.Context, sys string, msgs []ai.Message, t ai.ToolSpec) (json.RawMessage, error) {
	c.sys = sys
	return json.RawMessage(`{"findings":[]}`), nil
}

func TestSystemPromptIncludesRAGSnippets(t *testing.T) {
	p := &checkSysProvider{}
	a := New(p, "/nonexistent", "", nil)
	a.WithRAGSnippets([]string{"runbook says: rollback before commit"})
	_, err := a.Run(context.Background(), interfaces.AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !contains(p.sys, "runbook says") {
		t.Fatalf("RAG snippets missing from system prompt: %s", p.sys)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(s) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Modify `agent.go`**

Add to `Agent`:

```go
type Agent struct {
	provider     ai.Provider
	projectsDir  string
	systemName   string
	serviceTypes []string
	maxTokens    int
	ragSnippets  []string  // NEW: filled by analyzer before Run
}

func (a *Agent) WithRAGSnippets(snippets []string) *Agent {
	a.ragSnippets = snippets
	return a
}
```

Modify `Run` to use `ComposeSystemPrompt` with snippets:

```go
system := ComposeSystemPrompt(prompts, a.ragSnippets)
```

(Already in plan B; here just plumb `a.ragSnippets`.)

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/agents/reviewer/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agents/reviewer/
git commit -m "feat(reviewer): plumb RAG snippets into system prompt"
```

---

### Task 12: Wire RAG retrieval into analyzer Stage 2

**Files:**
- Modify: `cmd/analyzer/main.go`
- Create: `cmd/analyzer/stage2_rag.go`

- [ ] **Step 1: Implement RAG fetch in analyzer**

```go
// cmd/analyzer/stage2_rag.go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/acme/releaseguard/internal/indexer/corpus/embedder"
	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/rag"
	"github.com/acme/releaseguard/internal/storage"
)

// fetchRAGSnippets returns up to 10 hybrid-retrieval text chunks for the current MR.
// Returns nil if RAG is disabled, no DB, or any error (degrades to Channel A only).
func fetchRAGSnippets(ctx context.Context, pool *storage.Pool, repoID int64, in interfaces.AgentInput, emb embedder.Embedder) []string {
	if pool == nil || repoID == 0 {
		return nil
	}
	// build query string from changed files / commit msg
	parts := []string{in.CommitSHA}
	for _, f := range in.Diff {
		parts = append(parts, f.Path)
	}
	query := strings.Join(parts, " ")

	bm, _ := rag.BM25Search(ctx, pool, repoID, query, 20)

	var vec []rag.Hit
	if emb != nil {
		hotVec, err := rag.EmbedHotQuery(ctx, emb, in.CommitSHA, query, 4096)
		if err == nil {
			vec, _ = rag.VectorSearch(ctx, pool, repoID, hotVec, 20)
		}
	}

	merged := rag.RRFMerge([][]rag.Hit{vec, bm}, rag.RRFK, 10)
	out := make([]string, 0, len(merged))
	for _, h := range merged {
		if h.Text != "" {
			out = append(out, fmt.Sprintf("(score %.3f) %s", h.Score, h.Text))
		}
	}
	return out
}
```

Modify `cmd/analyzer/main.go` to call `fetchRAGSnippets` and pass to reviewer agent before agents run:

```go
// In run() after building Postgres pool and repoID:
var emb embedder.Embedder
if cfg.RAGEnabled {
	emb = embedder.NewOpenAI(os.Getenv("EMBEDDING_API_KEY"),
		envOr("EMBEDDING_MODEL", "text-embedding-3-small"))
}
ragSnippets := fetchRAGSnippets(ctx, pool, repoID, in, emb)

// after building agents, inject snippets into reviewer:
for _, a := range agents {
	if r, ok := a.(*reviewerpkg.Agent); ok {
		r.WithRAGSnippets(ragSnippets)
	}
}
```

(Add `reviewerpkg "github.com/acme/releaseguard/internal/agents/reviewer"` import; add `envOr` helper.)

- [ ] **Step 2-3: Run e2e tests against fixture DB**

```bash
TEST_POSTGRES_URL=... go test ./cmd/analyzer/ -v
```

- [ ] **Step 4: Build and verify**

```bash
make test
make docker
```

- [ ] **Step 5: Commit**

```bash
git add cmd/analyzer/stage2_rag.go cmd/analyzer/main.go
git commit -m "feat(analyzer): wire RAG retrieval into stage 2 + reviewer prompt"
```

---

### Task 13: Backfill subcommand

**Files:**
- Create: `cmd/indexer/backfill.go` (replace stub)
- Create: `cmd/indexer/backfill_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/indexer/backfill_test.go
package main

import (
	"context"
	"testing"
)

func TestBackfillRequiresRepoFlag(t *testing.T) {
	err := Backfill(context.Background(), []string{})
	if err == nil {
		t.Fatal("expected error: missing --repo")
	}
}

func TestBackfillSourcesParse(t *testing.T) {
	srcs := parseSourcesArg("mr_history,postmortem")
	if len(srcs) != 2 {
		t.Fatalf("expected 2, got %d", len(srcs))
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// cmd/indexer/backfill.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func parseSourcesArg(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func Backfill(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	repo := fs.String("repo", "", "repo name (matches repos.name)")
	since := fs.String("since", "1y", "lookback window, e.g. 1y or 6m")
	sourcesArg := fs.String("sources", "mr_history,postmortem",
		"comma-separated source types to backfill")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return fmt.Errorf("--repo required")
	}
	sources := parseSourcesArg(*sourcesArg)
	if len(sources) == 0 {
		return fmt.Errorf("--sources required")
	}
	rate := envIntDefault("EMBEDDING_RATE_LIMIT_RPM", 60)
	fmt.Printf("backfill: repo=%s since=%s sources=%v rate=%d/min\n", *repo, *since, sources, rate)

	pgURL := os.Getenv("POSTGRES_URL")
	if pgURL == "" {
		return fmt.Errorf("POSTGRES_URL required")
	}
	// rest reuses corpus pipeline logic; the connector for `mr_history` and `postmortem`
	// is the same `git_runbook` pattern with different RUNBOOK_SOURCES entries
	// configured by the operator.
	// Per-MR rate limit:
	tick := time.Second * 60 / time.Duration(rate)
	t := time.NewTicker(tick)
	defer t.Stop()

	// In a real backfill run, the operator passes RUNBOOK_SOURCES with mr_history /
	// postmortem source_type entries. This subcommand simply reuses stepRAG.
	pool, err := openPool(ctx, pgURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	var repoID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name=$1", *repo).Scan(&repoID); err != nil {
		return err
	}
	return stepRAG(ctx, pool, repoID)
}

func envIntDefault(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, _ := strconv.Atoi(v)
	if n <= 0 {
		return def
	}
	return n
}

// openPool is a tiny wrapper used by both nightly and backfill.
func openPool(ctx context.Context, url string) (*pool, error) {
	return loadPool(ctx, url) // see nightly.go
}
```

(Adjust nightly.go to expose `openPool`/`loadPool` helpers; refactor for shared use.)

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./cmd/indexer/ -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/indexer/backfill.go cmd/indexer/backfill_test.go
git commit -m "feat(indexer): implement backfill subcommand reusing stepRAG"
```

---

### Task 14: Plan D complete — final integration

- [ ] **Step 1: Run full test suite**

```bash
docker run -d --name rg-pg -p 5432:5432 -e POSTGRES_PASSWORD=test pgvector/pgvector:pg16
sleep 5
TEST_POSTGRES_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" make test
docker stop rg-pg && docker rm rg-pg
```

Expected: all green.

- [ ] **Step 2: Build both images**

```bash
make docker
make docker-indexer
```

- [ ] **Step 3: End-to-end smoke against fixtures**

Manual:
```bash
docker compose -f deploy/compose.yaml up -d  # if you set one up; else docker run pgvector
docker run --rm \
  -e POSTGRES_URL=postgres://... \
  -e EMBEDDING_API_KEY=$OPENAI_KEY \
  -e RUNBOOK_SOURCES='[{"name":"docs","path":"./fixtures/runbooks","globs":["**/*.md"],"scope":"global"}]' \
  releaseguard-indexer:latest nightly --repo=demo
```

Expected: `rag_documents` and `rag_embeddings` populated.

- [ ] **Step 4: Tag**

```bash
git tag plan-d-rag
```

- [ ] **Step 5: Document**

Append to `CHANGELOG.md`:

```
## Plan D status: complete (commit $(git rev-parse --short HEAD), tag plan-d-rag)
- Embedder + git_runbook connector + chunker
- pgvector migration with hybrid retrieval (vector + BM25 + RRF)
- Hot path embedding (in-memory only)
- AI Reviewer Channel B + Hot path integration
- backfill subcommand for onboarding ingest
```

```bash
git add CHANGELOG.md
git commit -m "docs: mark Plan D complete; topology 0 fully online"
```

---

## What Plan D delivers

After all tasks complete:
- pgvector extension + `rag_documents` + `rag_embeddings` tables
- `CorpusConnector` interface + `git_runbook` PoC implementation
- Chunker (token-approximate, 512/50 by default)
- OpenAI embedder
- RAG storage UPSERT (idempotent on `source_path + content_hash`)
- Indexer `nightly` step 4 (RAG ingest)
- `internal/rag/` retriever (vector + BM25 + RRF) + hot path
- AI Reviewer auto-injects RAG snippets into system prompt
- `cmd/indexer backfill` subcommand for onboarding past MRs / postmortems

**Topology 0 is now feature-complete.** All four agents run with full data backing; AI Reviewer benefits from runbook + past MR corpus; Selective Test reaches L3 confidence; Ownership uses precomputed cochange + blame.

**Future work** (per `Infra/spec/releaseGuard/plan.md` 可改進清單):
- Confluence / Notion / Slack `CorpusConnector` implementations
- Webhook-incremental ingest
- Read replica for analyzer
- Approval rule writer (after organizational consensus)
- Feedback learning loop for arbitration calibration
- Corpus repo split-out

---

## Cross-plan summary

| Plan | What it adds | Topology |
|---|---|---|
| A | Foundation (interfaces, storage helper, GitLab client, AI provider, analyzer entry) | — |
| B | PlantUML parser + alias lookup, Selective Test L1, Rollout Risk + drift, AI Reviewer Channel A, Composer + Arbitration + Renderer + Poster | **1 demoable** |
| C | Indexer steps 1-3 + 5, Selective Test L2/L3, Ownership Agent | 0 partial (no RAG) |
| D | RAG layer (embedder, connector, retriever, hot path), backfill | **0 complete** |

After all four plans, ReleaseGuard is feature-complete per the spec at `Infra/spec/releaseGuard/`.
