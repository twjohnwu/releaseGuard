## Plan A status: complete (commit efe202a, tag plan-a-foundation)

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

## Plan B status: complete (commit 82b7b9c, tag plan-b-topology-1)

Topology 1 demoable:
- PlantUML parser + diagram_aliases.yaml + cross_repo_edges schema
- Selective Test L1 (no DB)
- Rollout Risk + spec/code drift detector + 4 sub-analyses
- AI Reviewer Channel A only (with stable_id calculation)
- Decision Arbitration Layer (HOLD/REVIEW/PROCEED + triggered_signals)
- Composer + Markdown Renderer + Poster (2 sinks)
- All agents wired in cmd/analyzer/main.go with parallel execution
- e2e topology 1 smoke test passing

## Plan C status: complete (commit 461b041, tag plan-c-postgres-agents)

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
