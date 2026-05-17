# ReleaseGuard 執行時序圖（PlantUML）

所有圖可直接貼到 https://www.plantuml.com/plantuml 渲染。

---

## 1. Overview Sequence

```plantuml
@startuml overview
skinparam sequenceMessageAlign center
actor Developer
participant GitLab
participant "CI Runner" as CI
participant Analyzer
database Postgres
participant LLM

Developer -> GitLab: push commit / open MR
GitLab -> CI: trigger pipeline (merge_request_event)
CI -> Analyzer: docker run analyzer\n(env: AGENT flags, POSTGRES_URL,\nCALLGRAPH_IMAGE, GITLAB_TOKEN)

== Stage 1: Inputs ==
Analyzer -> GitLab: GET MR diff
Analyzer -> Postgres: SELECT symbols, edges, ownership_signals
Analyzer -> Analyzer: parse go.mod / package.json

== Stage 2: RAG ==
Analyzer -> Postgres: hybrid search (vector + BM25)
Analyzer -> Analyzer: RRF fusion + per-agent prompt assembly

== Stage 3: Agents (parallel) ==
par
  Analyzer -> Analyzer: Selective Test (pure compute)
also
  Analyzer -> Analyzer: Rollout Risk (oasdiff/buf/depgraph)
also
  Analyzer -> Analyzer: Ownership (blame + cochange)
also
  Analyzer -> LLM: AI Reviewer (tool use, JSON schema)
  LLM --> Analyzer: structured findings
end

Analyzer -> Postgres: INSERT mr_runs, agent_outputs, findings

== Stage 4: Aggregation ==
Analyzer -> Analyzer: Result Composer\n(check enable flags, dedupe, sort)
Analyzer -> Analyzer: Markdown Renderer

== Stage 5: Posting (2 sinks，PoC) ==
par
  Analyzer -> GitLab: POST MR note (Impact Scope Report)
also
  Analyzer -> CI: write selective-tests.txt artifact
end
note right of Analyzer: PoC **不**呼叫 PUT /approval_rules\n（見 task_agent_result.md / task_ownership.md）

CI -> Developer: pipeline status + MR comment
@enduml
```

---

## 2. Stage 1 — Inputs Collection

```plantuml
@startuml stage1
skinparam sequenceMessageAlign center
participant Analyzer
participant GitLab
database Postgres
participant LocalRepo as "Local clone\n(in CI runner)"

== Inputs in parallel (timeout 30s) ==
par
  Analyzer -> GitLab: GET /merge_requests/:iid/diffs
  GitLab --> Analyzer: changed files + line ranges
  Analyzer -> Analyzer: parse to changed symbols\n(go/ast, ts-morph)
also
  Analyzer -> Postgres: SELECT * FROM edges\nWHERE callee IN (changed symbols)
  Postgres --> Analyzer: caller chains
also
  Analyzer -> LocalRepo: read go.mod / package.json
  Analyzer -> Analyzer: build dep graph
also
  Analyzer -> Postgres: SELECT * FROM ownership_signals\nWHERE repo_id=? AND file_path IN (...)
  Postgres --> Analyzer: blame + co-change rows
end

Analyzer -> Analyzer: assemble Stage1Output struct
@enduml
```

---

## 3. Stage 2 — Prompt Sources（Channel A + B + Hot Path）

### 3.1 Channel A — 靜態 base prompt（直讀檔）

```plantuml
@startuml stage2_channelA
participant Analyzer
participant FS as "PROJECTS_DIR\n(local filesystem)"

Analyzer -> FS: readdir _shared/*.md
Analyzer -> FS: readdir _shared/<each service_type>/*.md
Analyzer -> FS: readdir <systemName>/*.md
Analyzer -> Analyzer: sort + concat (alphabetical)
Analyzer -> Analyzer: tokenize (tiktoken)
alt token > PROMPT_MAX_TOKENS
  Analyzer -> Analyzer: truncate tail; keep _shared root + review-focus.md
  Analyzer -> Analyzer: log warning
end
@enduml
```

### 3.2 Channel B — 動態 RAG retrieval（查 Postgres）

```plantuml
@startuml stage2_channelB
participant Analyzer
database Postgres
participant Embedder as "Embedding API\n(OpenAI/Voyage/local)"

Analyzer -> Embedder: embed(query = changed symbols summary)
Embedder --> Analyzer: vector(1536)

par
  Analyzer -> Postgres: SELECT id\nFROM rag_embeddings\nWHERE doc.repo_id IS NULL OR doc.repo_id=$repo\nORDER BY embedding <=> $vec\nLIMIT 20
  Postgres --> Analyzer: vector hits
also
  Analyzer -> Postgres: SELECT id, ts_rank(...)\nFROM rag_documents\nWHERE (repo_id IS NULL OR repo_id=$repo)\n  AND content_tsv @@ plainto_tsquery($q)\nLIMIT 20
  Postgres --> Analyzer: BM25 hits
end

note over Analyzer: scope filter = global ∪ this repo
@enduml
```

### 3.3 Hot Path — 對「本次 MR 自己」現場 embed（不寫回）

```plantuml
@startuml stage2_hotpath
participant Analyzer
participant Embedder

Analyzer -> Analyzer: assemble hot_query =\ncommit_messages + truncated_diff_summary
Analyzer -> Embedder: embed(hot_query)
Embedder --> Analyzer: vector
Analyzer -> Analyzer: nearest-neighbor among Channel B vector hits
note right: Hot path 結果只在記憶體\n直到 Stage 3 結束\n**不寫回 Postgres**
@enduml
```

### 3.4 Fusion + Assembly

```plantuml
@startuml stage2_fuse
participant Analyzer

Analyzer -> Analyzer: RRF fusion (Channel B vector + BM25 + Hot path)\nscore = sum(1/(k+rank_i))
Analyzer -> Analyzer: take top-K chunks
Analyzer -> Analyzer: Context Assembler\nbuild per-agent prompt:\n  - reviewer: Channel A base + RAG augment\n  - others: only RAG augment if needed

alt Postgres unreachable
  Analyzer -> Analyzer: skip Channel B + Hot path
  Analyzer -> Analyzer: log warning, continue with Channel A only
end
@enduml
```

---

## 4. Stage 3 — Agents in Parallel

```plantuml
@startuml stage3
participant Analyzer
participant SelectAgent as "Selective Test"
participant RolloutAgent as "Rollout Risk"
participant OwnerAgent as "Ownership"
participant ReviewerAgent as "AI Reviewer"
participant LLM
database Postgres

note over Analyzer: spawn 4 goroutines / Promise.all\nrespect RG_AGENT_*_ENABLED

par
  Analyzer -> SelectAgent: run(stage1, stage2_ctx)
  SelectAgent -> Postgres: reverse BFS edges + coverage_map intersect
  SelectAgent -> SelectAgent: compute confidence
  SelectAgent --> Analyzer: AgentOutput{required, skippable, confidence}
also
  Analyzer -> RolloutAgent: run(stage1, stage2_ctx)
  par
    RolloutAgent -> RolloutAgent: oasdiff (vs git merge-base)
  also
    RolloutAgent -> RolloutAgent: buf breaking
  also
    RolloutAgent -> RolloutAgent: depgraph diff
  also
    RolloutAgent -> RolloutAgent: configdrift
  end
  RolloutAgent --> Analyzer: AgentOutput{riskLevel, zones, mitigations}
also
  Analyzer -> OwnerAgent: run(stage1, stage2_ctx)
  OwnerAgent -> Postgres: SELECT ownership_signals\nWHERE file_path IN (changed files)
  OwnerAgent -> OwnerAgent: rank by blame * recency + cochange
  OwnerAgent --> Analyzer: AgentOutput{reviewers, hotspots}
also
  Analyzer -> ReviewerAgent: run(stage1, stage2_ctx)
  ReviewerAgent -> LLM: messages + tool(submit_review schema)
  LLM --> ReviewerAgent: tool_use(findings)
  ReviewerAgent --> Analyzer: AgentOutput{findings}
end

Analyzer -> Postgres: INSERT INTO agent_outputs (4 rows)\nINSERT INTO findings (flatten)
@enduml
```

---

## 5. Stage 4 — Aggregation & Rendering

```plantuml
@startuml stage4
participant Analyzer
participant Composer as "Result Composer"
participant Renderer as "Markdown Renderer"
database Postgres

Analyzer -> Composer: compose(agent_outputs, enable_flags)

Composer -> Composer: filter outputs by enable_flags
Composer -> Composer: compute overall riskLevel\n(rollout HIGH > MED > LOW)
Composer -> Composer: dedupe findings by stable_id
Composer -> Composer: sort by severity desc

== Decision Arbitration ==
Composer -> Composer: report.Arbitrate(outputs, flags)
note right
  rules (priority order):
  - critical finding → HOLD
  - HIGH risk → HOLD
  - high finding / MED risk / unstable test plan → REVIEW
  - any agent failed → ≥REVIEW
  - ownership signals: NOT trigger (neutral)
  - L1 partial: by-design, not trigger
  fallback: panic → REVIEW + error rationale
end note
Composer -> Composer: assemble Recommendation\n(HOLD/REVIEW/PROCEED + triggered_signals)

Composer -> Postgres: UPDATE mr_runs\nSET risk_level=?, recommendation=?, status=?

Composer -> Renderer: render(ImpactScopeReport, flags)
Renderer --> Composer: markdown

Composer --> Analyzer: { markdown, ci_var, approval_users }
@enduml
```

---

## 6. Stage 5 — Posting (2 sinks，PoC)

```plantuml
@startuml stage5
participant Analyzer
participant GitLab
participant CIArtifacts as "CI artifacts dir"

note over Analyzer: 2 sinks fire in parallel,\neach honoring agent enable flags\n(approval rule sink removed in PoC —\nsee task_agent_result.md)

par
  alt any agent enabled with content
    Analyzer -> GitLab: POST /merge_requests/:iid/notes\n(Impact Scope Report markdown)
  end
also
  alt SELECTIVE_TEST enabled and confidence>=threshold
    Analyzer -> CIArtifacts: write selective-tests.txt
  else
    Analyzer -> CIArtifacts: skip (CI fallback to test-full)
  end
end

note over Analyzer: PoC **不**呼叫\nPUT /approval_rules\n— Ownership agent 的 reviewer\n用 @mention 寫進 comment，\n由 MR 作者自行邀請

Analyzer -> Analyzer: exit 0
@enduml
```

---

## 7. Failure / Timeout Flow

```plantuml
@startuml failure
participant CI
participant Analyzer
participant Postgres
participant GitLab

CI -> Analyzer: docker run (timeout=180s)

alt Stage timeout
  Analyzer -> Analyzer: ctx deadline exceeded
  Analyzer -> Postgres: UPDATE mr_runs SET status='timeout'
  Analyzer -> Analyzer: exit 1
  CI -> CI: releaseguard job fails (allow_failure=true)
  CI -> CI: trigger test-full (rules: on_failure)
else One agent fails
  Analyzer -> Postgres: agent_outputs row with status='failed'
  Analyzer -> Analyzer: continue with partial result
  Analyzer -> GitLab: post comment with "(agent failed)" placeholder
else Selective Test confidence low
  Analyzer -> Analyzer: skip writing selective-tests.txt
  Analyzer -> GitLab: comment shows "fallback to full test"
  CI -> CI: test-selective sees no file → exit 0
  CI -> CI: test-full runs as safety net
end
@enduml
```

---

## 8. Indexer Cron Flow（與 analyzer 時序對照）

```plantuml
@startuml indexer
participant Cron
participant Indexer
participant Repo as "git repo\n(call graph target)"
participant Runbook as "RUNBOOK_SOURCES\n(git or local path)"
database Postgres
participant Embedder

Cron -> Indexer: nightly trigger (e.g. 03:00 UTC)

loop for each repo in `repos` table
  Indexer -> Indexer: merge global env + repos.indexer_config

  == Step 1: Callgraph build ==
  Indexer -> Repo: git pull
  Indexer -> Indexer: builder (go_builder / ts_builder)
  Indexer -> Postgres: UPSERT symbols, edges

  == Step 2: Coverage load ==
  Indexer -> Indexer: read COVERAGE_ARTIFACT_PATH\n(s3 / local)
  Indexer -> Postgres: UPSERT coverage_map

  == Step 3: Co-change & blame ==
  Indexer -> Repo: git log --since=$LOOKBACK\ngit blame
  Indexer -> Postgres: UPSERT ownership_signals

  == Step 5: PlantUML parse (if repo in DOCS_REPO_NAMES) ==
  alt repo is a docs repo
    Indexer -> Repo: glob *.puml / *.plantuml
    Indexer -> Indexer: parse participants + interactions
    Indexer -> Indexer: lookup diagram_aliases.yaml\nalias → repos.name → repo_id
    alt all aliases resolved
      Indexer -> Postgres: UPSERT cross_repo_edges
    else some alias unresolved
      Indexer -> Postgres: UPSERT cross_repo_edges\n(repo_id = NULL for unresolved)
      Indexer -> Indexer: log warning "unresolved alias: <name>"
    end
  end

  == Step 4: RAG ingest ==
  loop for each runbook source (override or global)
    alt source.repo
      Indexer -> Runbook: git clone --depth 1 --branch <ref>
    else source.path
      Indexer -> Runbook: read local files
    end
    Indexer -> Indexer: glob filter, chunk (512 token, overlap 50)
    Indexer -> Embedder: embed(chunks)
    Embedder --> Indexer: vectors
    Indexer -> Postgres: UPSERT rag_documents (repo_id by scope)\nUPSERT rag_embeddings
  end
end

Indexer -> Cron: exit 0
@enduml
```

**時序對照**：
- 03:00 UTC indexer 跑（10~30 分鐘，視 repo 數）
- 之後一整天 analyzer 跑都 SELECT 同一份 snapshot
- Hot path 在 analyzer 端做、不影響 indexer

---

## 9. Backfill Flow（onboarding 用、手動觸發）

```plantuml
@startuml backfill
actor Operator
participant Indexer as "cmd/indexer backfill"
participant SourceRepo as "Past MR repo /\nPostmortem store"
participant RateLimiter as "rate limiter\n(EMBEDDING_RATE_LIMIT_RPM)"
participant Embedder
database Postgres

Operator -> Indexer: cmd/indexer backfill\n--repo=acme/orders --since=2y\n--sources=mr_history,postmortem

Indexer -> SourceRepo: enumerate past MRs / postmortems\nin time window
SourceRepo --> Indexer: list of items

loop for each item
  Indexer -> Indexer: chunker.split (512 token, overlap 50)
  Indexer -> RateLimiter: acquire token
  RateLimiter --> Indexer: ok (or sleep until next minute)
  Indexer -> Embedder: embed(chunks)
  Embedder --> Indexer: vectors
  Indexer -> Postgres: UPSERT rag_documents\n(idempotent key: repo_id + source_path + content_hash)\nUPSERT rag_embeddings
end

Indexer -> Operator: summary (N items ingested,\nM skipped due to existing hash)
@enduml
```

**特性**：
- **手動觸發、不排程**：onboarding 新 repo 時跑一次
- **idempotent**：用 `(repo_id, source_path, content_hash)` 當 unique key，重跑不重複
- **rate limit**：embedding API 每分鐘上限可控，避免一次性 ingest 觸發 provider 限流
- **不影響 nightly**：跑完的內容立刻可被 analyzer 查詢（同個 Postgres）
