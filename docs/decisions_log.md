# ReleaseGuard Decisions Log

這份檔案不是規格也不是 plan。它記錄這個系統設計過程中的 18 個關鍵設計轉折——每個都附「最初想法 / 為什麼錯 / 現在做法 / 學到什麼」四段。

寫這份的動機：規格與 plan 只呈現最終答案，但**設計判斷力的展現**藏在「為什麼這樣設計而不是那樣」。對作品集而言，過程比結論更稀有。

---

## 1. 從「擴充 MRInspect」到「全新 ReleaseGuard」

**最初想法**：在既有 MRInspect 上加幾個新 agent（selective test、rollout risk、ownership），把它變強。

**為什麼錯**：MRInspect 的產品定位是「**逐行 code review 的助教**」——關注微觀的程式碼問題。我想加的東西是「**發布把關的工程師**」——關注巨觀的系統影響。把兩個方向混在一個系統裡，會讓 MR comment 變成既是「這行有 bug」又是「這次 release 會炸」的混合產物，使用者既看不下去也不知道哪個重要。

**現在做法**：ReleaseGuard 是獨立系統，原 MRInspect 不動。AI Reviewer 在新系統裡降級為四個 agent 之一、不再是主角。

**學到什麼**：產品定位（micro-level vs macro-level）比技術實作更早決定設計。混合定位的工具沒人愛用。

---

## 2. TypeScript mirror 加了又拿掉

**最初想法**：所有實作都要 Go + TypeScript 兩端，與既有雙跑道設計慣例一致。

**為什麼錯**：PoC 階段沒人需要兩端。雙端意味著每個介面要寫兩次測試、每個 schema 要保證跨語言對齊、每次重構要動兩個 repo。維護成本翻倍但 PoC 沒有可驗證的雙端使用情境。

**現在做法**：Go-only。`Finding` / `AgentOutput` 的 schema 仍以中性 JSON Schema 形式記在規格內，未來若要加 TS 端有契約可參照，但 PoC 不寫第二端。

**學到什麼**：「為了未來擴展性而現在多做」是常見的過度設計陷阱。**保留 schema 契約 + 砍掉重複實作**，是合理的中間路線。

---

## 3. Selective Test：單層 → L1/L2/L3 三層

**最初想法**：Selective Test 用 call graph + coverage map 反向追蹤，確定性高。

**為什麼錯**：這個設計依賴 Postgres + nightly indexer + 預建 call graph。**沒這些基礎設施就不能用**。但部分使用者（小團隊、PoC demo、沒有中央 Postgres 的環境）需要輕量啟動模式（拓樸 1）。如果 Selective Test 只能在拓樸 0 跑，整個拓樸 1 就缺了一個 agent。

**現在做法**：三層架構——
- L1：純 file path / package 模式，無 DB（拓樸 1 可用）
- L2：coverage_map intersect（需 Postgres）
- L3：reverse BFS on call graph（需 Postgres + indexer）

每個 finding 帶 `analysis_level` + `confidence` + `fallback_chain`，使用者一眼知道是哪層算的。

**學到什麼**：infrastructure-heavy 的功能要有 graceful degradation 的層級。**不能讓「有 DB / 沒 DB」變成 boolean，否則拓樸切割會被破壞**。

---

## 4. Ownership：智能推薦 → 中性提示

**最初想法**：Ownership Agent 計算每個候選 reviewer 的 expertise score、寫進 MR comment、自動寫 GitLab approval rule 把 top-2 設為 required approver。

**為什麼錯**：這是技術上正確、組織上**有毒**的設計。當系統公開「誰是這段 code 的 owner」並排名，被點名的人有兩種反應：「對，我懂，來吧」或「為什麼是我，這不是我的 scope」。後者在 reviewer 已經是隱性 bottleneck 時是常態。系統把組織內的「非正式知識地圖」變成可見輸出——這是把組織政治外部化。

**現在做法**：
- 移除 `score` 與 `kind` 欄位
- `reviewers` 改名 `suggested_reviewers`，順序 shuffle
- 改用 `context` 取代 `score`，用「has recent changes in X」「co-changes with Y」這類事實陳述
- 移除 GitLab approval rule writer
- 新增 `impact_zones`（影響哪些目錄、活躍作者數）與 `hidden_dependency_hints`（co-change 關聯）作為**中性地圖資訊**
- 對外定位改為「幫助 MR 作者理解影響範圍」，不是「找最懂的人」

**學到什麼**：**技術可行性 ≠ 產品適當性**。某些訊號暴露給使用者會造成負面後果，即使資料品質很好。組織政治意識是 senior 級別的設計判斷力。

---

## 5. Decision Arbitration Layer 是後加的（最重要的那次）

**最初想法**：四個 agent 並列輸出，使用者在 MR comment 裡看到完整 Impact Scope Report，自己整合資訊判斷該怎麼辦。

**為什麼錯**：這違反「CI is a trust system not a correctness system」的本質。使用者不會花時間看 5 個段落然後做心算總結；他們需要**第一眼就知道該怎麼辦**。沒有單一決策的 review 工具，使用者學會的是「忽略它」，因為認知負擔太高。

**現在做法**：新增 `internal/report/arbitration.go` Decision Arbitration Layer，把四個 agent 的輸出收斂成單一 `Recommendation`：`HOLD` / `REVIEW` / `PROCEED`，渲染在 MR comment 最頂層。原本的 Impact Scope Report 變成支撐細節，在下方展開。

**學到什麼**：**collapse insights, don't dump them**。這是這個系統最重要的產品 insight，且是後加的——表示我初期設計時對「使用者體驗」想得不夠深。

---

## 6. Arbitration 內部：單一 dominant_signal → triggered_signals 列表

**最初想法**：Arbitration 輸出一個 `dominant_signal` 欄位（值為 `risk` / `test` / `reviewer` / `ownership`），告訴使用者「這個建議的主要原因是什麼」。

**為什麼錯**：當一個 MR 同時有 critical finding **且** HIGH risk **且** test 低 confidence 時，把這三件事壓縮到 `dominant_signal: "reviewer"` 反而違背 arbitration 的初衷——「many insights → one decision」應該是「決定一個」，不是「掩蓋兩個」。

**現在做法**：拿掉 `dominant_signal`，改成 `triggered_signals: [...]` 陣列。Renderer 渲染成「HOLD because: 1) critical finding in foo.go, 2) HIGH risk: breaking API」——保留全部訊號，但 recommendation 等級仍是單一值。

**學到什麼**：collapse 是在 recommendation 層，**不是**在 evidence 層。把資訊壓縮到錯誤層級會損失重要訊號。

---

## 7. 拓樸 1（無 Postgres 模式）

**最初想法**：完整方案 A——所有 agent + RAG + Postgres + nightly indexer 都必要。

**為什麼錯**：這要求**所有部署環境都先架好基礎設施**才能用。但有人需要：(a) 快速 demo、(b) 單 PR 試跑、(c) 中央 Postgres 還沒建好的階段。如果系統不能漸進部署，第一個使用者根本進不來。

**現在做法**：兩個拓樸——
- 拓樸 0：完整方案（4 agent + RAG + Postgres + indexer）
- 拓樸 1：只跑 Rollout Risk + AI Reviewer（Channel A only）。零基礎設施依賴，CI runner 拉 image 即用

由 analyzer 入口層檢查 env 組合的合法性。**不允許**「Postgres 在但 partial」之類的中間狀態——要麼完整、要麼完全沒有。

**學到什麼**：「最小可用部署」是產品決定不是技術決定。**漸進式 onboarding 路徑**比「完美架構但部署門檻高」更重要。

---

## 8. PlantUML parser + diagram_aliases.yaml 是 retrofit

**最初想法**：所有分析在「這個 repo」內完成。call graph、ownership、coverage 都是 per-repo 概念。

**為什麼錯**：service repo 的 OpenAPI handler 改了、跨 repo 的 downstream 服務會炸——這個關聯**完全在 repo 之間**。沒有跨 repo 的拓樸資訊，rollout risk 無法回答「誰會被影響」。文件 repo 內的 PlantUML 圖是現成的跨 repo 拓撲表達，但初期設計沒納入。

**現在做法**：
- 新增 `internal/indexer/code/plantuml/` 解析 `.puml` 檔
- `config/diagram_aliases.yaml` 把 PlantUML participant 名稱對應到 `repos.name`
- 新表 `cross_repo_edges` 儲存解析結果
- Rollout Risk 的 `affected_services` 欄位透過 alias lookup 反查

**學到什麼**：「**這個 repo** 的邊界」與「**系統** 的邊界」是兩個不同層級的問題。設計初期要意識到**哪些事必須跨 repo 才能回答**，否則會做出單 repo 看起來完美、整體看起來破碎的系統。

---

## 9. Indexer 內部：單一 pipeline → code/ + corpus/ 子樹

**最初想法**：Indexer 是單一 nightly pipeline，五個 step 順序跑。

**為什麼錯**：Step 1-3（callgraph、coverage、cochange）是「對 git repo 做程式碼分析」，與 analyzer 緊耦合（共用 `symbols/edges/coverage_map/ownership_signals` schema）。Step 4（RAG ingest）是「從**異質外部來源**抓文件」，未來可能來自 Confluence、Notion、Slack——觸及完全不同的 auth、format、rate limit、語言生態（Python 接 LangChain/LlamaIndex 的能力遠勝 Go）。把這兩件事放同一個 pipeline，遲早會出現「想用 Python 但綁在 Go repo」的尷尬。

**現在做法**：
- `internal/indexer/code/`：Step 1-3 + 5（PlantUML），與 analyzer 緊耦合
- `internal/indexer/corpus/`：Step 4，未來 split 候選
- `CorpusConnector` 介面預留擴展點
- 在 `task_indexer.md` 明告 4 個 split 觸發訊號 + schema migration 協調風險

**學到什麼**：**boundary anticipation** 比 boundary enforcement 更重要。預先知道哪裡會分家，讓內部結構長得讓未來分家容易，比「等到必須分家才動」省 10 倍力氣。

---

## 10. RAG Hot Path 不寫回 Postgres

**最初想法**：每次 analyzer 對「本次 MR 的 commit message + diff」現場 embed，順便寫回 Postgres 當作 corpus，下次 MR 就能用。

**為什麼錯**：
- MR 可能被 reject / 重寫 / 廢棄——未驗證內容會污染歷史 corpus
- 表規模膨脹、需要 idempotent + 清理機制
- Hot path 與 nightly 涵蓋範圍**完全不同**（runbook、co-change、call graph、coverage 都不是本次 MR 能產生的），所以「寫 Postgres 取代 nightly」是錯誤對比

**現在做法**：Hot path 結果**只在記憶體**存活到 Stage 3 結束。要讓 past MR 進 corpus，採 webhook incremental（merge 事件觸發、不是 MR 開時觸發），列入可改進清單。

**學到什麼**：**寫入紀律比讀取紀律難**。讀取錯了重來就好，寫入錯了會污染歷史，且 idempotent / 清理 / 一致性都是額外負擔。預設不寫，是更謹慎的設計。

---

## 11. Backfill subcommand 砍了又留

**最初想法**：onboarding 新 repo 時跑一次 `cmd/indexer backfill --repo=X --since=2y` 把過去 MR/postmortem 一次吃進去。

**第一輪挑戰**：「等 nightly 跑就好了吧？差異多大？」——論點是 nightly 累積足夠久就涵蓋了。

**為什麼那個論點不完全對**：nightly 只抓「過去 24h 新增的 MR」，**永遠不會回頭**抓 onboarding 之前的歷史。要等 nightly 累積到 2 年深度需要 2 年。對接 SaaS、想要 Day 1 就有歷史 MR corpus 的場景，必須有 backfill。

**現在做法**：保留 `cmd/indexer backfill` subcommand，但明確標為「onboarding 時手動跑一次」，不排程。重用既有 corpus pipeline，只是參數不同（時間窗、source 類型、rate limit）。

**學到什麼**：**onboarding 是與 steady-state 不同的需求類別**。把它折進 nightly 會錯，但獨立做又不該重新發明 pipeline——用同一個底層、不同的 entry point 是正解。

---

## 12. Approval rule writer：sink 從 3 個變 2 個

**最初想法**：Composer 把結果分發到 3 個 sink——MR comment、GitLab approval rule、CI variable artifact。

**為什麼錯**：approval rule writer 與 Ownership Agent 的中性化（Decision #4）矛盾。一旦 ownership 不對外暴露排名，自動寫 approval rule 就失去基礎——你憑什麼把某個人設為 required approver？

**現在做法**：移除 approval rule sink。Composer 只剩 2 個 sink：MR comment（含 `@username` mention 風格的 reviewer 提示）+ CI variable artifact（selective tests）。Approval rule writer 移到可改進清單，註明「需先有組織共識才開啟」。

**學到什麼**：**橫切設計變更會 ripple**。當核心定位改變（Ownership 中性化），所有依賴它的下游（poster sink、template、metric）都要重審。

---

## 13. PgBouncer 加進來

**最初想法**：`POSTGRES_URL` 直接連 Postgres。

**為什麼錯**：接 SaaS 規模時，多 repo 並發峰值可達 50-150 個連線（每 MR 開 analyzer × N repos），Postgres 預設 `max_connections=100` 撐不住。

**現在做法**：`POSTGRES_URL` 範例改為指向 PgBouncer port 6432。連線設定使用 transaction pool mode，意味著放棄 prepared statement 與 LISTEN/NOTIFY，改用 simple query protocol。pgx v5 的 `DefaultQueryExecMode = QueryExecModeSimpleProtocol` 一行設定。

**學到什麼**：**連線池化在 SaaS 規模不是 nice-to-have**。設計時就應假設多 caller 並發，避免「先寫完再說」最後要回頭改 schema。同時，每個 trade-off（PgBouncer transaction mode vs prepared statement）都要明白寫進規格——不要讓使用者踩坑時才發現限制。

---

## 14. Renderer 從「燈號 only」→ 帶證據的 enriched rendering

**最初想法**：MR comment 只放 emoji + recommendation 字樣（`🟡 ReleaseGuard recommendation: REVIEW`），其他 agent 細節留在背後 JSON，reviewer 想看就點連結。

**為什麼錯**：實測 demo 後使用者直接反映「這三個情境只有燈號，沒有說明哪裡造成這樣的燈號」。reviewer 看到結論卻看不到根據——這違反第 5 條對 arbitration 的初衷。Arbitration 把訊號收斂成單一 recommendation，但 evidence 必須留在使用者眼前可稽核，不能藏進 JSON。

**現在做法**：Renderer 多輸出三層證據——
- `triggered_signals` 列表：哪個 agent / 哪類訊號 / 摘要敘述（例：`[rollout_risk] med_risk: MED (4 zones, 3 config_drift, 1 spec_code_drift)`）
- Risk-level 細項：每個 zone 的 severity + kind + path（例：`🟡 medium · config_drift: helm/values.yaml`）
- Selective Test plan reason：解釋為何選 L1（或 L2/L3）

**學到什麼**：**可稽核性是 gating 系統的命脈**。reviewer 對「黑箱建議」的接受度為零。第 5 條教會了 collapse，第 14 條才補齊「collapse 不等於 hide」——recommendation 收斂、evidence 攤開。

---

## 15. Lint 鎖死 Ownership 中性化（防回退機制）

**最初想法**：第 4 條已決定 Ownership 中性化（無 score / kind / approval rule writer），靠程式碼當下不寫這些欄位來保證。

**為什麼錯**：「current 程式碼這樣寫」不是保證。未來若有人加回 score 欄位（出於善意「這樣 reviewer 比較好挑」），整個中性化決策被靜默推翻。Code review 不一定攔得住——這類欄位看起來無害，reviewer 可能直覺認為「合理欄位」就放行。設計決策只寫進 `decisions_log.md` 是不夠的，文件不會擋 PR。

**現在做法**：新增 `TestOutputSchemaHasNoScoreField` lint test，掃 Ownership agent JSON output schema，若出現 `score` 或 `kind` 欄位即 fail。CI 跑這個 test，PR 過不了。同時在 spec 與 test 註解明寫此規則的歷史脈絡（指向第 4 條）。

**學到什麼**：**把組織政治決策寫進 lint 比寫進文件可靠得多**。文件會被忽略；CI 不會。當一個設計決策有「未來被善意推翻」的風險時，最低成本的防禦是把它寫成 test。

---

## 16. Plan C 從 deferred 重啟

**最初想法**：Plan A+B 完成後，Plan C/D 都標 deferred——PoC 已能 demo，再做投報率不高，把時間留給作品集打磨。

**為什麼錯**：Plan B 在 Topology 1 跑得通，但 confidence 永遠 0.4（L1 上限）。demo 三段 outcome 都是 L1，根本沒辦法展現 L2/L3 的差異化能力——也就是說「拓樸 0」只在規格上存在、實際沒程式碼。對作品集敘事而言，這是規格與實作的落差：宣稱有 Postgres-backed L2/L3，但 demo 不出來。reviewer 一眼看穿。

**現在做法**：Plan C 23 個 task 全部完成——4 個 migration、indexer 5-step pipeline、L2 coverage_map intersect、L3 反向 BFS、Ownership 真實版（blame + cochange）、analyzer Topology 0 wiring。3 個 git tag 對齊三個里程碑（plan-a-foundation、plan-b-topology-1、plan-c-postgres-agents）。

**學到什麼**：**「deferred」很容易實質變成「abandoned」**。當作品集敘事缺一塊（拓樸 0 規格存在但未實作），就要選擇是補上還是把規格也砍掉，**不能讓規格與實作的落差永遠存在**——否則文件成了「PR 標題」，無法兌現。

---

## 17. Renderer 長清單從「截斷」變成「收合」

**最初想法**：Selective Test 的 `required[]` 在 MR comment 只列前 5 個，超過就印一行 `… 23 more`，避免清單把整個 comment 拉長。完整清單留在 CI artifact `selective-tests.txt` 給 test runner 用——「reviewer 不會看完所以不必印全部」。

**為什麼錯**：擴到全 repo coverage 後 L3 反向 BFS 一次拉到 28 個 test，`… 23 more` 那行變成赤裸裸的「這 23 個我不告訴你」。這違反第 14 條剛建立的原則——可稽核性。reviewer 想知道「這 28 個是哪些」，渲染器卻把它藏起來，等於系統再次回到「黑箱建議」模式。「保持簡潔」與「不丟訊號」被誤解成同一件事。

**現在做法**：renderer 改用 GitHub / GitLab 原生支援的 `<details>` 收合區塊——預設仍只顯示前 5 個（保留掃讀體驗），其餘包在可點擊展開的 `<summary>… 23 more</summary>` 後。短清單（≤5）跳過 `<details>` wrapper 維持扁平樣式。

**學到什麼**：**「簡潔」是預設體驗，不是資訊上限**。當資訊量增加時應該加層級（disclosure），不是丟訊號。在第 14 條（renderer 從燈號 only → 帶證據）建立可稽核性後，這條補上「可稽核性也包含『可展開查看』，不是只有 first impression」。設計反思上，這也是個小型「many insights → one decision」失誤——我把「single decision」誤推導成「single 5-item list」，但 collapse 是在 recommendation 層、不是在 evidence 列舉層。

---

## 18. HOLD 誤報回饋：GitLab label MVP 而非 Postgres 表

**最初想法**：既然要量測 gate 是否誤報，自然想到開一張 Postgres 表（`gate_feedback`）記錄每個 MR 的 decision、reviewer 是否標記誤報、時間戳，再從表裡算 precision，甚至接上 dashboard。畢竟 T0 拓樸本來就有 Postgres。

**為什麼錯**：對「先驗證『這個回饋迴路有沒有人用』」這個目標來說，Postgres 表是過度工程。它綁死 T0 拓樸（T1 零 infra 就用不了）、需要 migration、需要寫入路徑、需要處理 reviewer 標記與 analyzer 寫入的競態——全部都是在「還不知道 reviewer 會不會真的去按 label」之前就付出的成本。真正稀缺的不是儲存，是**訊號本身**（reviewer 願不願意標記誤報）。

**現在做法**：用 GitLab MR label `releaseguard:false-positive` 當唯一資料來源。renderer 在 HOLD/REVIEW 的 comment 結尾加一行邀請 reviewer 標記；`analyzer feedback` 子指令掃近期已合併 MR，把 ReleaseGuard 自己貼的 decision（從 comment 標記字串解析）跟 label 比對，算出 HOLD count / 誤報數 / precision %。零 migration、零新表、T0 T1 都能跑。GitLab 本身就是資料庫。

**學到什麼**：**MVP 的瓶頸通常是訊號取得，不是訊號儲存**。在還沒證明「人會提供這個訊號」之前，把儲存做重是把成本花在錯的地方。另外一個副作用是誠實邊界：selective-test 的信心常數（`internal/agents/testselect/confidence.go`）目前是工程估計值、尚未校準——這條回饋迴路收集到的 precision 資料，正是未來校準那些常數的依據。先把量測管道打通（即使很陽春），比先把儲存做完整更能推進校準這件事。

---

## 19. 仲裁 panic 時 fail-closed 到 REVIEW，不回傳空 verdict

**最初想法**：`Arbitrate()` 用 `defer recover()` 包住仲裁邏輯，想法是「仲裁層任何 panic 都不該讓整份報告消失」。recover 的 body 是空的，註解寫著 fallback 在 wrapper 處理——但 wrapper 從來沒寫。

**為什麼錯**：Go 的非具名回傳在 panic 後只會回零值。結果是 recover 確實吞掉了 panic，但呼叫端拿到 `Recommendation{}`：recommendation 是空字串、沒有 rationale、沒有 signal。MR comment 會貼出一個沒有結論的 gate 結果，比直接 crash 更糟——crash 至少會被 CI 標紅，空 verdict 看起來像「沒事」。這條路徑也沒有任何測試覆蓋，所以兩年內都不會有人發現。

**現在做法**：改為具名回傳 `(rec Recommendation)`，recover 時明確設成 `REVIEW`，rationale 寫 `arbitration panicked: <原因>`，並附一個 `arbitration_panic` signal。選 REVIEW 而非 HOLD：系統自身故障不該封鎖合併（那是把工具的 bug 轉嫁成團隊的阻塞），但必須有人看一眼。仲裁邏輯抽成可注入的 `arbitrateFn` 供測試灌 panic；`TestArbitrate_PanicFailsClosedToReview` 先紅後綠，並列入 AGENTS.md 的 invariants 表。同一輪也把 `runAgentsParallel` 的四個 goroutine 各自加上 recover、per-agent timeout（`AGENT_TIMEOUT_SEC`，未設時等於 `ANALYZE_TIMEOUT_SEC`）與一行 `agent done` log，讓單一 agent 的 panic 或超時不再拖垮整個 analyzer（取消是 context 合作式的，卡在 syscall 裡的 agent 仍會等到它自己返回）。

**學到什麼**：**recover 不是 fallback，recover 之後「回傳什麼」才是 fallback**。防禦性程式碼如果沒有明確定義失敗時的輸出，它只是把失敗藏起來。判斷 fail-closed 該落在哪一級時，問的不是「最安全的是什麼」而是「這個失敗是誰的責任」：工具自己壞了，代價該由工具承擔（要求人看），不該由使用者承擔（封鎖合併）。

---

## 20. Replay dataset 先用 mock fixture 起步，明標「觀察基線」

**最初想法**：外部 review 要求建立匿名 MR replay dataset 來量測 precision 與誤報率。直覺是先去收真實 MR、做匿名化、人工標註，把 dataset 做完整再寫量測工具。

**為什麼錯**：真實匿名 MR 目前一筆都沒有，收集與標註是以週計的工作，而且需要一個真的在用 ReleaseGuard 的團隊。若量測管道要等資料齊全才存在，precision 這個指標會一直停在「計畫中」。跟 #18 同一個教訓的另一面：#18 說瓶頸在訊號取得，這裡則是「管道不存在，訊號來了也沒地方放」。

**現在做法**：先寫 `analyzer replay --dataset <dir>`：每個 case 一個資料夾，`diff.json` 沿用 mock-gitlab 的 GitLab MR changes 格式，`expected.json` 標預期 verdict。只跑 deterministic agents（Selective Test L1、Rollout Risk），輸出 exact-match、HOLD precision、false-positive rate，`--json` 可機讀。種子資料就是 mock-gitlab 的四個 fixture（`testdata/replay/`），CI 每次 push 都跑一遍。其中 `04-t0demo` 的 expected 是 deterministic 路徑跑出來的**觀察基線**（PROCEED），不是 ground truth，README 明說。

**學到什麼**：**先讓量測管道存在，再逐步換上真資料**。用 mock 資料起步不是造假，只要每個 case 都誠實標明來源與可信度；真正的風險是把「觀察到的輸出」寫成「預期」卻不註明，那會讓 regression test 變成把現狀鎖死的儀式。資料集的價值在於它能被替換，不在於第一版有多真。

---

## 21. Replay import：expected 從 ReleaseGuard 自己的 comment 推得，匿名化只去身份

**最初想法**：要拿真實 MR 建 replay dataset，直覺是先設計一套人工標註流程——匯出 diff、開表單、請 reviewer 逐筆填「這個 MR 該 HOLD 還是 PROCEED」——並且為了能把資料放進 repo，把路徑與字串都做面罩。

**為什麼錯**：人工標註是以週計的工作，而且 GitLab 上早就有現成的標籤：ReleaseGuard 每次跑都把 verdict 貼在 MR comment 裡，reviewer 覺得誤報時會打 `releaseguard:false-positive` label（#18）。這兩個訊號合起來就是一份免費的標註。面罩路徑則會直接毀掉訊號——Rollout Risk 的 zone 判斷、Selective Test 的 L1 對應都靠路徑，面罩後跑出來的 verdict 不再是原本那個 MR 的 verdict。

**現在做法**：`analyzer replay-import --source gitlab` 拉已合併 MR，expected 取最新一則 ReleaseGuard note 的 verdict，帶 false-positive label 且原 verdict 為 HOLD/REVIEW 時改為 PROCEED；沒有 note 的 MR 預設跳過。`--source github` 只拿 diff，全部寫成 `needs_label`，等人補標，`replay` 把這類 case 排除在 metrics 外並另計 `unlabeled`。匿名化只做去身份：不寫標題、作者、描述、URL、note 內文；路徑與 patch 原文保留；case 用 hash 命名，hash 對照表 `.manifest.json` 與預設輸出目錄 `.replay/` 都不進 repo。GitLab notes 明確以 `sort=desc` 取回，確保「最新一次 run 的 verdict 贏」不是靠 API 預設排序。

**學到什麼**：**系統自己的輸出加上使用者的糾正，就是最便宜的標註資料**——前提是這兩個訊號從第一天就設計成機器可讀（#18 的 marker 字串與 label 名稱在這裡直接變成資料來源）。另一個邊界要講清楚：「去身份」和「匿名化程式碼」是兩件事，前者能自動做、後者會毀掉訊號；文件與 commit message 都應該用前者的字眼，不要讓讀者以為資料可以隨手公開。

---

## 跨決策的觀察

回頭看這 21 個決策，可以歸納出幾個**反覆出現的設計判斷模式**：

### 模式 A：collapse 在正確的層級
- Decision #5（recommendation 層 collapse）
- Decision #6（不在 evidence 層 collapse）

### 模式 B：boundary anticipation 比 enforcement 重要
- Decision #9（code/ vs corpus/ 預先分層）
- Decision #11（onboarding vs steady-state 分開但共用底層）

### 模式 C：技術可行性 ≠ 產品適當性
- Decision #4（Ownership 政治考量）
- Decision #12（approval rule 連帶移除）

### 模式 D：漸進式 onboarding 路徑
- Decision #7（拓樸 1）
- Decision #3（Selective L1/L2/L3）

### 模式 E：寫入紀律
- Decision #10（hot path 不寫回）
- Decision #11（backfill 用既有 pipeline）

### 反思：哪些決策應該更早做？

- **#5 Arbitration Layer 太晚加**：這是最大的產品 insight，但是別人提醒後才意識到。早期我太專注在「四個 agent 各自輸出」這個架構之美，忽略了使用者體驗。下次設計工具類產品，應該**先想第一眼使用者看到什麼，再倒推架構**。

- **#7 拓樸 1 太晚加**：「最小可用部署」這個視角應該是第一輪設計就有的。我初期假設了「所有人都會架 Postgres」這個 unrealistic 前提。

- **#4 Ownership 政治化**：這是被外部人指出後才意識到的盲點。對 senior 級別的設計判斷力而言，**組織政治敏感度應該是內建的**，不該等別人挑出來才修。

- **#14 Renderer 該帶證據**：第 5 條 arbitration 加上去的同時就應該意識到「collapse 不等於 hide」。把 evidence 攤平在 reviewer 眼前是 gating UX 的基本功，等 demo 跑出來被使用者點出才補，等於把可預見的問題交給 demo。

- **#17 長清單該收合不該截斷**：第 14 條只解了 zone 列表那層，但 selective test 的 required[] 過 5 個就消失這件事一直存在，全 repo coverage 撞上 28 個 test 才被注意到。同一條「collapse 不等於 hide」原則在不同欄位犯了第二次——表示我對「資訊層級」這個概念在第 14 條後仍沒徹底內化。

### 反思：哪些決策做對了？

- **#7 拓樸 1**（晚加但對）：在 Plan B 收斂前加上拓樸 1，讓 demo harness 能在零 infra 下跑——這個決定回頭看是 case_study 能成立的關鍵。沒有拓樸 1，整個作品集的「跑給你看」這條路就斷了。

- **#15 Lint 鎖死中性化**：用機械化方式保護人為決策不被靜默推翻，是最低成本的防回退。把「這個決定不能被善意推翻」這件事寫成 CI 規則，比寫在文件裡可靠 100 倍。

- **#5 Arbitration**：雖然加得晚，但加上後整個產品形狀清晰起來——四個 agent 的角色從「各自貢獻訊號」變成「服務於單一決策」，每個 agent 的設計問題（要不要 score、要不要 ranking）都有了可參照的標準。

- **#16 Plan C 重啟**：當作品集敘事與實作有落差時，選擇補實作而不是改敘事——這保護了規格的可信度。長遠來看，「規格說的我都做出來」比「我跑得通的我才寫進規格」對讀者的信任度高。
