# Task: Selective Test Agent（三層架構）

## 新 repo 路徑

`Infra/releaseGuard/`（PoC 階段 Go-only，不做 TS mirror）

## 目的

接收 git diff，輸出「這次 MR 真正必須跑的 test」與「可安全跳過的 test」，附 `analysis_level` + `confidence`。CI 端據此寫入 `selective-tests.txt`，供 `test-selective` job 取用。Confidence 低時 short-circuit，由 `test-full` track 兜底。

---

## 三層架構

不是 all-or-nothing。**三個層次**漸進提升準確度，每層需要的基礎設施不同：

| Level | 依賴 | confidence 範圍 | 拓樸 1 可用？ | 邏輯 |
|---|---|---|---|---|
| **L1** | 純 git diff，無 DB | 0.2 ~ 0.5 | ✅ | 改了 `foo/bar.go` → 找 `foo/bar_test.go` 與同 package 內 test |
| **L2** | `coverage_map`（Postgres） | 0.6 ~ 0.85 | ❌ 需 Postgres | 改了 file f → 在 `coverage_map` 找哪些 test 覆蓋過 f |
| **L3** | `coverage_map` + `edges`（call graph） | 0.85 ~ 0.95 | ❌ 需 Postgres + nightly call graph | 反向 BFS on `edges` → 與 `coverage_map` 取交集 |

**Agent 自動依賴可用度選擇 level**：
- 拓樸 1（無 Postgres）→ 永遠 L1
- 拓樸 0、call graph 完整 → L3
- 拓樸 0、call graph 缺漏（dynamic ratio > 30%）→ 退到 L2

每個 finding 的 metadata 帶 `analysis_level` + `confidence` + `reason`，使用者一眼知道是哪層算的。

---

## 要新增的檔案

- `internal/agents/testselect/agent.go` — 主類，依賴可用度選 level、實作 `IAgent`
- `internal/agents/testselect/agent_test.go` — table-driven tests（L1/L2/L3 各一組）
- `internal/agents/testselect/level_l1.go` — file path / package 模式比對
- `internal/agents/testselect/level_l2.go` — coverage_map intersect
- `internal/agents/testselect/level_l3.go` — call graph reverse BFS
- `internal/agents/testselect/confidence.go` — confidence 計算（含 dynamic ratio penalty）
- `internal/analysis/callgraph/builder.go` — `Builder` 介面（給 indexer 用）
- `internal/analysis/callgraph/go_builder.go` — `golang.org/x/tools/go/callgraph/cha`
- `internal/analysis/callgraph/store.go` — Postgres 查詢（reverse BFS、coverage intersect）
- `internal/coverage/parser/parser.go` — adapter 介面
- `internal/coverage/parser/lcov.go` — PoC 唯一實作
- `internal/coverage/parser/cobertura.go` — 介面預留
- `internal/coverage/parser/gocover.go` — 介面預留
- `internal/coverage/loader.go` — indexer 端讀 CI artifact 寫入 `coverage_map`

---

## L1 演算法（純 file path 模式）

```
input: changed_files = ["foo/bar.go", "foo/baz.go", "qux/main.go"]

for each file in changed_files:
  if file ends with "_test.go": treat as test → 自己加進 required
  else:
    # 同檔對應 test
    test_path = strings.Replace(file, ".go", "_test.go", 1)
    if test_path exists: required.add(test_path)
    
    # 同 package 內所有 test
    pkg_dir = filepath.Dir(file)
    for f in glob(pkg_dir + "/*_test.go"):
      required.add(f)

confidence = 0.5
if any non-Go file in changed_files: confidence -= 0.2  # JS/TS/proto 等
if changed_files 跨多個 top-level dir: confidence -= 0.1
```

L1 的弱點：完全看不到「A pkg 改了、B pkg 的 test 會炸」這種跨 package 影響。明白告訴使用者「這層只能保證同 package 內的測試覆蓋」。

---

## L2 演算法（coverage_map intersect）

```
input: changed_files

required_tests = SELECT DISTINCT test_id
                 FROM coverage_map
                 WHERE repo_id = ?
                   AND covered_symbol_id IN (
                     SELECT id FROM symbols WHERE file IN changed_files
                   )

confidence = 0.75
# 對 stale entry 降權
stale_count = count(coverage_map where last_seen_sha < HEAD)
if stale_count / total > 0.2: confidence -= 0.15
```

L2 的弱點：coverage_map 是 nightly 快照，今天新加的 test 還沒進去。

---

## L3 演算法（call graph reverse BFS）

```
input: changed_symbols (parsed from diff via go/ast)

# Reverse BFS on edges, depth limit 3
WITH RECURSIVE upstream(sym, depth) AS (
  SELECT id, 0 FROM unnest(changed_symbols)
  UNION
  SELECT e.caller, u.depth + 1
  FROM edges e JOIN upstream u ON e.callee = u.sym
  WHERE u.depth < 3
)
SELECT DISTINCT cm.test_id
FROM coverage_map cm
JOIN upstream u ON cm.covered_symbol_id = u.sym
WHERE cm.repo_id = ?

# Confidence
base = 0.9
dynamic_ratio = count(edges where kind='dynamic' and callee in upstream)
              / count(edges where callee in upstream)
if dynamic_ratio > 0.3: base -= 0.3   # interface dispatch 保守展開的 caveat
if any changed_symbol not in symbols table: base -= 0.2
confidence = max(0, base)

if confidence < 0.6: 退到 L2
```

**退級行為**：當 L3 退到 L2，confidence **重新用 L2 公式計算**（`base = 0.75`，stale ratio 降權），不繼承 L3 的低分。`metadata.fallback_chain = ["L3", "L2"]`，`metadata.analysis_level = "L2"`，`metadata.reason` 註明「L3 dynamic_ratio too high, fell back to L2」。

L2 退級後若 confidence 仍 < `SELECTIVE_TEST_MIN_CONFIDENCE` → 不寫 `selective-tests.txt`、`status=partial`、CI 走 `test-full`。

L3 的 caveat 在 plan.md「Known Limitations」明告：interface dispatch 不準。

---

## JSON 輸出

```json
{
  "agent": "selective_test",
  "status": "ok",
  "duration_ms": 432,
  "schema_version": "1",
  "summary": "7 of 50 tests required (L3, confidence 0.92)",
  "findings": [],
  "metadata": {
    "analysis_level": "L3",
    "confidence": 0.92,
    "required": ["TestOrderCreate", "TestCheckoutFlow", "..."],
    "skippable": ["TestUserProfile", "..."],
    "reason": "diff touches 2 symbols; reached by 7 tests via 2-hop callgraph; dynamic_ratio=0.04",
    "fallback_chain": ["L3"]
  }
}
```

`fallback_chain` 記錄 agent 嘗試過哪些 level（如 `["L3", "L2"]` 表示 L3 confidence 太低退到 L2）。

---

## CI Variable / Artifact 寫入

```
selective-tests.txt 格式（| 分隔、正則）：
TestOrderCreate|TestCheckoutFlow|TestPromoApplication
```

供 `go test -run "$(cat selective-tests.txt)" ./...` 直接使用。

如果 `confidence < SELECTIVE_TEST_MIN_CONFIDENCE`（預設 0.85）或 agent disabled → **不**寫此檔，CI 的 `test-full` 接手。

---

## 與 Arbitration 的關係（避免拓樸 1 spam REVIEW）

Arbitration（見 `task_agent_result.md`）會把 `status=partial` 視為訊號之一觸發 REVIEW recommendation。但這只適用於「**因資料品質問題**導致的 partial」，不包括「**by-design 在拓樸 1 跑 L1**」的情境。

具體區分：

| 情境 | `analysis_level` | `status` | 觸發 arbitration unstable_test_plan? |
|---|---|---|---|
| 拓樸 1，L1 跑出來 confidence 0.5 | `L1` | `partial`（confidence 偏低，但這是 L1 設計上的本質） | ❌ 不觸發 |
| 拓樸 0，L3 跑出來 confidence 0.6（dynamic 邊太多） | `L3` | `partial` | ✅ 觸發 |
| 拓樸 0，L3 退到 L2 仍 confidence 0.7 | `L2` | `partial` | ✅ 觸發 |

換句話說：**L1 partial 是 by-design 不報警；L2/L3 partial 才是真正的訊號**。

agent 輸出時 `metadata.analysis_level` 必須準確反映執行層級，arbitration 端用這個欄位區分。

---

## 驗證

### Unit
- L1：input `internal/diff/local_fetcher.go` → 預期 required 含 `TestLocalDiffFetcher`、不含 `TestProjectLoader`、confidence ≈ 0.5
- L2：mock `coverage_map`，input changed_files → 預期正確交集
- L3：mock `edges`（含 30% dynamic 邊）→ 預期 confidence ≈ 0.6

### Integration
- 拓樸 1 模式跑：`POSTGRES_URL=""`、預期 `analysis_level=L1`
- 拓樸 0 模式跑：完整 indexer 跑過後，預期 `analysis_level=L3`、confidence ≥ 0.85

### Fixture repo
用任一註冊 repo（例如 `acme/orders`）當 fixture。改其中某個檔案（例如 `internal/diff/local_fetcher.go`）：
- L1 預期 required = 同 package 內所有 test
- L2/L3 預期 required = 真正反向追蹤到的 test 子集