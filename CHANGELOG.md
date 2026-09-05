## [Unreleased]

Fixed:
- `releaseguard-report.json` was written 0600 (from `os.CreateTemp`), so CI could not upload the demo artifacts written by the root container; report files are now 0644 and the docker-smoke job chowns the artifacts dir before upload

## [0.1.0] - 2026-09-04

First tagged release. Closes the productize round driven by an external review.

Added:
- GitHub Actions CI: `go test -race`, `go vet`, `make build`; golangci-lint v2; docker smoke of the T1 compose demo asserting HOLD / REVIEW / PROCEED
- `analyzer replay --dataset <dir>`: precision / false-positive measurement over recorded MR diffs; seed dataset `testdata/replay/` from the mock-gitlab fixtures
- Per-agent timeout `AGENT_TIMEOUT_SEC` (defaults to `ANALYZE_TIMEOUT_SEC`), per-goroutine panic recovery, one `agent done` log line per agent
- README badges (CI, release, Go) and replay docs
- `analyzer replay-import --source gitlab|github`: import merged MRs/PRs as identity-stripped replay cases; GitLab expected verdicts come from ReleaseGuard's own comment + `releaseguard:false-positive` label; `replay` reports `unlabeled` cases separately
- `release.yml`: publish `ghcr.io/twjohnwu/releaseguard-{analyzer,indexer}` on every `v*` tag
- CI gofmt gate
- `replay-import --force`; hand-labelled `expected.json` files are preserved on re-import by default

Changed:
- Module path `github.com/acme/releaseguard` → `github.com/twjohnwu/releaseGuard`
- Deploy manifest image → `ghcr.io/twjohnwu/releaseguard-analyzer`
- All golangci-lint v2 findings resolved (errcheck / staticcheck / ineffassign); error paths now propagate or log instead of being ignored

Fixed:
- `Arbitrate` returned an empty Recommendation after a recovered panic; now fails closed to REVIEW with an `arbitration_panic` signal (decisions_log #19)
- A panicking or hung agent could crash or stall the whole analyzer
- README / AGENTS.md / CHANGELOG referenced git tags that never existed
- GitLab MR diff fetch was unpaginated (default 20 files), truncating large MRs on the production analysis path and in replay-import; now pages through all changes

## Plan A status: complete (pre-squash history; no tag)

Foundation layer in place:
- Shared interfaces (Finding / AgentOutput / IAgent)
- Config loader with env validation + topology check
- Postgres pool helper (PgBouncer-safe via QueryExecModeSimpleProtocol)
- Migration runner + initial schema (repos table)
- GitLab REST client (GetMRDiff + PostMRNote)
- AI Provider interface + Anthropic implementation + retry helper
- Structured JSON logger
- cmd/analyzer entry with topology validation
- Multi-stage Dockerfile (golang:1.25-alpine baseline due to pgx toolchain)
- GitLab CI template (.releaseguard-full + -lite)
- e2e smoke test passing

## Plan B status: complete (pre-squash history; no tag)

Topology 1 demoable:
- PlantUML parser + diagram_aliases.yaml + cross_repo_edges schema
- Selective Test L1 (no DB)
- Rollout Risk + spec/code drift detector + 4 sub-analyses
- AI Reviewer Channel A only (with stable_id calculation)
- Decision Arbitration Layer (HOLD/REVIEW/PROCEED + triggered_signals)
- Composer + Markdown Renderer + Poster (2 sinks)
- All agents wired in cmd/analyzer/main.go with parallel execution
- e2e topology 1 smoke test passing

## Plan C status: complete (pre-squash history; no tag)

Topology 0 (Postgres-backed) infrastructure:
- 4 migrations: symbols + edges (callgraph), coverage_map, ownership_signals, mr_runs/agent_outputs/findings
- cmd/indexer with subcommand dispatch (nightly, backfill stub) + Dockerfile
- internal/indexer/code/ subsystems: callgraph (CHA), coverage parser (LCOV), cochange (blame + matrix), plantuml step
- Selective Test L2 (coverage_map intersect) + L3 (reverse BFS on edges) with confidence calculation
- Ownership Agent in proximity mode (no score, no kind, shuffled output, banned ranking words enforced by lint)
- analyzer wires NewWithDB testselect + ownership when POSTGRES_URL configured

## Hardening round: complete (commits 6526efb, 0964ed0)

Naming, transparency, and feedback-loop pass (Plan D / RAG remains spec-only):
- Renamed caller-provided env vars TARGET_SERVICE_* → RG_SERVICE_* to match the RG_ prefix convention (breaking: callers must update their .gitlab-ci.yml variables)
- RG_AI_MODEL env replaces the hardcoded Anthropic model id (default claude-sonnet-4-6); Cost section added to both READMEs
- Selective Test confidence values centralized as named constants (testselect/confidence.go); docs aligned to actual L1=0.5
- SPEC ONLY — NOT IMPLEMENTED banners on Channel B / self-reflection / Plan D specs; implementation-status table in both READMEs
- RG_REPORT_PATH writes a per-run JSON report (decision, triggered signals, agent outputs), exposed as a GitLab CI artifact
- False-positive feedback loop: HOLD/REVIEW comment footer invites the releaseguard:false-positive label; new `analyzer feedback` subcommand reports HOLD precision (see decisions_log #18)
