# Task: Indexer（離線 nightly job + 兩個內部子樹）

## 新 repo 路徑

`Infra/releaseGuard/`（PoC Go-only，不做 TS mirror）

## 目的

定期重建 ReleaseGuard analyzer 所需的所有 Postgres 資料。analyzer 端在 CI 跑時**只 SELECT 不寫**，所有寫入操作集中於 indexer。

Indexer 內部分成兩個性質完全不同的子樹：

| 子樹 | 性質 | 與 analyzer 耦合 |
|---|---|---|
| `internal/indexer/code/` | 對 git repo 做程式碼分析的預備工作 | 緊耦合（共用 `symbols/edges/coverage_map/ownership_signals` schema） |
| `internal/indexer/corpus/` | 從**異質外部來源**抓文件、embed 寫入 RAG 表 | 弱耦合（只透過 Postgres `rag_*` schema 交換） |

`corpus/` 是未來 split 出去的候選（見最末「Future Split」章節）。

---

## 目錄結構

```
Infra/releaseGuard/
├── cmd/
│   ├── analyzer/
│   └── indexer/
│       ├── main.go              ← dispatcher，解析 subcommand：nightly | backfill
│       ├── nightly.go           ← nightly 入口，串起 5 個 step
│       └── backfill.go          ← backfill 入口，重用 corpus pipeline
└── internal/indexer/
    ├── code/
    │   ├── callgraph/           ← Step 1: builder + storage
    │   ├── coverage/            ← Step 2: lcov / cobertura / gocover parser + loader
    │   ├── cochange/            ← Step 3: git log + blame + matrix
    │   └── plantuml/            ← Step 5: parser + alias resolver
    └── corpus/                  ← Step 4: split candidate
        ├── connectors/
        │   ├── connector.go     ← CorpusConnector interface（**擴展點**）
        │   ├── git_runbook.go   ← PoC 唯一實作
        │   └── (future: confluence.go, notion.go, slack.go, plantuml_connector.go)
        ├── chunker/
        ├── embedder/
        └── storage/             ← UPSERT rag_documents / rag_embeddings
```

`internal/analysis/aliases/lookup.go` 是**共用 helper**——code/plantuml 與 task_rollout_risk 的 drift detector 都會用。

---

## 5 個 Step

### Step 1 — Callgraph build（code-side）

```
for each repo:
  config = merge(global env, repos.indexer_config)
  git pull <repo>
  builder.go (cha algorithm)
  → UPSERT symbols, edges
```

`config.callgraph.include` / `exclude` 控制 builder 範圍。

### Step 2 — Coverage load（code-side）

```
artifact = read COVERAGE_ARTIFACT_PATH
parser   = select(repo.indexer_config.coverage_format ?? COVERAGE_FORMAT)
parser.Parse(artifact) → []CoverageEntry
→ UPSERT coverage_map
```

PoC 只實作 `lcov` parser；`cobertura` / `gocover` 介面預留。

### Step 3 — Co-change & blame（code-side）

```
log = git log --since=<OWNERSHIP_LOOKBACK_DAYS> --name-only --pretty=format:%H

for each commit C with files [f1..fn]:
  for each pair (fi, fj) where i != j:
    cochange[fi][fj] += 1

for each tracked file f:
  blame_weight = author_lines / total_lines
  recency_score = exp(-days / OWNERSHIP_LOOKBACK_DAYS)

→ UPSERT ownership_signals
```

### Step 5 — PlantUML 解析（code-side、新增）

只對 `DOCS_REPO_NAMES` 列出的 repo 跑：

```
files = glob(repo, ["**/*.puml", "**/*.plantuml"])

for each file:
  parsed = plantuml.Parse(file)   // 用 c4-plantuml-parser 或自寫 lexer
  for each interaction in parsed.Interactions:
    src_alias = interaction.Source     // e.g. "FE"
    dst_alias = interaction.Target     // e.g. "BE-payments"
    
    src_repo = aliases.Lookup(src_alias)   // → "web-dashboard"
    dst_repo = aliases.Lookup(dst_alias)   // → "payments-api"
    
    edge = {
      source_repo_id: src_repo?.ID (NULL if unresolved),
      target_repo_id: dst_repo?.ID (NULL if unresolved),
      source_alias, target_alias,
      call_kind: classify(interaction.Arrow),  // "->" → sync_call, "->>"" → async_event
      source_diagram_path: file
    }
    
    if either src_repo or dst_repo is unresolved:
      log warning "unresolved alias: <name> in <file>"
    
    → UPSERT cross_repo_edges
```

#### `diagram_aliases.yaml` 格式

放於 `Infra/releaseGuard/config/diagram_aliases.yaml`（baked into image）。平台團隊維護。

```yaml
# diagram_aliases.yaml
# 把 PlantUML 內 participant 名稱對應到 repos.name

aliases:
  FE:                web-dashboard
  Web:               web-dashboard
  BFF:               api-gateway
  BE-payments:       payments-api
  BE-orders:         orders-api
  BE-promo:          promo-api
  Postgres:          (skip — 不是 service)
  Redis:             (skip)

# (skip) 標示這個 alias 不對應任何 service repo，PlantUML parser 跳過此 edge 不報 warning
```

#### PlantUML DSL 範例（被解析的目標）

```plantuml
@startuml
participant FE
participant BFF
participant "BE-orders" as Orders

FE -> BFF: POST /api/v1/checkout
BFF -> Orders: gRPC CreateOrder
Orders ->> Postgres: INSERT
@enduml
```

解析結果（節錄）：
- `FE → BFF` (sync_call) → 寫 cross_repo_edges
- `BFF → Orders` (sync_call) → 寫 cross_repo_edges
- `Orders → Postgres` → alias 為 (skip)，跳過

### Step 4 — RAG ingest（corpus-side）

```
sources = repos.indexer_config.runbook_sources_override
       ?? global RUNBOOK_SOURCES

for each source:
  connector = ConnectorRegistry.Resolve(source)   // 目前 PoC 只回 git_runbook
  documents = connector.Fetch(ctx)
  
  for each doc:
    chunks  = chunker.Split(doc.Content, 512 tokens, overlap=50)
    vectors = embedder.EmbedBatch(chunks)
    repo_id = resolveScope(doc.Scope)             // global → NULL, repo:X → id
    
    → UPSERT rag_documents (source_type, source_path, repo_id, content)
    → UPSERT rag_embeddings (document_id, chunk_idx, embedding)
```

---

## Corpus Connector 介面（擴展點）

這是「資料來源擴展方案」的落地點。**新加 connector = 新增一個檔案實作介面、註冊到 dispatcher，無需改其他模組**。

```go
// internal/indexer/corpus/connectors/connector.go
type CorpusConnector interface {
    Name() string                                    // 例如 "git_runbook"
    Fetch(ctx context.Context) ([]Document, error)   // 統一輸出 Document 結構
}

type Document struct {
    SourcePath string             // <connector-name>/<relative-path>
    Content    string
    Metadata   map[string]string  // connector 自訂欄位（commit_sha、page_id...）
    Scope      string             // global | repo:<name>
    SourceType string             // runbook | mr_history | postmortem | ...
}
```

PoC 唯一實作：

```go
// internal/indexer/corpus/connectors/git_runbook.go
type GitRunbookConnector struct {
    sources []RunbookSource   // from RUNBOOK_SOURCES
}

func (c *GitRunbookConnector) Name() string { return "git_runbook" }

func (c *GitRunbookConnector) Fetch(ctx context.Context) ([]Document, error) {
    // 1. for each source.repo: git clone --depth 1
    //    for each source.path: read local
    // 2. glob filter
    // 3. wrap as Document, set SourceType = "runbook"
    // 4. return
}
```

未來其他 connector（Confluence、Notion、Slack、PlantUML 文件來源、Distributed spec provider）都實作這個介面。**這個系統設計上就是可以擴展語料來源的**——不是只能用單一文件 repo 場景。

---

## Backfill subcommand

```
cmd/indexer backfill --repo=acme/orders --since=2y --sources=mr_history,postmortem
```

- **手動觸發、不排程**：onboarding 新 repo 時跑一次
- **重用 corpus pipeline**：呼叫同樣的 chunker / embedder / storage helper，**不**改架構
- **多 source 一次跑完**：`--sources` 逗號分隔，dispatcher 找對應 connector
- **rate limit**：`EMBEDDING_RATE_LIMIT_RPM` 控制每分鐘呼叫上限，避免一次性 ingest 觸發 provider rate limit
- **idempotent**：用 `(repo_id, source_path, content_hash)` 當 unique key，重跑不重複
- **進度回報**：跑完印 `N items ingested, M skipped due to existing hash`

`mr_history` 與 `postmortem` 兩個 source type 在 PoC 實作（不像 Confluence 等需要外部 API auth）。

---

## Per-repo Override

```go
effectiveConfig := mergeConfig(globalEnv, repo.IndexerConfig)
```

- `repo.IndexerConfig.RunbookSourcesOverride` 非空 → **取代**全域 `RUNBOOK_SOURCES`（不是 merge）
- `repo.IndexerConfig.Callgraph` 非空 → **蓋過**全域 include/exclude
- `repo.IndexerConfig.CoverageFormat` 非空 → **蓋過**全域 `COVERAGE_FORMAT`

---

## Cron 排程

- 建議 `0 3 * * *`（03:00 UTC）
- 預估 5 分鐘 × `INDEXER_CONCURRENCY` 並行
- PoC 用 GitLab schedule pipeline 觸發

> **拓樸 1（無 Postgres）模式不需要跑 indexer**——沒有要灌的表。indexer 只在拓樸 0 部署時排程。

### 失敗處理

| 情境 | 處理 |
|---|---|
| 單次 nightly 失敗 | 隔天 analyzer 仍讀前一晚 snapshot，差距 ≤ 24 hr 可接受 |
| **連續失敗 > 48 hr**（snapshot 過期） | analyzer 端讀取時檢查 `symbols.indexed_at` / `coverage_map.updated_at`，若最新 row > 48 hr → log warning，受影響的 agent 降級行為：Selective Test 退到 L1、Ownership 視為「ownership_signals 空」、`mr_runs.status = partial` 註明原因 |
| 連續失敗 N=3 次 | 觸發告警（PoC 用 GitLab schedule pipeline 失敗 webhook + log 監控） |
| backfill 與 nightly 同時跑 | 安全：`(repo_id, source_path, content_hash)` 是 idempotent upsert key，並行寫入不會重複；只是 Postgres 連線數會疊加（PgBouncer 配額 25 已預留 buffer） |

---

## Hot Path 不在 indexer

明確切割：
- **Indexer 寫**：symbols / edges / coverage_map / ownership_signals / cross_repo_edges / rag_documents / rag_embeddings
- **Analyzer 寫**：mr_runs / agent_outputs / findings（per-MR 紀錄）
- **Hot path**：analyzer 對「本次 MR commit msg + diff」現場 embed，**只在記憶體**，**不寫 Postgres**

---

## Indexer 專用環境變數

| Variable | Example | Required when | Description |
|---|---|---|---|
| `POSTGRES_URL` | `postgres://rg:secret@pgbouncer.internal:6432/releaseguard` | 拓樸 0 | 與 analyzer 共用（透過 PgBouncer） |
| `RUNBOOK_SOURCES` | （JSON 字串，見 plan.md） | RAG 啟用 | 全域 runbook 來源 |
| `COVERAGE_ARTIFACT_PATH` | `s3://ci-artifacts/coverage/` | Selective Test 啟用 | indexer 撈 coverage 檔的根路徑 |
| `COVERAGE_FORMAT` | `lcov` | Selective Test 啟用 | `lcov` / `cobertura` / `gocover`（per-repo override） |
| `OWNERSHIP_LOOKBACK_DAYS` | `180` | Ownership 啟用 | git log / blame 回溯 |
| `DOCS_REPO_NAMES` | `docs/api,docs/proto` | Step 5 PlantUML | 中央 spec repo 名單 |
| `EMBEDDING_PROVIDER` | `openai` | `RG_RAG_ENABLED=true` | `openai` / `voyage` / `local` |
| `EMBEDDING_MODEL` | `text-embedding-3-small` | `RG_RAG_ENABLED=true` | 模型名（決定 vector 維度） |
| `EMBEDDING_API_KEY` | `sk-...` | `RG_RAG_ENABLED=true` | provider API key |
| `EMBEDDING_RATE_LIMIT_RPM` | `60` | backfill 啟用 | 每分鐘呼叫上限 |
| `INDEXER_CONCURRENCY` | `4` | 拓樸 0 | 並行處理 repo 上限 |

---

## Future Split（明告 schema migration 風險）

當以下任一訊號出現，建議把 `internal/indexer/corpus/` 整個搬到獨立 repo（命名建議 `releaseguard-corpus`）：

1. **第 2 個 connector 出現**（Confluence / Notion / Slack 任一進場）
2. **想改用 Python**（接 LangChain / LlamaIndex 生態）
3. **節奏分歧**：corpus pipeline 想要快速迭代、不想綁 analyzer 釋出
4. **安全隔離**：embedding key、外部 API token 要與 analyzer 隔離

### Split 後的 schema contract

- `rag_documents` / `rag_embeddings` schema 的 **owner 仍是 ReleaseGuard 主 repo**（`migrations/` 留在主 repo）
- corpus repo 只 INSERT / UPSERT，不做 schema migration
- 兩 repo 透過 Postgres 與 schema 約定耦合

### ⚠️ Schema migration 協調風險（不解、但不假裝不存在）

當主 repo 要改 `rag_documents` schema（例如加欄位）：

1. 主 repo migrate Postgres
2. corpus repo 跟上、開始用新欄位

若兩 repo 部署節奏不同步 → **短暫 schema 不一致窗口**。

PoC 不解這題（單 repo 就沒這問題），但 split 真正發生時必須回頭設計協調機制。**Candidate 解法**（非 PoC scope）：

- **Schema versioning column**：`rag_documents.schema_version`，corpus repo 寫入時帶版本，主 repo 讀取兼容多版本
- **Blue-green migration**：新增欄位 → 兼容讀寫雙寫期 → 切流 → 移除舊欄位
- **Contract test gate**：CI 跑跨 repo schema contract test，不通過不允許 deploy

---

## 驗證

### Manual smoke test

```bash
docker run --rm \
  -e POSTGRES_URL=$PG_URL \
  -e RUNBOOK_SOURCES='[{"name":"test","path":"./fixtures/runbooks","globs":["*.md"],"scope":"global"}]' \
  -e EMBEDDING_PROVIDER=openai \
  -e EMBEDDING_API_KEY=$KEY \
  releaseguard-indexer:latest \
  nightly --repo=acme/orders
```

預期 `symbols` / `edges` / `ownership_signals` / `rag_documents` / `rag_embeddings` 各有資料。

### PlantUML 驗證

1. 在一個 `DOCS_REPO_NAMES` 內的 repo 放一個 fixture `.puml` 檔
2. 確認該 repo 已註冊在 `repos` 表
3. 跑 indexer
4. 預期 `cross_repo_edges` 有資料、unresolved alias 有 warning log

### Backfill 驗證

```bash
cmd/indexer backfill --repo=acme/orders --since=1y --sources=mr_history
```

連跑兩次，第二次預期：
- 大部分 row 因 `(repo_id, source_path, content_hash)` 命中而 skip
- log 顯示 `0 ingested, N skipped`

### Cron drift 驗證

連跑 nightly 兩次，預期 `symbols.indexed_at` 更新但 row count 不翻倍（upsert 正確、idempotent）。

---

## 與其他 task 的關係

```
task_indexer
   ├─ 提供 symbols/edges    ─► task_selective_test (L3) 查
   ├─ 提供 coverage_map     ─► task_selective_test (L2) 查
   ├─ 提供 ownership_signals ─► task_ownership 查
   ├─ 提供 cross_repo_edges ─► task_rollout_risk drift detector + affected_services 查
   └─ 提供 rag_*            ─► task_ai_reviewer 查（Channel B）
```

實作建議順序（與 plan.md「Implementation Order」對齊；indexer 整體屬於拓樸 0，所有 step 都在拓樸 1 demo 完成後才上）：
1. Step 1-3 callgraph / coverage / cochange（plan.md Phase 5）
2. Step 5 PlantUML（plan.md Phase 4，提早於 Phase 5 因為不依賴 callgraph；alias helper 在 Phase 2 已抽好可直接重用）
3. Step 4 RAG ingest（git_runbook connector，plan.md Phase 6）
4. backfill subcommand（重用 Step 4 pipeline）
