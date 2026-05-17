# Task: Agent Result Composer & Poster（最終結果聚合）

## 新 repo 路徑

`Infra/releaseGuard/`（PoC Go-only）

## 目的

讀取已啟用 agents 的 `agent_outputs`，組合成最終 `ImpactScopeReport`，並依四個 `RG_AGENT_*_ENABLED` 旗標決定 **MR comment 與 CI variable 兩個 sink** 的呈現。**在 task 4~7 之後執行**——是 analyzer 流程的最後一棒。

> **PoC 不含 GitLab approval rule sink**——Ownership agent 改採「提示而非命令」的對外定位（見 `task_ownership.md` 開頭），不自動寫 approval rule、不自動 assign reviewer。Approval rule writer 移到可改進清單。

---

## 要新增的檔案

- `internal/result/composer.go` — 主類，組裝 `ImpactScopeReport`、呼叫 arbitration
- `internal/result/composer_test.go` — 旗標組合 + 容錯 + arbitration 規則矩陣
- `internal/result/poster.go` — 兩個 sink 的分發邏輯
- `internal/result/poster_test.go`
- `internal/report/arbitration.go` — **Decision Arbitration Layer**：把四個 agent 的 output 聚合成單一 `Recommendation`
- `internal/report/arbitration_test.go` — 規則矩陣 + 邊界 case + 防禦性 fallback
- `internal/result/templates/report.tmpl` — Go template（renderer 在最頂層渲染 recommendation 區塊）

---

## Decision Arbitration Layer

**動機**：把「四個 agent 並列輸出，使用者自己整合」改成「先給一個明確的 recommendation，再附細節」。CI system 的本質是 trust system 不是 correctness system——使用者第一眼必須看到「現在該怎麼辦」。

### 輸出 schema

```json
{
  "recommendation": "HOLD | REVIEW | PROCEED",
  "rationale": "短 markdown，從 triggered_signals 自動組句",
  "triggered_signals": [
    { "agent": "ai_reviewer",  "kind": "critical_finding", "detail": "Missing defer tx.Rollback() in handler.go:142" },
    { "agent": "rollout_risk", "kind": "high_risk",        "detail": "Breaking API change: discount_code field removal" }
  ]
}
```

**不含**：`dominant_signal`（單值會掩蓋共存訊號）、`confidence`（規則式決策非機率性）、`suggested_action`（與 rationale 重疊）。

### 觸發規則表

| 條件 | 訊號 kind | 結果等級 |
|---|---|---|
| AI Reviewer 任一 `critical` finding | `critical_finding` | **HOLD** |
| Rollout Risk = `HIGH` | `high_risk` | **HOLD** |
| AI Reviewer 任一 `high` finding（非 critical） | `high_finding` | REVIEW |
| Rollout Risk = `MED` | `med_risk` | REVIEW |
| Selective Test `status=partial` 且 `analysis_level != L1` | `unstable_test_plan` | REVIEW |
| 任一 agent `status=failed` | `agent_failure` | 下限至少 REVIEW |
| Ownership 任何訊號 | （**不觸發**） | 不影響 |
| 否則 | （無觸發） | PROCEED |

最終 recommendation = `HOLD` if any HOLD 訊號 else `REVIEW` if any REVIEW 訊號 else `PROCEED`。

### 防禦性規則

| 情境 | 行為 |
|---|---|
| 全部 agent `status=failed` | recommendation = `REVIEW`，rationale 註明「all agents failed」 |
| arbitration 內部 panic | 預設 `REVIEW`，rationale = `"arbitration error: <details>"`，不擋 report |
| L1 by-design partial | **不**觸發 `unstable_test_plan`（拓樸 1 模式不該被 spam REVIEW） |
| Ownership 訊號 | 永遠不進 `triggered_signals`——保持「提示模式」中性立場 |

### Renderer 在 MR comment 最頂層

```markdown
## 🔴 ReleaseGuard recommendation: HOLD
> 🚨 Critical: Missing defer tx.Rollback() in handler.go:142
> 🚨 HIGH risk: Breaking API change in /v2/orders

[原本的 Impact Scope Report 細節接在下方]
```

emoji 映射：`HOLD=🔴` / `REVIEW=🟡` / `PROCEED=✅`。

---

## Composer 流程

```
input:
  agent_outputs: AgentOutput[]   // 從 Postgres 讀回
  flags: { selective_test, rollout_risk, ownership, ai_reviewer }

1. 早退：if all flags false → return EmptyReport, log "all agents disabled"

2. 篩選：outputs = agent_outputs.filter(o => flags[o.agent])

3. 計算 RiskLevel：
   if rollout_risk in outputs && status != failed:
     overall = outputs.rollout_risk.metadata.riskLevel
   else: overall = nil

4. 攤平 findings：
   all = []
   for o in outputs: all.push(...o.findings)
   dedupe by stable_id
   sort by severity desc

5. 組裝 ImpactScopeReport（fields 的存在性對應 flag）:
   {
     Recommendation:           report.Arbitrate(outputs)         // ← top-level decision
     RiskLevel:                flags.rollout_risk ? overall : nil
     SelectiveTests:           flags.selective_test ? extract(...) : nil
     SuggestedReviewers:       flags.ownership ? extract(...) : nil   // 已 shuffle、無 score
     ImpactZones:              flags.ownership ? extract(...) : nil
     HiddenDependencyHints:    flags.ownership ? extract(...) : nil
     AIReviewFindings:         flags.ai_reviewer ? extract(...) : nil
     HighSeverityFindings:     filter(severity in [critical, high])
     EnabledFlags:             flags
   }

6. 容錯標記：
   for each agent in outputs where status=failed:
     report.FailedAgents.push(agent.name)
   for each agent where status=partial:
     report.PartialAgents.push({name, reason})
```

---

## 旗標組合 → 可見輸出（共 16 種，列出 5 個代表）

| selective | rollout | ownership | reviewer | comment 段落 | CI variable |
|---|---|---|---|---|---|
| ✓ | ✓ | ✓ | ✓ | RiskLevel + Selective + 影響範圍 + reviewer 提示 + AI details | ✓ |
| ✗ | ✓ | ✓ | ✓ | 無 Selective 段；其餘 3 段 | ✗ |
| ✓ | ✗ | ✓ | ✓ | 無 RiskLevel/Mitigation | ✓ |
| ✓ | ✓ | ✗ | ✓ | 無「影響範圍」與「reviewer 提示」段 | ✓ |
| ✓ | ✓ | ✓ | ✗ | 無 AI details | ✓ |
| ✗ | ✗ | ✗ | ✗ | analyzer short-circuit、不 post | ✗ |

---

## Poster — 兩個 Sink

```go
func (p *Poster) Post(ctx context.Context, report ImpactScopeReport) error {
    var wg errgroup.Group

    // Sink 1: MR comment
    if report.HasAnyContent() {
        wg.Go(func() error {
            md := p.renderer.Render(report)
            return p.gitlab.PostMRNote(ctx, p.mrID, md)
        })
    }

    // Sink 2: CI variable / artifact
    if report.EnabledFlags.SelectiveTest && report.SelectiveTests != nil &&
       report.SelectiveTests.Confidence >= p.minConfidence {
        wg.Go(func() error {
            return os.WriteFile("selective-tests.txt",
                []byte(strings.Join(report.SelectiveTests.Required, "|")), 0644)
        })
    }

    return wg.Wait()
}
```

每個 sink 失敗**不影響其他 sink**——errgroup 收集錯誤但繼續。

> **不含 approval rule sink**：PoC 不呼叫 `PUT /merge_requests/:iid/approval_rules`。MR comment 內 reviewer 提示用 `@username` mention 風格陳述事實，由 MR 作者自行邀請。

---

## MR comment 範本（節錄）

```markdown
## 🔴 ReleaseGuard recommendation: HOLD
> 🚨 Critical: Missing defer tx.Rollback() in handler.go:142
> 🚨 HIGH risk: Breaking API change in /v2/orders

---

## Impact Scope Report

### Risk level: HIGH
- Breaking change: POST /v2/orders removed `discount_code`
- Spec drift: handler changed without OpenAPI update

### Selective test plan (L3, confidence 0.92)
▶ Required (7): TestOrderCreate, ...
⏭ Skippable (43): ...

### 影響範圍
這次 diff 觸及：
- `orders/` — 最近 90 天有 3 位作者活躍改動
- `payments/` — 與 `orders/` 在過去 22 次相關 commit 中有 18 次同時被改動

### 可考慮邀請 review 的人
> 系統提供相關背景，最終 reviewer 由 MR 作者決定。
- @alice — 最近在 orders/ 有 commit（3 天前）
- @bob — 在 promo/ 與 orders/ 有共改紀錄
- @carol — 最近在 api/openapi.yaml 有 commit（5 天前）

---
<details>
<summary>📝 AI code review (line-level findings)</summary>
...
</details>
```

順序在每次 render 時 **shuffle**（從 ownership agent metadata 過來時已 shuffle，這邊不再排序）。

---

## 容錯規則

| 情況 | 行為 |
|---|---|
| 某 agent `status=failed` | 該段落顯示 `> ⚠️ {agent}: failed (reason: ...)`；其他段落照常 |
| 某 agent `status=partial` | 段落正常顯示，附 caveat 提示（如 selective 信心低）|
| `selective-tests.txt` 寫入失敗 | log 錯誤，CI 自動走 test-full track |
| `mr_runs.status` 更新時機 | composer 結束前 `UPDATE mr_runs SET status=?, risk_level=?` |

---

## 驗證

### Unit
- 16 種 flag 組合 × fixture agent_outputs：assert markdown 含/不含預期段落
- partial agent fixture：assert caveat 顯示
- failed agent fixture：assert placeholder 顯示
- **語氣 lint test**：渲染結果**不能**包含 `score`、`expert`、`top owner`、`highest`、`best` 字串

### Arbitration 規則矩陣
專屬測試表格（不與 16 種 flag 組合重疊）：

| Fixture 情境 | 預期 recommendation | 預期 triggered_signals |
|---|---|---|
| AI critical + Rollout HIGH + Test partial L3 | HOLD | 3 個訊號 |
| AI high only | REVIEW | 1 個 |
| Rollout MED only | REVIEW | 1 個 |
| Test partial L1（拓樸 1） | PROCEED | 0 個（L1 by-design） |
| Test partial L3 confidence=0.5 | REVIEW | 1 個 unstable_test_plan |
| 全 OK | PROCEED | 0 個 |
| Ownership 大量 hidden hints + 其他全 OK | PROCEED | 0 個（ownership 不觸發） |
| AI failed + Rollout LOW | REVIEW | 1 個 agent_failure |
| 全部 4 agent failed | REVIEW | rationale 註明 all failed |
| Arbitration 內部 panic（用 mock 注入） | REVIEW | rationale 含 "arbitration error" |

### Integration
- 跑完整 analyzer，驗證：
  - `mr_runs` 有一筆、status 與 risk_level 正確
  - GitLab 收到 1 個 note（mock server 攔截）
  - **GitLab approval_rules API 沒有被呼叫**（重要：驗證 PoC 沒誤觸 sink）
  - `selective-tests.txt` 內容正確

### Snapshot
- 每種主要 flag 組合存一份 markdown snapshot 在 `testdata/snapshots/`
- 後續 template 修改時用 snapshot 比對防回歸

---

## 與其他 task 的依賴

```
task_selective_test  ─┐
task_rollout_risk    ─┼──► task_agent_result（這個 task）
task_ownership       ─┤
task_ai_reviewer     ─┘
```

四個 agent 都產出統一 `AgentOutput` 後，本 task 才能整合。實作建議順序：
1. 先寫 composer + renderer + 16 種 unit fixture（不依賴實際 agent）
2. 等任一 agent 實作完成 → 接 poster 真實 sink
3. 全部 agent 完成 → end-to-end 驗證
