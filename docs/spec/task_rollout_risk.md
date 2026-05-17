# Task: Rollout Risk Agent

## 新 repo 路徑

`Infra/releaseGuard/`

## 目的

判斷這次 MR 上 production 的「發布風險等級」並指出炸點。**四條**子分析平行進行：
1. API schema breaking（OpenAPI、Protobuf）
2. Dependency 升降版傳遞性影響
3. Config drift（與上次 release tag 比）
4. **Spec/Code drift detector**（code 改了、對應 spec 沒改 → warning）

不引入外部 schema registry — 上一版基準由 git `merge-base` 取得。

PoC 階段 Go-only，不做 TS mirror。

---

## 要新增的檔案

- `internal/agents/rollout/agent.go` — 主類；spawn 四條 goroutine，aggregate
- `internal/agents/rollout/agent_test.go`
- `internal/agents/rollout/risk.go` — riskLevel 計算
- `internal/analysis/schema/oasdiff.go` — `exec.Command("oasdiff", "diff", base, head, "--breaking-only")`
- `internal/analysis/schema/protobuf.go` — `exec.Command("buf", "breaking", ...)`
- `internal/analysis/schema/locator.go` — 找出 repo 中的 OpenAPI / .proto 檔
- `internal/analysis/depgraph/gomod.go` — 解析 `go.mod` 前後版差異
- `internal/analysis/depgraph/npm.go` — 解析 `package.json` + `package-lock.json`
- `internal/analysis/depgraph/transitive.go` — 計算傳遞影響（深度上限 2）
- `internal/analysis/configdrift/diff.go` — `git diff <last-release-tag>..HEAD -- 'config/**'`
- `internal/analysis/specdrift/detector.go` — code 改 vs spec 改的對齊偵測
- `internal/analysis/specdrift/handler_locator.go` — 找出 endpoint handler 函式（Go：route registration、TS：decorator / framework-specific）
- `internal/analysis/aliases/lookup.go` — 共用 helper：透過 `diagram_aliases.yaml` 反查 alias → repo（PlantUML parser 也用同一份，避免重複實作）

---

## 四條子分析

| 編號 | 子分析 | 嚴重度範圍 |
|---|---|---|
| A | API Schema Breaking（OpenAPI / Protobuf） | low ~ critical |
| B | Dependency 升降版傳遞性 | low ~ high |
| C | Config Drift（與上次 release tag 比） | low ~ critical |
| D | Spec/Code Drift（code 改了 spec 沒改） | medium |

### A. API Schema Breaking

```
locate openapi files (glob: **/openapi.yaml, api/*.yaml)
locate proto files (glob: **/*.proto)

for each schema file:
  base = git show $MERGE_BASE:$path
  head = current
  if openapi: oasdiff diff <base> <head> --breaking-only -f json
  if proto:   buf breaking --against <base> -o json

zones += { type: "breaking_api", detail, severity: "critical" } per breaking change
```

### B. Dependency

```
parse go.mod / package.json before & after
for each (module, old_version, new_version):
  if major bump: severity = "high"
  elif minor bump and module in critical_list: severity = "medium"
  else: severity = "low"
  
  if transitive: detail += " (affects N modules)"
  zones += { type: "dep_bump", ... }
```

### C. Config Drift

```
git diff $LAST_RELEASE_TAG..HEAD -- 'config/**' '*.env' 'helm/**' 'k8s/**'
classify per file:
  - secrets pattern → critical
  - resource limits → high
  - feature flags → medium
  - other → low
zones += { type: "config_drift", ... }
```

### D. Spec/Code Drift Detector

**只偵測單向**：code 改了、對應 OpenAPI / Protobuf spec 沒改 → 提示 committer 去更新 spec。

反向（spec 改但 code 沒實作）**不在 PoC scope**——誤報率太高（合理情境：先寫 spec 再實作）。詳見 plan.md「Known Limitations」。

```
1. handler_locator 找出 diff 內動到的 endpoint handler 函式
   Go:  在 route registration（如 mux.Handle / chi.Get）反查
   TS:  framework-specific（NestJS @Controller / Express app.get）

2. 對每個動到的 handler:
   - 從 endpoint path 推 spec 中對應位置（e.g. POST /v2/orders → openapi.yaml 內 paths./v2/orders.post）
   - 檢查該 spec 區塊是否在這次 diff 內也被改動

3. 如果 handler 改了 + spec 沒改:
   finding = {
     severity: "medium",
     category: "spec_code_drift",
     title: "Endpoint handler changed without updating spec",
     body: "...",
     suggestion: "Update <spec_file> to reflect changes in <handler_func>"
   }

zones += { type: "spec_code_drift", detail, severity: "medium" } per drift

# affected_services 是 agent metadata 級（whole-agent 一份），
# 不寫進 finding；由 aliases.Lookup 從 cross_repo_edges 反查後累積到 metadata.affected_services
```

---

## riskLevel 計算

```
counts = count zones by severity   // 含 A/B/C/D 全部 zone type，
                                   // spec_code_drift（D）的 medium 也算進 counts.medium
if counts.critical >= 1: riskLevel = "HIGH"
elif counts.high >= 2 OR counts.medium >= 3: riskLevel = "MED"
else: riskLevel = "LOW"
```

---

## Mitigation 建議

對每種 zone type 套用 template：

| Type | Mitigation Template |
|---|---|
| `breaking_api` (field removal) | "Add `Deprecation` header for {N} days before removing `{field}`" |
| `breaking_api` (type change) | "Version the endpoint (e.g. `/v3/{path}`) and keep `/v2/` running" |
| `dep_bump` (major) | "Run integration test against staging before merge" |
| `config_drift` (secrets) | "Rotate compromised secret if leaked" |

---

## JSON 輸出

```json
{
  "agent": "rollout_risk",
  "status": "ok",
  "duration_ms": 1840,
  "schema_version": "1",
  "summary": "1 critical breaking change in /v2/orders",
  "findings": [
    {
      "id": "rl-001",
      "stable_id": "rollout_risk:breaking_api:openapi.yaml:POST /v2/orders:discount_code",
      "severity": "critical",
      "category": "breaking_api",
      "title": "POST /v2/orders removed field discount_code",
      "body": "Field `discount_code` was removed. Downstream: checkout-svc, promo-svc.",
      "location": { "file": "api/openapi.yaml", "line_start": 142 },
      "suggestion": "Add Deprecation header for 30 days before removing discount_code"
    }
  ],
  "metadata": {
    "riskLevel": "HIGH",
    "zones": [
      { "type": "breaking_api", "detail": "...", "severity": "critical" },
      { "type": "dep_bump", "detail": "lodash 4.17.20 → 4.17.21 (3 transitive)", "severity": "low" },
      { "type": "spec_code_drift", "detail": "POST /v2/orders handler changed, openapi.yaml not updated", "severity": "medium" }
    ],
    "affected_services": ["checkout-svc", "promo-svc"]
  }
}
```

---

## 驗證

### Unit
- 構造一份 OpenAPI before/after，移除一個 required 欄位 → 預期 status=ok、riskLevel=HIGH
- `go.mod` major bump → 預期 zone severity=high

### Integration
- Fixture：在任一註冊 repo（例如 `acme/orders`）建一個 fixture PR，刪除一個 OpenAPI 欄位、升 lodash major、改 endpoint handler 但沒同步 spec
- 跑 analyzer，預期：
  - `mr_runs.risk_level = HIGH`
  - `agent_outputs` 有 rollout_risk row、status=ok
  - `findings` 含 1 critical breaking_api + 1 medium spec_code_drift 條目
  - 若 `DOCS_REPO_NAMES` 設定且 `cross_repo_edges` 有資料 → `affected_services` 欄位有值

---

## 與 DOCS_REPO_NAMES 的關係

- `DOCS_REPO_NAMES`（plan.md 環境變數）標出哪些 repo 是中央 spec repo
- **drift detector 不在文件 repo 跑**——只在 service repo MR 跑
- 文件 repo 自己的 MR 不接 ReleaseGuard
- `affected_services` 透過 `internal/analysis/aliases/lookup.go` 從 `cross_repo_edges` 反查（PlantUML parser 灌入的資料）
