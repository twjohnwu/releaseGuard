# Case Study：ReleaseGuard 實況

本文**不是架構設計**。這是 [`deploy/compose/`](../deploy/compose/) 底下的 local harness 在 `docker compose up` 跑出的實際輸出——三段 ReleaseGuard 在三種情境下會貼到 MR 的 comment，逐字取自 `artifacts/note-proj{1,2,3}-mr{1,2,3}-*.md`。

重點：**設計的核心主張「many insights → one recommendation」是可觀察的**。三份不同的 diff 在同一份 code path 跑出三種不同的 `HOLD / REVIEW / PROCEED`，沒有任何 per-fixture 的特殊分支。

程式碼狀態：tag `plan-b-topology-1`（commit [`82b7b9c`](https://github.com/twjohnwu/releaseGuard/commit/82b7b9c)），以及後續的 renderer + L1 polish。

---

## 設置

mock GitLab 依 `project_id` 提供三份不同的 fixture diff。一次 `docker compose up` 平行起三個 analyzer 實例，各自抓一份 fixture、貼回各自的 MR comment。mock 把每次的 POST 寫到 `artifacts/` 底下獨立的 markdown 檔。

| Project | Fixture | Diff 重點 | 預期觸發 |
|---|---|---|---|
| 1 | `diff-proceed.json` | 只動 `README.md` + `docs/usage.md` | 不觸發任何訊號——純文件 |
| 2 | `diff-review.json` | `internal/orders/` handler 變更 + 3 份 config（`config/feature_flags.yaml`、`helm/values.yaml`、`config/services.yaml`） | spec/code drift（handler ≠ OpenAPI）+ 3 個 medium config drift = 4 個 medium zone → MED |
| 3 | `diff-hold.json` | `config/secrets.yaml` + `internal/auth/handler.go` | critical config drift（secrets）+ spec/code drift = HIGH |

沒有任何一份 fixture 直接寫「請給我 HOLD」。決定 outcome 的是 `internal/report/arbitration.go` 裡的仲裁規則。

---

## Outcome 1 — PROCEED（純文件變更）

![PROCEED MR comment 在 GitHub 上的呈現](screenshots/proceed.png)

實際 markdown：

```markdown
## ✅ ReleaseGuard recommendation: PROCEED

---

## Impact Scope Report

### Risk level: LOW

### Selective test plan
- analysis_level: L1
- confidence: 0.40
- reason: L1 file-path mapping; only same-package tests are guaranteed
- required (1):
  - `docs/...`
- skippable: 0
```

**為何 PROCEED**：

- Rollout Risk 找出 0 個 zone（沒動 config、沒動 handler、沒有 breaking schema change）。
- Selective Test L1 看到一份純文件 diff，發出單一個 `docs/...` glob——這份 MR 沒有預期會觸發任何 Go test runner 輸出，但 agent 仍如實回報它的啟發式對 test coverage 的判斷。
- 仲裁層看到沒有 critical finding、沒有 HIGH risk、沒有 high finding、沒有 MED、沒有 agent 失敗 → 落到 default 的 `PROCEED`。

**報告上方沒有 `triggered_signals` 那一行**——這就是「此 MR 在所有訊號源上都乾淨」的提示。

---

## Outcome 2 — REVIEW（handler 變更未更新 spec + config drift）

![REVIEW MR comment 在 GitHub 上的呈現](screenshots/review.png)

實際 markdown：

```markdown
## 🟡 ReleaseGuard recommendation: REVIEW
> • [rollout_risk] med_risk: MED (4 zones, 3 config_drift, 1 spec_code_drift)

---

## Impact Scope Report

### Risk level: MED
- 🟡 medium · config_drift: config/feature_flags.yaml
- 🟡 medium · config_drift: helm/values.yaml
- 🟡 medium · config_drift: config/services.yaml
- 🟡 medium · spec_code_drift: Endpoint handler changed without updating spec

### Selective test plan
- analysis_level: L1
- confidence: 0.40
- reason: L1 file-path mapping; only same-package tests are guaranteed
- required (4):
  - `internal/orders/handler_test.go`
  - `internal/orders/...`
  - `config/...`
  - `helm/...`
- skippable: 0
```

**為何 REVIEW**：

- Rollout 的 `riskLevel(zones)` 規則是 `c≥1→HIGH; h≥2 OR m≥3→MED; else LOW`。本 MR 有 **4 個 medium zone**，滿足 `m≥3` → `MED`。
- 仲裁規則：`Rollout Risk MED → REVIEW`（不升級為 HOLD）。
- `triggered_signals` 那一行告訴 reviewer **哪一個訊號** fire 以及 **為什麼**（zone 數量 + 細項分布），不只是 outcome。
- `Risk level: MED` 接著**列出實際檔案**，MR 作者一眼就能看出「啊，我的 handler 改了但沒更新 spec，加上 helm values 也動了」。
- Selective Test L1 抓到變更的 package（`internal/orders/`）以及被動到的 config 目錄（`config/`、`helm/`）作為下游 test runner 的範圍。

這是最具產品意義的 outcome——它顯示系統**在解釋自己的推理**，不只是貼 label。

---

## Outcome 3 — HOLD（secrets 被動到 + handler drift）

![HOLD MR comment 在 GitHub 上的呈現](screenshots/hold.png)

實際 markdown：

```markdown
## 🔴 ReleaseGuard recommendation: HOLD
> 🚨 [rollout_risk] high_risk: HIGH (2 zones, 1 config_drift, 1 spec_code_drift)

---

## Impact Scope Report

### Risk level: HIGH
- 🔴 critical · config_drift: config/secrets.yaml
- 🟡 medium · spec_code_drift: Endpoint handler changed without updating spec

### Selective test plan
- analysis_level: L1
- confidence: 0.40
- reason: L1 file-path mapping; only same-package tests are guaranteed
- required (3):
  - `config/...`
  - `internal/auth/handler_test.go`
  - `internal/auth/...`
- skippable: 0
```

**為何 HOLD**：

- `config/secrets.yaml` 的檔名包含 `secret` 子字串 → `configdrift.classify` 回傳 `critical` → 1 個 critical zone。
- Rollout 規則：`c≥1 → HIGH`。
- 仲裁規則：`Rollout Risk HIGH → HOLD`。
- `🚨` 圖示與 `high_risk` kind 在 triggered_signals 行一出現，reviewer 不必看後面細節就能感知嚴重性。
- 注意：HIGH 是由 2 個 zone 驅動的，但**一個 critical zone 就足夠**——這就是規則。breakdown 那一行（`2 zones, 1 config_drift, 1 spec_code_drift`）讓這判斷可被稽核。

訊號升級是**確定性的**：單一 critical zone 就足以建議 hold merge。

---

## 這個 demo 證明了什麼

從設計可導出的三條主張在這裡都可觀察到：

### 1. Decision Arbitration Layer 正確收斂訊號

同一份 code path 在沒有 per-fixture 分支的情況下產出 PROCEED / REVIEW / HOLD。`internal/report/arbitration.go` 的規則：

- AI Reviewer critical → HOLD
- Rollout Risk HIGH → HOLD
- AI Reviewer high → REVIEW
- Rollout Risk MED → REVIEW
- Selective Test partial（non-L1）→ REVIEW
- 任一 agent 失敗 → 至少 REVIEW
- **Ownership 訊號永不觸發 gating**（決定紀錄在 [`decisions_log.md`](decisions_log.md) 第 4 與第 14 條）
- 其他 → PROCEED

### 2. Recommendation 是可稽核的

每一條 triggered_signals 與每一筆 Risk-level entry 都能回溯到：
- 哪個 agent 發出
- 屬於哪一類訊號（`high_risk` / `critical_finding` / `unstable_test_plan` 等）
- 哪一個實際檔案或 finding 是肇因

reviewer 不必翻 agent JSON 才能理解「為何 HOLD」——markdown 直接攤開。

### 3. 子分析產出結構化 zone，不是自由文字

`🟡 medium · config_drift: config/feature_flags.yaml` 這個 render 格式在三種 outcome 都一致。沒有 per-recommendation 的散文敘述——renderer 只是逐筆 iterate 收到的 zone。這就是讓系統**新增 sub-analysis 不需動仲裁與 rendering** 的關鍵。

---

## Output 中可見的限制

demo 對自己的弱點誠實：

- **`confidence: 0.40` 在三段都一樣**——Plan B 只實作 Selective Test L1，那是純檔案路徑啟發式。L1 設計上的信心上限約 0.5，混合語言再被懲罰之後就更低。L2/L3（Postgres-backed call graph + coverage map）見 [`docs/spec/superpower/plan_c_postgres_agents.md`](spec/superpower/plan_c_postgres_agents.md)。
- **AI Reviewer 在這份 demo 是關閉的**（`RG_AGENT_AI_REVIEWER_ENABLED=false`），不會打真的 Anthropic API。如果接上真 key + 填好 `PROJECTS_DIR`，reviewer 就會把它的結構化 finding 餵進同一個仲裁層。
- **Ownership Agent 不在 demo 裡**——Plan B 為「政治中性」考量刻意排除，紀錄在 `decisions_log.md` 第 4 條。Renderer 雖然有 `SuggestedReviewers` / `ImpactZones` 的 stub，但在 topology 1 維持空白。

這些限制是**設計的一部分**，不是 bug。PoC 範圍寫在 `docs/spec/plan.md` 與 `decisions_log.md` Decision #7（Topology 1）。

---

## 在本機重現

```bash
git clone https://github.com/twjohnwu/releaseGuard.git
cd releaseGuard/deploy/compose
mkdir -p artifacts
docker compose up --build --abort-on-container-exit

# 檢視三段 MR comment
for f in artifacts/note-proj*.md; do
  echo "=== $f ==="
  cat "$f"
  echo
done
```

不需 GitLab token、不需 Anthropic key、不需 Postgres。mock server 約 120 行 Go（[`deploy/compose/mock-gitlab/main.go`](../deploy/compose/mock-gitlab/main.go)），從 `mock-gitlab/fixtures/diff-{proceed,review,hold}.json` 讀預先準備的 fixture。

要產出不同結果，可以編輯 fixture（不必 rebuild——mock 每次 request 才讀檔），或為 analyzer 加新的 sub-analysis（架構支援新增 zone 來源時不必動仲裁）。

---

---

## 補充：Topology 0 端對端驗證（commit `ff5c484`）

![Topology 0 L3 MR comment](screenshots/topology0-l3.png)

前面三段 outcome 都跑在 Topology 1（zero-infra），Selective Test 只有 L1 可用，confidence 上限 0.5。本節補上**Topology 0 整合驗證**——起 Postgres、跑 indexer 對 ReleaseGuard 自己做索引、再讓 analyzer 在 T0 模式跑出帶 L3 confidence 的 MR comment。

### 準備

```bash
docker exec local-postgres psql -U postgres -c "CREATE DATABASE releaseguard_dev;"
for f in migrations/000*.sql; do
  case "$f" in *.down.sql) continue;; esac
  docker exec -i local-postgres psql -U postgres -d releaseguard_dev < "$f"
done
docker exec -i local-postgres psql -U postgres -d releaseguard_dev \
  -c "INSERT INTO repos(name) VALUES('releaseGuard') ON CONFLICT DO NOTHING;"
```

### 跑 indexer 對自己做索引 + 產生真實 coverage

```bash
# 索引 callgraph / ownership / cross-repo edges
POSTGRES_URL="postgresql://postgres:postgres@localhost:5432/releaseguard_dev" \
  REPO_CHECKOUT_DIR="$(pwd)" \
  MIGRATIONS_DIR="$(pwd)/migrations" \
  ./bin/indexer nightly --repo=releaseGuard

# 產生 per-test LCOV：bash 腳本對每個 Test* 各跑一次 go test -coverprofile，
# 經 cmd/cover2lcov 轉成 LCOV（TN= 為該 test 名）合併輸出
LCOV=$(bash scripts/gen-coverage.sh)

# 把 LCOV 餵給 indexer 的 coverage step
COVERAGE_ARTIFACT_PATH=$LCOV \
  POSTGRES_URL="postgresql://postgres:postgres@localhost:5432/releaseguard_dev" \
  REPO_CHECKOUT_DIR="$(pwd)" \
  MIGRATIONS_DIR="$(pwd)/migrations" \
  ./bin/indexer nightly --repo=releaseGuard
```

結果（`SELECT COUNT(*) FROM ...`）：

| 表 | 列數 | 來源 |
|---|---|---|
| symbols | 17,015 | callgraph step（CHA over `golang.org/x/tools`） |
| edges | 369,340 | callgraph step |
| ownership_signals | 146 | cochange step（git blame + cochange matrix） |
| coverage_map | 151（68 unique tests） | `gen-coverage.sh` + `cmd/cover2lcov`（全 repo per-test `go test -coverprofile`） |

### 跑 analyzer（T0 + 自定 fixture）

加 `mock-gitlab/fixtures/diff-t0demo.json`，內容是兩個對 `internal/agents/rollout/agent.go`、`internal/report/arbitration.go` 的虛構 diff（兩個 package 與 `gen-coverage.sh` 預設覆蓋的 `TARGETS` 一致，因此 L3 反向 BFS 能命中真實 coverage）。然後：

```bash
POSTGRES_URL="postgresql://postgres:postgres@localhost:5432/releaseguard_dev" \
  GITLAB_API_BASE="http://localhost:8080/api/v4" \
  RG_SERVICE_NAME=releaseGuard \
  RG_AGENT_OWNERSHIP_ENABLED=true \
  CI_PROJECT_ID=4 CI_MERGE_REQUEST_IID=4 \
  ./bin/analyzer
```

得到的 MR comment：

```markdown
## ✅ ReleaseGuard recommendation: PROCEED

---

## Impact Scope Report

### Risk level: LOW

### Selective test plan
- analysis_level: L3
- confidence: 0.90
- reason: L3: reverse BFS on edges
- required (28):
  - `TestAnthropicCallWithToolReturnsToolInput`
  - `TestAnthropicErrorPropagates`
  - `TestArbitrationDetailIncludesZoneBreakdown`
  - `TestArbitrationRules`
  - `TestBuildAgentsRespectsFlags`
  - … 23 more
- skippable: 0
```

`required[]` 列出的是 **真實的 Go test name**，由 `gen-coverage.sh` 對每個 test 跑 `go test -run` 後的 coverprofile 反推。L3 的反向 BFS 從 diff 觸到的 symbols（`rollout.Run`、`report.Arbitrate` 等）出發，沿 `edges` 走 3 層回頭，再 join `coverage_map.covered_symbol_id` 拿到所有覆蓋這些 symbol 的 test_id。全 repo 索引後反向 BFS 命中的 28 個 test 跨 `internal/ai`、`internal/report`、`internal/agents/*`、`cmd/analyzer` 等多個 package——印證了 callgraph 的 transitive 追溯能力。

### Topology 1 vs Topology 0 對照（同一份 diff）

| 維度 | Topology 1 | Topology 0 |
|---|---|---|
| analysis_level | L1 | **L3** |
| confidence | 0.50 | **0.90** |
| required 性質 | file glob（`internal/agents/rollout/...`） | **真實 Go test name**（`TestArbitrationRules` 等） |
| 訊號根據 | 路徑啟發 | **callgraph 反向 BFS + 真實 coverage_map** |
| 基礎建設 | 零 | Postgres + indexer（17K symbols） + per-test coverprofile |

### 過程中發現的 wiring gap

跑這次 e2e 順便撞出三個 Plan C 漏接 wiring 的真實問題（已修，commit `ff5c484`）：

1. `nodeToSymbol()` 沒填 `symbols.file` / `line_start`，導致 L3 lookup `WHERE file=ANY($)` 全失配，selective test 永遠掉到 L1
2. `ssautil.Packages` 不含 transitive stdlib 導致 SSA build panic（缺 `strings` / `context` / `fmt`）；同時 `packages.Load` Mode 漏 `NeedImports`
3. `cmd/indexer/nightly.go` 寫死 `/migrations` 路徑，container 外跑不通——加 `MIGRATIONS_DIR` env 覆蓋

這三個問題在 Plan C 23 個 task 的 unit test 都通過、但 end-to-end 就會撞出來，恰好印證 [`learnings.md`](learnings.md) 第 5 條提到的「subagent-driven 開發若沒 e2e 驗證階段，整合問題會被推到後面」——而本節就是補上那個 e2e 階段。

### 已知限制（demo 範圍內）

- **只在自己 repo 上 demo**：indexer 對非 Go 程式碼沒測過（builder interface 預留，但只實作了 GoBuilder）
- **AI Reviewer 仍關閉**：T0 demo 這段沒打 Anthropic API，與前三段一致

---

## 接著看哪裡

- [`README.md`](../README.md) — 專案頂層定位
- [`docs/architecture.md`](architecture.md) — 三張 mermaid 架構圖
- [`docs/learnings.md`](learnings.md) — 設計反思
- [`docs/decisions_log.md`](decisions_log.md) — 17 個設計轉折
- [`docs/spec/`](spec/) — 完整規格（10 份）
- [`docs/spec/superpower/`](spec/superpower/) — 實作 plan（A、B、C 已完成；D 維持 deferred）
- [`deploy/compose/README.md`](../deploy/compose/README.md) — local harness 跑法
