# Task: Ownership Agent

## 新 repo 路徑

`Infra/releaseGuard/`（PoC Go-only）

## 對外定位

> **幫助 MR 作者理解這次改動的影響範圍，以及誰可能有相關背景知識。最終的 reviewer 決策由人來做。**

PoC 階段**不**做：自動 assign reviewer、寫 GitLab approval rule、暴露 expertise score、用排名語言（"top owner"、"highest expertise"）。

技術邊界是「誰改過這裡」，產品邊界是「這裡有哪些值得注意的歷史」。前者是資料，後者是建議。本 task 只做後者。

---

## 為什麼這樣設計

Ownership agent 把組織裡的「非正式知識地圖」變成可見輸出——這在技術上是功能、在組織裡是威脅。被系統點名的人有兩種反應：

1. 「對，我最懂這塊，來吧」
2. 「為什麼是我，這不是我的 scope，你們系統有問題」

第二種反應在 reviewer 已經是隱性 bottleneck 時是常態。PoC 階段把語氣從「找最懂的人」改成「指出影響範圍」，把主動權留給 MR 作者，可大幅降低組織阻力。

---

## 要新增的檔案

- `internal/agents/ownership/agent.go` — 主類，組合三個輸出（suggested_reviewers / impact_zones / hidden_dependency_hints）
- `internal/agents/ownership/agent_test.go`
- `internal/agents/ownership/selector.go` — 內部 top-K 篩選（K=3）+ shuffle 輸出順序
- `internal/agents/ownership/zones.go` — 計算 impact_zones：影響的目錄、最近活躍作者數
- `internal/agents/ownership/hints.go` — 計算 hidden_dependency_hints：co-change 關聯敘述
- `internal/agents/ownership/context_phrasing.go` — 把 numeric score 翻譯成自然語言 `context`（不暴露分數）
- `internal/analysis/blame/blame.go` — `git blame --line-porcelain` 解析（indexer 用）
- `internal/analysis/cochange/matrix.go` — co-change matrix（indexer 用）
- `internal/analysis/cochange/recency.go` — recency_decay 計算

> **拓樸 1（無 Postgres）模式不可用**：完全依賴預計算的 `ownership_signals` 與 git history。analyzer 入口層擋下。

---

## 演算法（agent run，線上）

### Phase A — 取候選人（內部用 score）

```
input: changed_files

candidates = {}

for f in changed_files:
  rows = SELECT * FROM ownership_signals WHERE repo_id=? AND file_path=f
  for row in rows:
    candidates[row.author].score += row.blame_weight * row.recency_score
    
    # co-change 隱性候選（折扣係數 0.5）
    for cc in row.co_change_files:
      cc_rows = SELECT * FROM ownership_signals WHERE repo_id=? AND file_path=cc.file
      for cc_row in cc_rows:
        candidates[cc_row.author].score += cc_row.blame_weight
                                         * cc_row.recency_score
                                         * cc.score
                                         * 0.5

# 內部排序、取 top-3
sorted = sortDesc(candidates by score)
top_k = sorted[:3]

# **輸出時 shuffle**——score 不對外暴露，順序也不暗示排名
shuffled = randomOrder(top_k)
```

**為什麼仍然要 score**：repo 有 30 個 contributor 時不能全部列出來、MR comment 會爆長。score 純粹用於「篩 top-K」，不對外。

### Phase B — 翻譯成 context（自然語言）

對每個 top-K candidate 套句模板：

```
if 主要訊號 == blame:
  context = "has recent changes in {file_path} (last activity {N} days ago)"
elif 主要訊號 == cochange:
  context = "has worked on {related_zone} which co-changes with {target_zone}"
```

不出現「owns」、「expert」、「top」、「highest」、「best」這類字眼。

### Phase C — 計算 impact_zones

```
zones = {}
for f in changed_files:
  zone = topLevelDir(f)        // "orders/handler.go" → "orders/"
  zones[zone].files.append(f)
  zones[zone].active_authors = count(distinct authors of zone in last 90 days)

# 過濾很小的 zone
zones = filter(zones where files >= 1)
```

純地圖資訊：**這次 diff 觸到哪些區域、那些區域最近有多少人在動**。完全不指向特定個人。

### Phase D — 計算 hidden_dependency_hints

```
for each pair (zone_a, zone_b) in zones:
  cochange_ratio = COUNT(commits where both zones touched)
                 / COUNT(commits where zone_a touched)
  if cochange_ratio >= 0.5:
    hint = {
      from: zone_a,
      to: zone_b,
      co_change_ratio: cochange_ratio,
      context: "these two zones changed together in {N}/{M} past commits"
    }
```

**語言指向「區域」不指向「人」**——「這兩個地方常一起改」是中性事實，不暗示誰是 owner。

---

## JSON 輸出

```json
{
  "agent": "ownership",
  "status": "ok",
  "duration_ms": 280,
  "schema_version": "1",
  "summary": "diff touches orders/ and payments/; 3 contributors with relevant background",
  "findings": [],
  "metadata": {
    "suggested_reviewers": [
      { "name": "alice", "context": "has recent changes in orders/ (last activity 3 days ago)" },
      { "name": "bob",   "context": "has worked on promo/ which co-changes with orders/" },
      { "name": "carol", "context": "has recent changes in api/openapi.yaml (last activity 5 days ago)" }
    ],
    "impact_zones": [
      { "path": "orders/",   "recent_active_authors_count": 3, "lookback_days": 90 },
      { "path": "payments/", "recent_active_authors_count": 2, "lookback_days": 90 }
    ],
    "hidden_dependency_hints": [
      {
        "from": "payments/",
        "to":   "orders/",
        "co_change_ratio": 0.82,
        "context": "these two zones changed together in 18/22 past commits"
      }
    ]
  }
}
```

**不出現**：`score`、`kind`（blame/cochange）、`hotspots`、`reviewers`（用詞太命令式）、ranking 數字。

---

## MR comment 渲染（task_agent_result 的 renderer 套用）

```markdown
### 影響範圍

這次 diff 觸及：
- `orders/` — 最近 90 天有 3 位作者活躍改動
- `payments/` — 最近 90 天有 2 位作者活躍改動

`payments/` 與 `orders/` 在過去 22 次相關 commit 中有 18 次同時被改動。

### 可考慮邀請 review 的人

> 系統提供相關背景，最終 reviewer 由 MR 作者決定。

- @alice — 最近在 orders/ 有 commit（3 天前）
- @bob — 在 promo/ 與 orders/ 有共改紀錄
- @carol — 最近在 api/openapi.yaml 有 commit（5 天前）
```

順序隨機（每次 render 重新 shuffle），避免「永遠 alice 在最前面」造成隱性排名。

---

## Arbitration 中性立場

**Ownership 訊號永遠不進 Decision Arbitration Layer 的 `triggered_signals`**——即使 `hidden_dependency_hints` 顯示強烈的 co-change 模式、即使 `impact_zones` 跨多個區域，都不會把 recommendation 從 PROCEED 推到 REVIEW。

理由：把 ownership pattern 當成 REVIEW 觸發訊號，等於系統替組織判斷「這個改動需要找人看」——這違反本 agent 的提示模式定位（系統提供地圖、決策由人）。

Ownership 在 MR comment 內仍以 `impact_zones` / `suggested_reviewers` / `hidden_dependency_hints` 等中性形式呈現，但**不影響 recommendation 等級**。

Arbitration 規則詳見 `task_agent_result.md`。

---

## 不做（PoC 砍掉、移到可改進清單）

- ❌ GitLab approval rule writer（不呼叫 `PUT /approval_rules`）
- ❌ 自動 assign reviewer
- ❌ `score` 欄位輸出
- ❌ `kind` 欄位輸出（blame vs cochange 內部分類，不對外）
- ❌ `hotspots` 警告（語義模糊、容易被當成「這個人有問題」的暗示）
- ❌ 排名輸出（用 shuffle 取代）

---

## 驗證

### Unit
- 構造 `ownership_signals` fixture（5 個 author、不同 blame_weight）→ 驗證內部 top-3 選對
- 連續呼叫 10 次，輸出順序有變化（驗證 shuffle 生效）
- candidate 不到 3 個時：照樣輸出（不補空），summary 自然描述
- 無 candidate 時：`suggested_reviewers` 為空陣列、impact_zones 仍要有

### Integration
- 用任一註冊 repo（例如 `acme/orders`）當 fixture，改其中一個檔案
- 預期輸出：
  - `suggested_reviewers` 內名字是 `internal/diff/` 真正的 contributor（隨機順序）
  - `impact_zones` 至少 1 筆（`internal/diff/`）
  - `hidden_dependency_hints` 視 co-change matrix 而定
- 驗證 GitLab API **沒有**被呼叫 `PUT /approval_rules`

### 語氣驗證（regex / lint test）
- `metadata` 內**不能**出現 `score` 欄位
- `metadata.suggested_reviewers[].context` 字串**不能**包含 `owns`、`expert`、`top`、`highest`、`best` 字根