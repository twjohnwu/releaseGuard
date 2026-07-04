# Task: AI Reviewer Agent

## 新 repo 路徑

`Infra/releaseGuard/`

## 目的

實作 AI 逐行 code review，**強制以 JSON 結構輸出**（透過 LLM tool use / structured output），不吐 markdown。Reviewer 不負責 post comment——只回 `AgentOutput` 給 result composer 統一發。

PoC 階段 Go-only，不做 TS mirror。

> **跨語言 schema 契約（未來擴展空間）**：PoC 只實作 Go 端，不維護其他語言。`Finding` / `AgentOutput` 的結構定義以中性 JSON Schema 形式記在規格內（見下方 schema 區塊）是為**未來**若新增 TS / Python 端 agent 保留的擴展點，現階段不是必須遵守的多端契約。`schema_version` 欄位同理——預留欄位、PoC 階段固定 `"1"`。

## 要新增的檔案

- `internal/agents/reviewer/agent.go` — 主類，實作 `IAgent`
- `internal/agents/reviewer/agent_test.go`
- `internal/agents/reviewer/prompt.go` — composer，組 base prompt（Channel A）+ RAG augment（Channel B + Hot path）
- `internal/agents/reviewer/loader.go` — 自動掃 `PROJECTS_DIR` 下 `.md` 檔（行為見下方規格）
- `internal/agents/reviewer/tokenizer.go` — `tiktoken` 包裝、token 上限截斷
- `internal/agents/reviewer/schema.go` — JSON schema for tool input
- `internal/agents/reviewer/reflection.go` — 第二輪 self-reflection（比對 JSON 結構，不比 string）
- `internal/ai/provider.go` — 介面：`CallWithTool(ctx, messages, toolSchema) (toolResult, error)`
- `internal/ai/anthropic.go` — Anthropic tool use 實作
- `internal/ai/gemini.go` — Gemini structured output (`responseSchema`)
- `internal/ai/openai.go` — OpenAI structured output (`response_format: json_schema`)
- `internal/interfaces/finding.go` — `Finding` / `AgentOutput` Go struct（schema 契約 owner）
- `internal/report/aggregator.go` — flatten findings、dedupe by stable_id、severity sort
- `internal/report/renderer.go` — Go template，渲染 ImpactScopeReport markdown

---

## Provider 介面

```go
type Provider interface {
    Name() string
    CallWithTool(ctx context.Context, sys string, user string, tool ToolSpec) (json.RawMessage, error)
}

type ToolSpec struct {
    Name        string
    Description string
    InputSchema map[string]any  // JSON schema
}
```

### Anthropic 實作要點
```json
{
  "tools": [{
    "name": "submit_review",
    "input_schema": { /* findings schema */ }
  }],
  "tool_choice": { "type": "tool", "name": "submit_review" }
}
```
回應是 `tool_use` block，取 `input` 欄位。

### OpenAI
`response_format: { type: "json_schema", json_schema: { ..., strict: true } }`

### Gemini
`generationConfig: { responseMimeType: "application/json", responseSchema: {...} }`

---

## Tool Schema（強制 LLM 必須吐這個結構）

```json
{
  "type": "object",
  "required": ["findings"],
  "properties": {
    "summary": { "type": "string" },
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["severity", "category", "title", "body"],
        "properties": {
          "severity": { "enum": ["critical","high","medium","low","info"] },
          "category": { "type": "string" },
          "title":    { "type": "string" },
          "body":     { "type": "string" },
          "location": {
            "type": "object",
            "properties": {
              "file": { "type": "string" },
              "line_start": { "type": "integer" },
              "line_end":   { "type": "integer" }
            }
          },
          "suggestion": { "type": "string" }
        }
      }
    }
  }
}
```

`stable_id` 由 agent 端計算後填入：`sha256("ai_reviewer:" + category + ":" + file + ":" + title)`。

---

## Base Prompt 載入規則（Channel A）

### RG_SERVICE_TYPE 解析

caller 透過 `.gitlab-ci.yml` job `variables:` 傳入逗號分隔 list，例如 `RG_SERVICE_TYPE=backend,frontend`。

Fallback 鏈（四層 fallback，不另設 server-side 預設變數）：

```
① system.yaml.serviceTypeOverrides[serviceName]   ← 單值，命中後直接返回，不繼續往下
② caller env (RG_SERVICE_TYPE list)
③ system.yaml.defaultServiceType                  ← 單值
④ 字面值 "backend"
```

### .md 自動掃載

自動掃指定目錄下所有 `.md` 檔，按字母排序：

```go
// pseudo
files := readdir(dir).filter(f => strings.HasSuffix(f, ".md")).sort()
```

不挑檔名、所有 `.md` 全收。新增 `FE.md` / `BE.md` / `01-priority.md` 都立即生效。

### 載入順序

```
1. _shared/*.md                            (根層、字母排序)
2. for each type in service_types:
     _shared/<type>/*.md                   (字母排序)
3. <systemName>/*.md                       (字母排序)
```

### Token 上限與截斷

拼接後用 `tiktoken`（Anthropic）/ `tiktoken` cl100k_base（OpenAI）/ Gemini SDK count tokens 計數：

```
if total_tokens > PROMPT_MAX_TOKENS:
  log warning: "prompt exceeds limit, truncating"
  保留優先級高的段落：
    - _shared/ 根層全部
    - <systemName>/review-focus.md（如存在）
  截斷後段：
    - <systemName>/architecture.md 等
    - _shared/<type>/ 下優先級低的檔
```

### RAG Fallback

兩種觸發路徑進入相同 code path（only Channel A），log 訊息不同：

| 觸發路徑 | log 訊息 |
|---|---|
| `RG_RAG_ENABLED=false`（明確設定） | `info: RAG disabled by config, reviewer using Channel A only` |
| Postgres 連線失敗（runtime fallback） | `warn: Postgres unreachable, reviewer falling back to Channel A` |
| `POSTGRES_URL` 空字串（拓樸 1） | `info: no Postgres configured (topology 1), reviewer using Channel A only` |

三種情況都：
- 跳過 Channel B（vector + BM25 query）
- 跳過 Hot path（不呼叫 embedding API）
- 只用 Channel A 跑，`status=ok`（不算 failed）

**注意**：這是 reviewer 自己的 fallback；analyzer 入口層的拓樸檢查（見 plan.md「Analyzer 入口檢查」）會更早把不合法組合擋掉。reviewer 內部只處理「合法組合下的 RAG 不可用」場景。

### Hot Path 規範

- embed 對象：本次 MR 的 `commit_messages` + `truncated_diff_summary`（diff 超過 4KB 截斷）
- 結果**只在記憶體存活到 Stage 3 結束**，不寫 Postgres
- 與 Channel B 的 vector hits 做 RRF 融合（`k=60`，業界慣例）

---

## Self-Reflection（預設 false，比對 JSON）

```
1st pass: 產生 findings JSON
2nd pass: 把 JSON + project review-focus.md 餵回 LLM
          要求回 { kept, removed, added } 三個 array
          理由必須引用 review-focus 規則
final findings = (1st.findings - removed) ∪ added
```

預設關閉（`RG_REVIEWER_SELF_REFLECTION=false`），由設為 `true` 啟用。PoC 階段保持 false 以省 token 與時間。

---

## JSON 輸出

```json
{
  "agent": "ai_reviewer",
  "status": "ok",
  "duration_ms": 4200,
  "schema_version": "1",
  "summary": "2 findings (1 high, 1 info)",
  "findings": [
    {
      "id": "ai-001",
      "stable_id": "ai_reviewer:logic_bug:orders/handler.go:Missing defer tx.Rollback",
      "severity": "high",
      "category": "logic_bug",
      "title": "Missing defer tx.Rollback() after db.Begin()",
      "body": "If error returns happen between line 142 and the explicit commit on line 195, the transaction leaks.",
      "location": { "file": "orders/handler.go", "line_start": 142, "line_end": 195 },
      "suggestion": "Add `defer tx.Rollback()` immediately after `tx, err := db.Begin()`."
    }
  ]
}
```

---

## Aggregator（render markdown）

`internal/report/renderer.go` 用 Go template，`ImpactScopeReport` 結構：

```go
type ImpactScopeReport struct {
    RiskLevel        string         // from rollout
    SelectiveTests   *TestPlan      // from selective
    Reviewers        []Reviewer     // from ownership
    AIReviewFindings []Finding      // from ai_reviewer
    HighSeverityFindings []Finding  // 跨 agent 抽出 critical/high
    AllFindings      []Finding
    EnabledFlags     map[string]bool
}
```

Template 結構：

```
## 🔴 Impact Scope Report
{{if .RiskLevel}}### Risk level: {{.RiskLevel}} ...{{end}}
{{if .SelectiveTests}}### Selective test plan ...{{end}}
{{if .Reviewers}}### Recommended reviewers ...{{end}}
---
{{if .AIReviewFindings}}<details>...AI review...</details>{{end}}
```

不啟用的 agent 對應段落不渲染（見 `task_agent_result.md`）。

---

## 驗證

### Unit
- mock Provider 回固定 JSON → 預期 agent 回 `status=ok`、findings 數量正確
- Provider 回非法 JSON → 預期重試 1 次後 `status=failed`
- Self-reflection: 1st pass 3 findings、2nd pass 移除 1 個 → final = 2 findings

### Integration
- 真實 Anthropic API key、跑一個 fixture PR
- 驗證 `agent_outputs` 有 ai_reviewer row、payload 符合 schema_version=1
- 驗證 `findings` 表有對應條目、`stable_id` 計算正確

### Schema contract
- 同一份 fixture findings JSON 用 Go struct unmarshal、再 marshal 回字串，預期與原始輸入位元級相等（schema_version=1 的契約測試）
- 未來新增其他語言 agent 時，用同一份 fixture 跨語言比對
