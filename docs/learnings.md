# 設計反思

`decisions_log.md` 紀錄「我們決定了什麼」；本文紀錄「從這些決定中我學到什麼」。前者是事實清單，後者是把事實變成下次能用的判斷。

---

## 1. Many insights → one decision：仲裁層是後加的，但回頭看才是核心

**初版設計**裡，四個 agent 分別在 MR comment 列出自己的結論——測試清單、風險等級、reviewer 建議、逐行 finding 並列呈現。看起來很豐富，實際上很糟糕：reviewer 打開 PR 看到四段並列的訊號，第一個動作是「然後呢？要 merge 還是不 merge？」。

**收到的 feedback 是**「many insights → one decision」。這句話一講，整個產品的形狀就變了：四個 agent 還是要存在（每個視角都不可省略），但它們的工作從「告訴 reviewer 結論」降級為「提供仲裁層的訊號」，由仲裁層產出**唯一一個** recommendation 寫在最頂層。

**學到的**：豐富的訊號量 ≠ 高品質的判斷。產品的價值在收斂，不在堆訊號。Agent 的數量不是賣點，仲裁的明確性才是。

## 2. Ownership 政治化的疑慮：人事判斷不該外包給系統

**初版的 Ownership agent** 會給每個檔案算 expertise score（blame 比重 + cochange 強度 + recency），排序後產出「top 3 reviewers」並自動寫入 GitLab approval rule。技術上很 satisfying——資料齊全、排序合理、自動化程度高。

**外部的 pushback 一針見血**：你在用一個系統替團隊做人事判斷。誰是「expert」、誰該被 assign review、誰的 contribution 「分數」高，這些事情的政治後果遠超技術層面——當 score 進到 perf review、進到誰拿 promotion，這個系統就成了凶器。

**現在的 Ownership** 完全中性化：沒有 score、沒有 kind 欄位、沒有 hotspots、沒有 ranking、沒有 approval rule writer，輸出的 reviewer 順序刻意打亂。Lint test (`TestOutputSchemaHasNoScoreField`) 強制 schema 不能有 score 欄位以防回退。

**學到的**：技術可行 ≠ 應該做。系統能做出排序，不代表它應該做出排序——尤其當排序的對象是人。中立性是設計選擇，不是缺乏 feature。

## 3. Topology 1 補位：infra-heavy 設計把早期採用者擋在門外

**初版只有 Topology 0**：Postgres + nightly indexer + callgraph + coverage_map + ownership_signals + cochange matrix。這套設計能力強，但對「想試水溫」的團隊門檻太高——光要架 Postgres、跑 indexer、確認 schema migration 正確，可能就要一個下午。

**Topology 1 是後加的補位設計**：砍掉 DB 與 indexer，僅靠當下 diff + 程式碼路徑，跑出降級但仍可用的結果。同一份 analyzer code path、由 `TOPOLOGY` 環境變數切換降級策略，避免維護兩套程式碼。

**學到的**：基礎建設依賴是 adoption 的稅。「降級版本」不是次等公民，它是把「先嚐一口」這條路打開。一個系統如果只能「全部裝起來才能跑」，等於把所有想試的人擋在門外。

## 4. L1 / L2 / L3 信心階梯：用 confidence 量化 graceful degradation

**Selective Test** 在三種環境下需要回答同一個問題（「這份 diff 該跑哪些測試？」），但可用資料差很多：T1 沒 DB 只有 file path、T0 有 coverage_map、T0 callgraph 完整時還能跑反向 BFS。

**初版**做法是寫三個獨立 agent，由 caller 決定要哪個——複雜、容易選錯、且難以表達「我有 L3 但部分檔案 fallback 到 L2」這種混合狀態。

**現在**改用單一 agent 內部 chain：L3 first → L2 fallback → L1 fallback；每階各自附帶 confidence（0.9 / 0.75 / 0.5），仲裁層在做決策時直接消費 confidence，而不是去看「這次跑的是哪一階」。L3 的 dynamic-ratio 信心懲罰處理了「symbol 太發散」這種情境。

**學到的**：不要讓 caller 決定品質階層；讓系統自動降級並把品質量化成 confidence。這樣 caller 永遠拿到「目前可達的最佳結果」，仲裁層也不用為「哪一階」這件事寫額外的 if-else。

## 5. Subagent-driven 開發：協調者守住品質，實作者專注交付

**完整的 62 個 task** 全程由 implementer / spec reviewer / quality reviewer 三段式 subagent 流程交付。主 session 只負責 task 拆解、context 注入、review loop 收斂，不直接寫程式碼。

**這個流程的 leverage 點**：
- 主 session 的 context 不被實作細節污染——可以同時管 23 個 plan C task 而不混亂
- Spec review 和 quality review 兩段分開——spec reviewer 只看「有沒有照計畫做」、quality reviewer 只看「程式碼好不好」，兩個訊號獨立不互相妥協
- Implementer 主動標記疑點（例如「`callgraph.cg.Visit` 在 v0.45.0 不存在」），讓主 session 在 merge 前能介入

**踩到的坑**：subagent 偶爾會誤判自己處於 plan mode，拒絕執行 implementation。需要在 prompt 裡明確標示「plan mode is NOT active」並且偶爾退回主 session 直接動手。

**學到的**：把「想清楚」與「動手做」拆給不同角色，比一個全能 agent 從頭做到尾要可靠得多。前提是協調者要承擔 context 工程的責任——subagent 拿到的不是一份 plan，而是「為這個 task 量身剪裁的指令包」。

---

## 把這五條串起來

仲裁層（1）告訴我**產品的價值在收斂**；Ownership（2）告訴我**技術可行不等於應該做**；Topology 1（3）告訴我**門檻就是把人擋在外面**；信心階梯（4）告訴我**讓系統自動降級而不是讓 caller 選**；subagent 流程（5）告訴我**協調者的工作是 context 工程**。

這五條不是專案的副產品，是專案讓我看清楚的東西。
