# ReleaseGuard — 總體架構規格

## Context

ReleaseGuard 解決的是「MR 開出來後，沒人知道這次發布的真實風險範圍與該找誰看」的問題。具體痛點：

- **測試成本失控**：CI 預設全跑，沒能力區分「這個 diff 真正影響的測試 vs 沒影響的測試」，導致 review 等待時間長、CI 費用高
- **Breaking change 漏網**：API schema、依賴升級、config 變更的傳遞性風險靠人眼看 diff，常常漏
- **Reviewer 派發失準**：CODEOWNERS 是靜態檔、常 outdated，真正懂這段 code 的人 ≠ 名單上的人，且看不見「副作用區域」涉及的隱性 owner
- **Code review 沒有結構化輸出**：傳統 AI review 直接吐 markdown，無法後處理、無法跨工具聚合、無法做 metrics

ReleaseGuard 把「發布把關」變成由四個專責 agent 並行產出的**結構化決策資訊**：哪些測試必跑、這次發布會炸到哪、誰應該 review、逐行 finding。所有結果以 JSON 為主、markdown 為渲染層，落地到 MR comment 與 CI variables。

---

## 架構總覽

```
                       ┌────────────────────────────────────────┐
                       │            Input Layer                  │
                       │  ① Git diff   ② Call graph index       │
                       │  ③ Dep graph  ④ Ownership signals      │
                       └────────────────────┬───────────────────┘
                                            ▼
                       ┌────────────────────────────────────────┐
                       │   Prompt Sources（三條互補）             │
                       │   Channel A：PROJECTS_DIR 直讀 .md       │
                       │   Channel B：Postgres RAG (vector+BM25) │
                       │   Hot path：本次 MR 自身 embed (不寫回)  │
                       └────────────────────┬───────────────────┘
                                            ▼
        ┌──────────────┬──────────────┬──────────────┬──────────────┐
        ▼              ▼              ▼              ▼              │
  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐            │
  │ Selective│  │ Rollout  │  │Ownership │  │   AI     │            │
  │   Test   │  │  Risk    │  │          │  │ Reviewer │            │
  │  Agent   │  │  Agent   │  │  Agent   │  │  Agent   │            │
  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘            │
       │  JSON       │  JSON       │  JSON       │  JSON            │
       └─────────────┴───────┬─────┴─────────────┘                  │
                             ▼                                      │
                  ┌──────────────────────┐                          │
                  │ Decision Arbitration │                          │
                  │ HOLD / REVIEW /      │                          │
                  │ PROCEED + signals    │                          │
                  └──────────┬───────────┘                          │
                             ▼                                      │
                  ┌──────────────────────┐                          │
                  │  Result Composer     │ ◄── per-agent enable     │
                  │  + Markdown Renderer │     flags                │
                  └──────────┬───────────┘                          │
                             ▼                                      │
                  ┌──────────────────────┐                          │
                  ▼                      ▼                          │
              MR comment           CI variable                      │
              (兩個 sink，PoC 不寫 approval rule)                     │
                                                                    │
        Postgres (call graph · coverage · RAG · history) ◄──────────┘
```

---

## 方案 A 形態（Phase 1 PoC）

- **Analyzer**：CI 端短命 binary（Go `cmd/analyzer/main.go`），執行完即丟
- **共用 Postgres**：放 `symbols` / `edges` / `coverage_map` / `rag_*` / `mr_runs` / `agent_outputs` / `findings` / `ownership_signals`
- **Call graph index image**：`cmd/indexer` nightly 跑、產出 Docker image，內含預先建好的 SQLite/Parquet（或直接寫 Postgres，視效能而定）。CI 端拉這個 image 啟動快
- **無常駐 worker**：所有運算在 CI runner 完成

方案 B（worker fleet）、方案 C（multi-tenant SaaS）見〈可改進清單〉。

---

## 新 repo 結構（`Infra/releaseGuard/`）

```
releaseGuard/
├── cmd/
│   ├── analyzer/              # CI 端短命 entry
│   │   └── main.go
│   └── indexer/               # 離線 nightly job
│       ├── main.go
│       └── cochange.go
├── internal/
│   ├── agents/
│   │   ├── testselect/
│   │   ├── rollout/
│   │   ├── ownership/
│   │   └── reviewer/
│   ├── analysis/
│   │   ├── callgraph/         # builder.go + go_builder.go + ts_builder.go + store.go
│   │   ├── schema/            # oasdiff.go + protobuf.go
│   │   ├── depgraph/          # gomod.go + npm.go
│   │   ├── configdrift/
│   │   ├── blame/
│   │   └── cochange/
│   ├── ai/                    # provider 抽象 + Anthropic/Gemini/OpenAI
│   ├── coverage/              # CI artifact loader
│   ├── interfaces/            # Finding / AgentOutput 共用 schema
│   ├── rag/                   # indexer / retriever / assembler
│   ├── report/                # aggregator + renderer
│   ├── result/                # composer + poster
│   ├── gitlab/                # GitLab API client
│   └── config/                # env loading
├── migrations/                # Postgres schema
├── deploy/
│   ├── Dockerfile.analyzer
│   ├── Dockerfile.indexer
│   └── ci/                    # GitLab CI templates
└── go.mod
```

> PoC Go-only。TypeScript mirror（`src/`）列入可改進清單，現階段不實作。

---

## 執行流程（Stage 1~5 摘要）

詳細時序圖見 `sequenceFlow.md`。

| Stage | 摘要 | 超時 |
|---|---|---|
| 1. Inputs | 並行抓 git diff、查 callgraph index、解析 depgraph、查 ownership_signals | 30s |
| 2. RAG | 用 changed symbols 為 query → hybrid search → context assembler → per-agent prompt | 30s |
| 3. Agents | 4 agent 並行，每個呼叫 LLM（reviewer）或純運算（其他三個） | 90s |
| 4. Aggregation | findings 排序、dedupe、composer 依旗標組 ImpactScopeReport | 5s |
| 5. Posting | MR comment + CI variable 兩個 sink（PoC 不寫 approval rule） | 10s |

整體 `ANALYZE_TIMEOUT_SEC=180`。超時 → analyzer exit 1 → CI fallback 走 `test-full` track。

---

## Agent JSON 統一 Schema

所有 agent 必須輸出此結構，存進 `agent_outputs.payload`：

```typescript
interface AgentOutput {
  agent: "selective_test" | "rollout_risk" | "ownership" | "ai_reviewer";
  status: "ok" | "partial" | "failed";
  duration_ms: number;
  schema_version: "1";
  findings: Finding[];
  summary?: string;
  metadata?: object;          // agent-specific 擴充
}

interface Finding {
  id: string;                 // agent local id
  stable_id: string;          // 跨 MR dedupe key (hash of agent+category+location+title)
  severity: "critical" | "high" | "medium" | "low" | "info";
  category: string;
  title: string;
  body: string;               // markdown 允許
  location?: { file: string; line_start?: number; line_end?: number };
  suggestion?: string;
  references?: string[];
}
```

---

## Two-Track CI Pipeline

```yaml
stages:
  - analyze
  - test
  - gate

releaseguard:
  stage: analyze
  image: registry.example.com/releaseguard-analyzer:latest
  script: ./bin/analyzer
  artifacts:
    paths: [selective-tests.txt, releaseguard-result.json]
    expire_in: 1 hour
  timeout: 3 minutes
  allow_failure: true

test-selective:
  stage: test
  needs: [releaseguard]
  script:
    - if [ -f selective-tests.txt ]; then
        go test -run "$(cat selective-tests.txt)" ./...;
      else
        echo "no list, skip"; exit 0;
      fi
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'

test-full:
  stage: test
  script: go test ./...
  rules:
    - if: '$CI_COMMIT_MESSAGE =~ /\[full-test\]/'
    - when: on_failure
      needs: [releaseguard]   # releaseguard 失敗就跑 full

gate:
  stage: gate
  needs: [test-selective, test-full]
  script: 'true'
  rules: [{ when: on_success }]
```

---

## Prompt 雙通道（Channel A / B）+ Hot Path

AI Reviewer 的 prompt 來源拆成兩條通道，互補不互斥：

| Channel | 內容 | 流入方式 | 關閉時影響 |
|---|---|---|---|
| **A. 靜態基底** | `projects/_shared/*.md`、`_shared/<service_type>/*.md`、`<systemName>/*.md` | analyzer 啟動讀 `PROJECTS_DIR`，自動掃 `.md` 注入 base prompt | AI Reviewer 失去全部規則→ 退化為通用 review |
| **B. 動態 RAG** | runbook、postmortem、past MR decisions、schema 歷史 | indexer nightly 灌入 Postgres `rag_documents` / `rag_embeddings`，analyzer 只 SELECT | AI Reviewer 仍可運作（fallback only Channel A），log warning |

**Hot Path（每次 analyzer 跑時）**：
Stage 2 對「本次 MR 的 commit messages + truncated diff summary」現場呼叫一次 embedding API，與 Channel B 結果做 RRF 融合。**不寫回 Postgres**——理由：

1. MR 可能被 reject / 重寫 / 廢棄，未驗證內容會污染 corpus
2. 表規模膨脹、需要 idempotent 與清理機制
3. Hot path 與 nightly 涵蓋範圍**完全不同**（runbook、co-change、call graph、coverage、past MR decisions 都不是本次 MR 能產生的），所以「寫 Postgres 取代 nightly」是錯誤對比

後續若要把已 merge MR 加進 corpus，採 **webhook incremental**（merge 事件觸發、不是 MR 開時觸發）—— 列入可改進清單。

**載入順序**（base prompt 拼接時）：
```
_shared/*.md (根層、字母排序)
  ↓
_shared/<service_type>/*.md  ← 對 RG_SERVICE_TYPE list 中每個 type 跑一次
  ↓
<systemName>/*.md
  ↓
[拼接後若超過 PROMPT_MAX_TOKENS → 截後段；保留 _shared 根與 <system>/review-focus.md]
```

字母排序，可用 `01-` / `02-` 前綴控制。新增 `FE.md` / `BE.md` 等檔案丟對位置即生效，不需改程式。

---

## Variable Ownership 對照表

ReleaseGuard 的執行需要兩類來源的變數，**界線清楚不能混**：

| 類別 | 設定位置 | 誰擁有 | 範例 | 安全等級 |
|---|---|---|---|---|
| **Server-side 配置** | ReleaseGuard 自己的 GitLab project → CI/CD Variables（Protected + Masked），或 Docker image 啟動環境注入 | ReleaseGuard / 平台團隊 | `POSTGRES_URL`、`AI_PROVIDER_KEY`、`GITLAB_TOKEN`、`CALLGRAPH_IMAGE`、`RG_AGENT_*_ENABLED`、`RG_RAG_ENABLED`、`PROJECTS_DIR`、`PROMPT_MAX_TOKENS`、`SELECTIVE_TEST_MIN_CONFIDENCE`、`OWNERSHIP_LOOKBACK_DAYS`、`ANALYZE_TIMEOUT_SEC` | 含 secret，**caller 不可寫** |
| **Caller-provided pipeline 變數** | caller repo 的 `.gitlab-ci.yml` job `variables:` 區塊 | 各 service repo 維護者 | `RG_SERVICE_NAME`、`RG_SERVICE_TYPE` | 純識別資訊，無 secret |
| **GitLab 自動注入** | 不用設定 | GitLab Runner | `CI_PROJECT_ID`、`CI_MERGE_REQUEST_IID`、`CI_COMMIT_REF_NAME`、`CI_MERGE_REQUEST_TARGET_BRANCH_NAME` | 自動帶入 |

### 設計原則

- **Secret 不外流**：DB 連線字串、API key 永遠在 ReleaseGuard 自己的 protected variables，caller repo 完全不持有
- **拓樸決策的單一來源**：拓樸 0 / 拓樸 1 是 ReleaseGuard 部署決策，不允許 caller 覆寫
- **caller 只負責「我是誰」**：`RG_SERVICE_NAME` / `RG_SERVICE_TYPE` 是 service 識別資訊，由各 repo 自主管理

### Caller `.gitlab-ci.yml` 應該長什麼樣

```yaml
include:
  - project: 'platform/releaseguard'
    ref: main
    file: 'templates/releaseguard.yaml'

releaseguard-review:
  extends: .releaseguard-full
  variables:
    RG_SERVICE_NAME: payments-api
    RG_SERVICE_TYPE: backend
    # ❌ 不要寫 POSTGRES_URL、AI_PROVIDER_KEY、RG_AGENT_*
    # 這些都已經在 ReleaseGuard project 的 protected variables 注入
```

---

## Caller-Provided Pipeline Variables

這些變數由呼叫方 repo 的 `.gitlab-ci.yml` job `variables:` 區塊傳入，**不是** ReleaseGuard server config，不應該在 ReleaseGuard 部署環境設預設值。analyzer 收到後參與內部 fallback chain。

| Variable | 範例 | 說明 |
|---|---|---|
| `RG_SERVICE_NAME` | `payments-api` | 服務名，對應 `projects/registry.yaml` 的 key |
| `RG_SERVICE_TYPE` | `backend,frontend` | 逗號分隔 list；fallback chain：① `serviceTypeOverrides[serviceName]` → ② 此 env list → ③ `system.yaml.defaultServiceType` → ④ 字面值 `"backend"` |
| `CI_PROJECT_ID` | `123` | GitLab 自動提供 |
| `CI_MERGE_REQUEST_IID` | `45` | GitLab 自動提供 |
| `CI_COMMIT_REF_NAME` | `feat/orders-rewrite` | source branch（GitLab 自動提供） |
| `CI_MERGE_REQUEST_TARGET_BRANCH_NAME` | `main` | target branch（GitLab 自動提供） |

caller `.gitlab-ci.yml` 範例：

```yaml
ai-review:
  extends: .releaseguard-full
  variables:
    RG_SERVICE_NAME: payments-api
    RG_SERVICE_TYPE: backend,frontend
```

---

## 環境變數清單

| Variable | Example | Description |
|---|---|---|
| `RG_AGENT_SELECTIVE_TEST_ENABLED` | `true` | 啟用 Selective Test agent（預設 true） |
| `RG_AGENT_ROLLOUT_RISK_ENABLED` | `true` | 啟用 Rollout Risk agent（預設 true） |
| `RG_AGENT_OWNERSHIP_ENABLED` | `true` | 啟用 Ownership agent（預設 true） |
| `RG_AGENT_AI_REVIEWER_ENABLED` | `true` | 啟用 AI Reviewer agent（預設 true） |
| `POSTGRES_URL` | `postgres://rg:secret@pgbouncer.internal:6432/releaseguard` | 共用 Postgres（透過 PgBouncer，port 6432）。**拓樸 1（無 Postgres）可留空**——analyzer 啟動時偵測為空字串、且僅啟用無 DB 依賴的 agent 組合 → 進入無 DB 模式 |
| `CALLGRAPH_IMAGE` | `registry.example.com/callgraph-index:2026-05-09` | 預打包的 call graph index image，由 indexer nightly 產出 |
| `AI_PROVIDER` | `anthropic` | `anthropic` / `gemini` / `openai` |
| `AI_PROVIDER_KEY` | `sk-ant-...` | 對應 provider 的 API key |
| `GITLAB_TOKEN` | `glpat-xxxx` | `api` + `write_repository` |
| `OWNERSHIP_LOOKBACK_DAYS` | `180` | git blame 與 co-change 回溯天數 |
| `ANALYZE_TIMEOUT_SEC` | `180` | analyzer 整體超時 |
| `SELECTIVE_TEST_MIN_CONFIDENCE` | `0.85` | 低於此值退回 full test |
| `RG_REVIEWER_SELF_REFLECTION` | `false` | AI Reviewer 第二輪 self-reflection 開關（PoC 預設關閉省成本） |
| `PROJECTS_DIR` | `/app/projects` | analyzer 啟動時直讀的靜態 prompt 目錄（Channel A） |
| `RG_RAG_ENABLED` | `true` | 啟用 RAG（Channel B + Hot path）。`false` 時 AI Reviewer 只用 Channel A、不呼叫 embedding API |
| `RUNBOOK_SOURCES` | （JSON 字串，見下方範例） | 動態 RAG 來源（Channel B）的設定。indexer 用，analyzer 不用 |
| `PROMPT_MAX_TOKENS` | `8000` | base prompt 拼接後的 token 上限，超過警告並截斷後段 |
| `COVERAGE_FORMAT` | `lcov` | `lcov` / `cobertura` / `gocover`。indexer 端 coverage parser 預設用此值，per-repo 可在 `repos.indexer_config.coverage_format` 覆寫 |
| `DOCS_REPO_NAMES` | `docs/api,docs/proto` | 中央 spec repo 名單（逗號分隔）。drift detector 與 PlantUML parser 用：標出哪些 repo 持有跨服務的 OpenAPI / Protobuf / `.puml` 檔 |
| `EMBEDDING_RATE_LIMIT_RPM` | `60` | backfill subcommand 對 embedding API 的每分鐘呼叫上限，避免一次性 ingest 觸發 provider rate limit |

> `RG_SERVICE_TYPE` 與 `RG_SERVICE_NAME` 不在此表——它們是 **caller-provided pipeline variables**（見下方專屬小節），不是 ReleaseGuard server config。

`AI_REVIEW_METRICS_FILE` 從規範中明確排除——telemetry 由 Postgres `mr_runs` / `agent_outputs` 取代。PoC 階段 log 一律 debug，不設 `LOG_LEVEL`。

### `RUNBOOK_SOURCES` 範例

```json
[
  {
    "name": "ops-runbooks",
    "repo": "git@gitlab.example.com:platform/runbooks.git",
    "ref": "main",
    "globs": ["**/*.md"],
    "scope": "global"
  },
  {
    "name": "orders-postmortems",
    "repo": "git@gitlab.example.com:team-orders/postmortems.git",
    "ref": "main",
    "globs": ["incidents/**/*.md", "lessons/**/*.md"],
    "scope": "repo:acme/orders"
  },
  {
    "name": "shared-wiki",
    "path": "/mnt/shared/wiki",
    "globs": ["*.md", "policies/*.md"],
    "scope": "global"
  }
]
```

各欄位說明：

| Key | Required | Type | 說明 |
|---|---|---|---|
| `name` | ✓ | string | 來源識別名稱，寫入 `rag_documents.source_path` 前綴，便於追蹤 |
| `repo` | △ | string | git remote URL；與 `path` 二擇一。indexer 會 `git clone --depth 1 --branch <ref>` 到暫存目錄 |
| `ref` | △ | string | git ref（branch / tag / sha）。`repo` 形式時必填，預設 `main` |
| `path` | △ | string | 本地檔案系統路徑（NFS / shared volume）；與 `repo` 二擇一，indexer 直接讀檔 |
| `globs` | ✓ | string[] | 從來源根目錄篩檔的 glob，可多筆 |
| `scope` | ✓ | string | `global` 或 `repo:<repos.name>`。決定該 source ingest 後的可見性 |

`scope` 行為對應：

| Scope 值 | 寫入 `rag_documents.repo_id` | 哪個 analyzer 查得到 |
|---|---|---|
| `global` | `NULL` | 全部 repo 的 analyzer |
| `repo:acme/orders` | 對應 `repos.name='acme/orders'` 的 id | 僅該 repo 的 analyzer |

Per-repo override：在 `repos.indexer_config.runbook_sources_override` 內可放同樣 schema 的 list，indexer 處理該 repo 時用 override 取代全域 `RUNBOOK_SOURCES`。

---

## Postgres 並發策略

PoC 接 SaaS 規模時，多 repo 並發峰值可達 50-150 個連線（每 MR 開 analyzer，再加上 indexer / backfill）。Postgres 預設 `max_connections=100` 撐不住，**必須走 PgBouncer**。

| 元件 | 連線預估 | 用途 | Pool mode |
|---|---|---|---|
| analyzer（per MR pipeline） | 2~3 個 | Stage 1 Postgres queries（symbols/edges/ownership_signals）+ Stage 4 寫入（mr_runs/agent_outputs/findings） | transaction |
| indexer nightly | 4~8 個 | 並行處理 repo（`INDEXER_CONCURRENCY`）× 每 repo 1~2 連線 | session（需要 long-running tx for upsert batch） |
| backfill | 2~4 個 | 單 repo 集中寫入，rate limit 主要在 embedding API 端 | session |
| Hot path embedding fetch | 0 個 | 不查 Postgres（只用 Channel B 結果做 fusion） | N/A |

### 設定建議

- **PgBouncer pool size**：`max_client_conn=200`、`default_pool_size=25`（Postgres 端）
- **PoC 階段 Postgres**：`max_connections=100` 維持預設即可（PgBouncer multiplexing 撐住）
- **Query timeout**：analyzer 端設 `5s`（讀）/ `10s`（寫）；indexer 端設 `60s`（大 batch upsert）
- **Read replica**：PoC 不做。未來若 analyzer 並發超過 PgBouncer 處理上限，把 SELECT-only query 路由到 replica（見可改進清單）

### 注意

- analyzer 用 transaction pool mode 時**不能**用 prepared statement / session-level state（PgBouncer transaction mode 限制）
- `LISTEN/NOTIFY` 不可用（同上）— PoC 沒用到，未來 webhook incremental ingest 需考慮

---

## Topology Modes

ReleaseGuard 不為個別 agent 寫「有/無 Postgres」分支邏輯。而是**用既有旗標組合**支援不同部署複雜度，由 analyzer 入口統一檢查。

### 拓樸 0：Full（預設方案 A）

所有 4 agent + RAG + Postgres + nightly indexer。

### 拓樸 1：Script-only（無 Postgres、零基礎設施）

只跑 Rollout Risk + AI Reviewer（Channel A only）。零基礎設施依賴——CI runner 拉 image 跑、結束即丟，不需 Postgres、不需 indexer cron、不需 embedding API。

旗標組合：

```
POSTGRES_URL=                            ← 留空
RG_AGENT_SELECTIVE_TEST_ENABLED=false    ← 必須關（依賴 symbols / coverage_map）
RG_AGENT_OWNERSHIP_ENABLED=false         ← 必須關（依賴 ownership_signals）
RG_AGENT_ROLLOUT_RISK_ENABLED=true       ← 純 git/CLI 操作
RG_AGENT_AI_REVIEWER_ENABLED=true
RG_RAG_ENABLED=false                     ← 必須關（依賴 rag_*）
```

**仍然可用的功能**：

| 類別 | 功能 |
|---|---|
| AI Reviewer | 逐行 review、JSON 結構化輸出、self-reflection、自動掃 `.md` prompt、多 LLM provider |
| Rollout Risk | breaking API 偵測（OpenAPI/Protobuf）、依賴升降版風險、config drift、HIGH/MED/LOW risk 標籤 |
| Result | Impact Scope Report 渲染、MR comment 發布 |

**失去的能力（明告使用者）**：

- Selective Test（無 call graph index）
- Ownership 智能推薦（無 co-change matrix）+ approval rules 自動寫入
- RAG augmentation（runbook、past MR 經驗不可用）
- 跨 MR finding dedupe（每個 MR 都會看到重複的同一個問題）
- Telemetry / metrics（`mr_runs` 不寫入 → 無歷史可分析）

### Analyzer 入口檢查（在 `cmd/analyzer/main.go` startup）

```
if POSTGRES_URL is empty:
  if any of (selective_test, ownership) enabled:
    fatal "Selective Test / Ownership require POSTGRES_URL"
  if RG_RAG_ENABLED:
    fatal "RAG requires POSTGRES_URL"
  → 進入無 DB 模式：跳過所有 mr_runs / agent_outputs / findings 寫入
```

**不允許**「Postgres 在但 partial」之類的中間狀態——要麼有完整 Postgres（拓樸 0）、要麼完全沒有（拓樸 1）。

### 拓樸 1 的 Two-Track CI 範例

```yaml
releaseguard-lite:
  stage: analyze
  image: registry.example.com/releaseguard-analyzer:latest
  variables:
    RG_AGENT_SELECTIVE_TEST_ENABLED: "false"
    RG_AGENT_OWNERSHIP_ENABLED: "false"
    RG_RAG_ENABLED: "false"
    POSTGRES_URL: ""
  script: ./bin/analyzer
  allow_failure: true
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
```

---

## Scope Discipline（PoC 範圍切割）

完整規格定義了拓樸 0（含 Postgres / Indexer / RAG）與拓樸 1（零基礎設施）兩個部署模式，但**目前 PoC 主軸只做拓樸 1**——對應 `superpower/` 內的 Plan A + Plan B。

| 模式 | 對應 Plan | 狀態 |
|---|---|---|
| 拓樸 1（零基礎設施輕量啟動） | Plan A + Plan B | ✅ 已實作（tag `plan-b-topology-1`） |
| 拓樸 0（完整 Postgres + Indexer） | Plan A + B + C | ✅ 已實作（tag `plan-c-postgres-agents`） |
| 拓樸 0 + RAG 完整 | Plan A + B + C + D | 🟡 Deferred Roadmap |

Plan C 完成後新增的能力：
- Selective Test L2（coverage_map 交集，confidence 0.6~0.85）+ L3（call graph 反向 BFS，confidence 0.85~0.95）
- Ownership Agent 提示模式（中性輸出、`suggested_reviewers` shuffled、無 score / kind / approval_rule）
- Indexer 5 step pipeline：callgraph、coverage、cochange、plantuml、（RAG ingest 留待 Plan D）

Plan D（RAG corpus + backfill）仍 deferred，作為未來擴展保留。

---

## Development Principles

實作 ReleaseGuard 各 agent / 工具時必須遵守：

### SOLID

- **Single Responsibility**：每個 agent 一個職責（`testselect` / `rollout` / `ownership` / `reviewer`）。Composer / Poster / Renderer 三個角色各司其職
- **Open/Closed**：`Provider`、`Builder`、`Embedder`、`IDiffFetcher` 等介面允許新增類型不改舊 code（已寫進 task_ai_reviewer / task_indexer）
- **Liskov**：任一 LLM provider 必須完全可替換、不洩漏 provider 特定行為到 agent 層
- **Interface Segregation**：`IAgent` / `IAIProvider` / `IGitLabClient` / `IEmbedder` / `ICallgraphBuilder` 等小介面，不堆積巨型介面
- **Dependency Inversion**：`cmd/analyzer/main.go` 是唯一 composition root，agents 只持有 interface 引用、不 import 具體 type

### DRY

- `Finding` / `AgentOutput` 介面在 Go 端定義一次（PoC Go-only）；`schema_version` 欄位是**未來**新增其他語言 agent 時保留的擴展點，現階段固定 `"1"`
- Postgres upsert helper 共用（`internal/indexer/storage/upsert.go`）
- JSON → markdown 渲染集中於 `internal/report/renderer.go`，不在各 agent 內各自做
- prompt 載入 / token 計數共用 helper（`internal/agents/reviewer/loader.go`、`tokenizer.go`）
- Provider 重試 / 指數退避包在 `internal/ai/retry.go` 共用，不在每個 provider 實作裡複製

### Testing

- 每個介面實作必須附 mock 與 table-driven test
- Cross-language consistency test：同一份 fixture diff 餵 Go 與 TS 兩端，比對 `findings[].stable_id` 一致

---

## Known Limitations

PoC 階段以下四個技術限制無解、必須在規格與輸出明告，避免使用者誤判：

### 1. Interface dispatch 不準（影響 Selective Test L3）

Go 的 interface 是動態 dispatch：`var s Storer; s.Save()` 在 AST 階段看不出 runtime 會呼叫哪個實作。在大量使用 interface 的 codebase 裡 L3 call graph 會出現以下問題：

- **保守策略**：遇到 `kind=interface` 邊 → 把 `Save` method 的所有已知實作全部視為可能 callee
- **代價**：required test list 會膨脹（false positive，多跑了一些不必要的 test）
- **替代不採用**：放棄保守、信任靜態 graph → 會 false negative（漏跑該跑的 test，發布後才炸）

L3 的 confidence 計算會把 interface ratio 納入：interface 邊比例 > 30% → confidence -= 0.3。輸出 finding 的 `reason` 欄位明告。

### 2. Coverage map 新鮮度 lag

`coverage_map` 由 indexer nightly 從 CI artifact 寫入。後果：

- 今天新增一個 test 並 push，**今天**開的 MR 看不到它
- L2 Selective Test 會把這個新 test 誤判為 skippable
- 隔天 indexer 跑完才看得到

**緩解**：confidence 計算對 `coverage_map.last_seen_sha < HEAD` 的 entry 降權。**未來解法**（可改進清單）：webhook incremental，merge 後立刻重算受影響的 coverage entry。

### 3. Arbitration 邊界基於 LLM 標籤、會漂移

`HOLD` vs `REVIEW` 取決於 AI Reviewer 把 finding 標 `critical` 還是 `high`，而這個分類由 LLM 決定、prompt 換一次邊界就漂。

- **若 HOLD 過於頻繁**：使用者學會忽略「狼來了」、信任崩
- **若 HOLD 過於稀有**：嚴重問題沒被即時警示

PoC 階段沒有自動校準機制——需要靠 feedback loop 與長期觀察 dashboard 來調整 prompt 或 severity 規則（已加入可改進清單「Arbitration calibration」）。

### 4. Spec/Code Drift Detector 不對稱

Rollout Risk 的 drift detector 只能偵測「**code 改了、spec 沒改**」這個方向：

- ✅ 能偵測：service repo 內 endpoint handler 函式改了，但對應 OpenAPI 沒更新 → warning
- ❌ 偵測不到：spec 改了但 code 還沒實作（spec 領先 code）

理由：偵測「spec 領先 code」需要從 spec 反查實作位置，且要排除「故意先寫 spec 再實作」的合理情境，誤報率太高。Spec 改完沒人實作這個風險靠別的機制（issue tracker、release checklist）兜。

---

## 可改進清單

### 架構升級
- **方案 B**：常駐 worker fleet + webhook trigger，去除 CI cold start
- **方案 C**：multi-tenant SaaS、Auth、Billing、Dashboard
- **API server 架構**：常駐 HTTP API + queue + worker pool（中期方向；短期 PgBouncer 夠）
- **Postgres read replica**：SELECT-only 查詢路由到 replica，PgBouncer 上限拉高

### Corpus / RAG 擴展
- **Corpus repo split out**：`internal/indexer/corpus/` 整個搬到獨立 repo（觸發訊號：第 2 個 connector 出現、想用 Python、不同節奏、安全隔離）
- **Confluence / Notion / Slack corpus connector**：新增 `CorpusConnector` 介面實作
- **RUNBOOK_SOURCES 動態 ingest**：webhook 觸發、不等 nightly
- **Per-repo runbook override 啟用**：`repos.indexer_config.runbook_sources_override`（schema 已預留）
- **Webhook incremental ingest**：merge 後立即把已 merge MR 寫入 RAG corpus
- **Past MR decisions corpus**：實作 `mr_history` source type 在 nightly 流程

### 程式分析升級
- **動態語言支援**：Python / Ruby / JS 用 import graph + runtime coverage 取代 static call graph
- **Cross-repo co-change matrix**：需多 repo git history 存取權限，先確認 token 權限再做
- **外部 schema registry**：Rollout Risk 從 OpenAPI / Protobuf central registry 取基準
- **Spec-leads-code drift detection**：偵測 spec 改了但 code 沒實作的反向 drift

### Ownership 升級（需組織共識才開啟）
- **GitLab approval rule writer**：自動寫 `PUT /merge_requests/:iid/approval_rules`，把推薦的 reviewer 設為 required approver。需先有組織層級的 reviewer 政策共識
- **Reviewer ranking 暴露**：把內部 score 翻譯成可見的排序標籤（例如「主要維護者」），需要先有 feedback loop 與 opt-in 機制避免造成負面組織信號
- **Score-based MR comment**：若組織文化接受可量化的 expertise，把 `score` 暴露在 MR comment 中協助快速判斷
- **Ownership intelligence 對外定位升級**：從「提示」改成「智能推薦」（產品語言升級，預設 PoC 是中性「help author understand impact」定位）

### Arbitration 校準（PoC 後）
- **Arbitration calibration via feedback loop**：HOLD/REVIEW 邊界基於 LLM categorical 標籤（critical / high），prompt 換一次邊界就漂；需要長期蒐集 reviewer 採納/忽略訊號重新校準
- **Arbitration A/B testing**：對同一 MR 平行跑兩套規則、比對 reviewer 行為
- **HOLD/REVIEW 頻率監控 dashboard**：避免「永遠 HOLD」（信任崩）或「永遠 PROCEED」（系統失能）

### 信心 / 學習
- **Confidence-based gating 進階版**：partial 狀態下隨機抽 N% skippable 跑（safety net）
- **History-driven confidence**：用過往採納 / 忽略歷史訓練 confidence 模型
- **Feedback learning loop**：MR comment 中的 reviewer 採納 / 忽略訊號回灌 confidence model
- **Sandbox validation**：對 high-confidence finding 在 sandbox 跑驗證 case

### 拓樸與 fallback
- **拓樸 1.5**：給 Selective Test cold-parse fallback、Ownership on-the-fly co-change，無 Postgres 仍能跑全 4 agent

---

## 驗證計畫（end-to-end）

1. 在一個註冊 repo（例如 `acme/orders`）上開一個 PR fixture，改動範圍限於某一模組
2. nightly indexer 已先建好該 repo 的 call graph index image
3. CI 觸發 analyzer，預期：
   - Selective Test 列出 ≤ 10 個必跑 test、confidence ≥ 0.9
   - Rollout Risk 為 LOW（無 schema、無 dep 變更）
   - Ownership 提示對應檔案的相關背景人員
   - AI Reviewer 提出至少 1 個結構化 finding
4. MR 上出現完整 Impact Scope Report
5. 切換 `RG_AGENT_*_ENABLED=false` 各組合，驗證 comment / CI variable 對應段落消失（詳見 `task_agent_result.md`）

---

## Implementation Order

實作建議順序，優先以 **ROI 高、依賴少** 為原則：

| Phase | 內容 | 依賴 | Status |
|---|---|---|---|
| 1 | Selective Test L1（純 file path / package 對應） | 無 DB 依賴 | ✅ In Scope（Plan B） |
| 2 | Rollout Risk 四條子分析（含 spec/code drift detector） | 無 DB 依賴 | ✅ In Scope（Plan B） |
| 3 | AI Reviewer（Channel A only 先行） | `PROJECTS_DIR` | ✅ In Scope（Plan B） |
| 4 | PlantUML parser + `diagram_aliases.yaml` + `cross_repo_edges` 表 | Postgres | ✅ In Scope（Plan B 提供 alias lookup helper；cross_repo_edges schema 在 Plan B migration，但實際填寫由 Plan C indexer 做） |
| 5 | Indexer Step 1-3（callgraph / coverage / cochange） | Postgres + PgBouncer | ✅ In Scope（Plan C） |
| 6 | RAG ingest（Step 4）+ backfill subcommand | Embedding API + Postgres | 🟡 Deferred（Plan D） |
| 7 | Selective Test L2 / L3、Ownership Agent（提示模式、無 approval rule） | Phase 5 完成 | ✅ In Scope（Plan C） |
| 8 | Result composer / poster + Decision Arbitration Layer + 16 旗標組合 × arbitration 規則矩陣驗證 | 上述全部 | ✅ In Scope（Plan B 核心） |

**關鍵依賴鏈**：
- Phase 1-3 完成 → **拓樸 1 可以 demo**（零基礎設施輕量啟動，純 git/CLI + AI Reviewer Channel A）
- Phase 4-6 完成 → 拓樸 0 完整功能上線
- Phase 7-8 完成 → 全功能 PoC 收斂

**返工風險點**：Phase 2 的 spec/code drift detector 與 Phase 4 的 PlantUML parser 都會用到 `diagram_aliases.yaml`——Phase 2 實作 drift detector 時必須先把 alias 反查邏輯抽成共用 helper（`internal/analysis/aliases/lookup.go`），Phase 4 才能直接重用。Phase 2 在無 DB 環境下，`affected_services` 為空陣列；Phase 4 接上 `cross_repo_edges` 後才有實際內容。
