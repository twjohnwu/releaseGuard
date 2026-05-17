# ReleaseGuard — 一頁總覽

**MR 級別的 release 風險閘門。** 四個專責 agent 並行檢視 diff，由仲裁層收斂成單一 **HOLD / REVIEW / PROCEED** 建議，貼回 MR comment 最頂層。

## 問題

傳統 CI 把「該跑哪些測試」、「rollout 風險多高」、「該找誰 review」、「程式碼有沒有 anti-pattern」這四個正交視角全壓成一條 pass/fail。reviewer 看到 MR 時拿到的不是判斷，而是一堆需要自己整合的訊號。

## 方法

把判斷拆成四個正交視角，由四個 agent 並行產出結構化訊號：

| Agent | 回答的問題 |
|---|---|
| Selective Test | 這份 diff 該跑哪些測試？信心多高？ |
| Rollout Risk | 哪些 zone 受影響？嚴重度多高？ |
| Ownership | 誰熟這塊？建議哪些 reviewer？ |
| AI Reviewer | 程式碼層面有沒有 finding？ |

仲裁層（規則式、可回溯）收四份輸出，套用決策矩陣（critical → HOLD、rollout HIGH → HOLD、L2/L3 partial 或 high finding → REVIEW、其餘 → PROCEED），產出唯一一個 recommendation 與其 triggered_signals。

## 成果

- **3 個 git tag**：`plan-a-foundation`（Foundation 19 task）、`plan-b-topology-1`（Demoable 20 task）、`plan-c-postgres-agents`（Postgres 23 task）
- **Local harness demo**：`docker compose up` 一鍵跑出 PROCEED / REVIEW / HOLD 三段截圖（[`docs/case_study.md`](case_study.md)）
- **兩種拓撲**：T0（Postgres + indexer，能力完整）vs T1（zero-infra，降級但可用），由 `TOPOLOGY` 環境變數切換
- **設計反思**：5 條已寫成 [`learnings.md`](learnings.md)；13 個關鍵轉折寫在 [`decisions_log.md`](decisions_log.md)

## 設計亮點

- **Many insights → one decision**：仲裁層收斂訊號為單一可行動建議
- **Ownership 中性化**：刻意不打分數、不寫 approval rule、輸出順序隨機（lint 強制）
- **L1 / L2 / L3 信心階梯**：DB 在不在都能跑，confidence 量化品質
- **Subagent-driven 開發**：62 個 task 全程三段式 review 流程交付

## 連結

- 實際截圖與 case study：[`docs/case_study.md`](case_study.md)
- 架構圖與決策矩陣：[`docs/architecture.md`](architecture.md)
- 設計反思：[`docs/learnings.md`](learnings.md)
- 設計轉折紀錄：[`docs/decisions_log.md`](decisions_log.md)
- GitHub repo：[twjohnwu/releaseGuard](https://github.com/twjohnwu/releaseGuard)
