# AGENTS.md

Project-level guidance for AI coding agents (Claude Code, Cursor, Copilot, Codex, etc.). Read this first.

## What this project is

ReleaseGuard is an MR-level release-gating system. Four specialised agents (Selective Test, Rollout Risk, Ownership, AI Reviewer) run in parallel; a Decision Arbitration Layer collapses their signals into a single **HOLD / REVIEW / PROCEED** recommendation posted at the top of the MR comment.

- **Language:** Go-only. No TypeScript / Python mirror — cross-language contracts live as neutral JSON Schema in `docs/spec/`.
- **Topologies:** T0 (Postgres + indexer, full capability) and T1 (zero-infra script-only, degraded). Selected by the `TOPOLOGY` env var.
- **Status:** Plans A / B / C complete (see CHANGELOG.md; git history was squashed at the first commit, so there are no per-plan tags). Plan D (RAG) has a written spec but is **not implemented**.

## Read first

| File | When to read it |
|---|---|
| `docs/decisions_log.md` | **Before changing any decided behaviour.** Eighteen design pivots in "initial → why wrong → current → lesson" form. Many tempting refactors are already discussed and intentional. |
| `docs/architecture.md` | 30-second orientation: three mermaid diagrams (pipeline / topology / decision matrix). |
| `docs/learnings.md` | Design reflections — the "why behind the why". |
| `docs/case_study.md` | Three real MR-comment outcomes + Topology 0 integration validation. |
| `docs/spec/` | Full design spec + implementation plans. Authoritative *intent*; the code is authoritative *fact*. |

Most narrative docs are in 繁體中文. Code, comments, and commit messages are English. This file is English for context efficiency.

## Standard operations

```bash
# Build & test (must pass before every commit)
make build
go test ./... -count=1

# T1 demo (no infra): three analyzers vs mock-gitlab → 3 MR comments
cd deploy/compose && mkdir -p artifacts
docker compose up --build --abort-on-container-exit

# T0 demo (Postgres + indexer): produces analysis_level=L3, confidence=0.90
bash scripts/demo-t0.sh

# Rebuild coverage_map from real `go test -coverprofile`
bash scripts/gen-coverage.sh   # → /tmp/rg-cov/all.lcov
COVERAGE_ARTIFACT_PATH=/tmp/rg-cov/all.lcov \
  POSTGRES_URL="postgresql://postgres:postgres@localhost:5432/releaseguard_dev" \
  MIGRATIONS_DIR="$(pwd)/migrations" \
  ./bin/indexer nightly --repo=releaseGuard
```

mock-gitlab routes fixtures by `project_id`: `1=proceed`, `2=review`, `3=hold`, `4=t0demo`.

## Local environment prerequisites

- **Postgres:** the operator's local docker container `local-postgres` (image `postgres:latest`, PG 14.5) is already running on `localhost:5432`. Reuse it; do not start a new container. Database for this project: `releaseguard_dev`. Credentials: `postgres / postgres`.
- **pgvector is NOT installed.** If you implement Plan D (RAG), expect to either install it or fall back to `double precision[]` with similarity in Go.
- **No OpenAI / Anthropic key in this environment.** AI Reviewer is disabled in demos (`RG_AGENT_AI_REVIEWER_ENABLED=false`). Do not assume external API calls work.
- **`gh` CLI is not installed.** GitHub metadata changes must go through the web UI or be deferred to the operator.

## Coding conventions

**Env access.** Go through `internal/config/env.go` (`config.Load()` → `cfg.X`). `grep "os.Getenv" cmd/analyzer/main.go` must return zero. To add a new env var, extend the `Config` struct and parse it in `Load()`.

**Severity.** String → enum via `interfaces.ParseSeverity(s)`. Enum → string via plain `string(s)` (Severity *is* `type Severity string`). No new inline switches. (See decisions_log §S3.)

**Indexer steps.** Add to the `steps` slice in `cmd/indexer/nightly.go`. Signature: `func(ctx, *storage.Pool, repoID int64, repoPath string) error`. The orchestrator does not need editing (OCP).

**Long lists in MR rendering.** Use `<details><summary>… N more</summary>…</details>`. Never silently truncate with a `… N more` plain-text line. (See decisions_log #17.)

**Agent output.** `interfaces.AgentOutput.SchemaVersion = "1"` is a fixed constant. Add new fields to `Metadata map[string]any`, not to the struct — the schema is a cross-language contract.

**Ownership agent output.** Must not contain `score` or `kind` fields. `TestOutputSchemaHasNoScoreField` will fail. (See decisions_log #4 and #15 for the reasoning.)

## Invariants (lint- or logic-enforced — do not break)

| Invariant | Guard |
|---|---|
| Ownership output has no `score` / `kind` | `TestOutputSchemaHasNoScoreField` lint test |
| L1 partial never triggers REVIEW | Rule in `internal/report/arbitration.go` |
| Ownership signals never escalate the decision | Same rule set |
| `SchemaVersion == "1"` | Cross-language contract stability |
| `symbols.file` is repo-relative and non-empty | L3 lookup `WHERE file = ANY($)` depends on it |
| `go test ./... -count=1` is green | Required before every commit |

## Common task recipes

**Add a new sub-analysis (e.g. licence drift).** Add a package under `internal/analysis/`. Append zones in `collectRolloutZones()` in `cmd/analyzer/main.go`. Add unit tests. The arbitration, renderer, and agents do not need to know about the new source.

**Add a new agent.** Implement `interfaces.IAgent`. Wire it in `buildAgents()` in `cmd/analyzer/main.go`. Add an enable flag in `config.AgentFlags`. Decide in `internal/report/arbitration.go` whether the agent's signals escalate the decision — the default should usually be "no" (advisory only).

**Change AI Reviewer prompt or model behaviour.** Files live in `internal/agents/reviewer/`. The Anthropic client uses tool-use mode (`tool_choice: {type: "tool", name: …}`) to force JSON output.

## Don't do this

- ❌ Downgrade `go 1.25` in `go.mod` — pgx v5.9.2 requires it.
- ❌ Add a TypeScript / Python mirror — decisions_log #2 explicitly decided Go-only.
- ❌ Add scoring / ranking / approval-rule writes to the Ownership agent — decisions_log #4, #12, #15.
- ❌ Make Selective Test "DB present → L3, DB absent → error" — the L1/L2/L3 confidence ladder (decisions_log #3) is core architecture.
- ❌ Write RAG hot-path results back to Postgres — decisions_log #10.
- ❌ Hard-code `/migrations` — read `MIGRATIONS_DIR` (commit `ff5c484` fixed this).
- ❌ Use `ssautil.Packages` — must be `ssautil.AllPackages` for transitive stdlib (same commit).
- ❌ Read env directly in `cmd/analyzer/main.go` — go through `cfg`.
- ❌ Mix Chinese and English in user-facing docs (`README.md` is English, `README.zh-TW.md` is Chinese, `docs/*` is Chinese with English technical tokens).
- ❌ Rewrite git history / force-push / skip hooks (`--no-verify`).

## Spec vs decisions_log vs code

- `docs/spec/` is the **intended design** (normative).
- `docs/decisions_log.md` is the **design history** (narrative, "why we landed here").
- The **code is the source of truth.** When they disagree, trust the code and update the docs.

If you make a non-trivial design pivot, append a new entry to `decisions_log.md` (continue numbering past #17).

## Doc language convention

- Code / comments / commit messages: English.
- Spec / design reflections / chat-style narrative docs: 繁體中文 with English technical tokens (HOLD/REVIEW/PROCEED, Topology, file paths, env var names).
- README: bilingual — `README.md` (English, default) + `README.zh-TW.md` (繁體中文) with a language switcher at the top of both.
- `AGENTS.md` (this file): English, for context efficiency in AI tools.
