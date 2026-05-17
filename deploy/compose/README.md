# Local end-to-end harness

Run the **analyzer** against a fake GitLab so you can see real MR comments without wiring a real instance.

This harness validates **Plan B's topology 1** (zero-infra mode):
- Selective Test L1 + Rollout Risk + spec/code drift + config drift + composer + arbitration + poster
- AI Reviewer is disabled (no real Anthropic key needed)
- No Postgres, no indexer

The compose stack runs **three analyzer instances in parallel** against the mock — one for each
arbitration outcome (PROCEED / REVIEW / HOLD) — so a single `docker compose up` produces all three
report flavors side by side.

## Quick run

```bash
cd deploy/compose
mkdir -p artifacts
docker compose up --build --abort-on-container-exit
```

Each analyzer container is short-lived: it runs once, posts a note to the mock, and exits.
The mock keeps running until you Ctrl-C or `docker compose down`.

## What you'll see

After `docker compose up --abort-on-container-exit`, three analyzer containers ran in parallel
against the mock. The `artifacts/` directory will contain:

| File | From | Recommendation |
|---|---|---|
| `note-proj1-mr1-*.md` | analyzer-proceed | PROCEED — pure docs change |
| `note-proj2-mr2-*.md` | analyzer-review  | REVIEW — handler changed, spec untouched |
| `note-proj3-mr3-*.md` | analyzer-hold    | HOLD — secrets config touched + handler drift |

Compare them to see how the Decision Arbitration Layer collapses different signals into different
recommendations.

## Inspect

```bash
# All three reports side by side
for f in artifacts/note-proj*.md; do echo "=== $f ==="; cat "$f"; echo; done

# Append-only log of all three posts
cat artifacts/notes.log

# The selective-test plan artifact (only present when confidence >= threshold)
cat artifacts/selective-tests.txt 2>/dev/null
```

The analyzer's stdout/stderr (JSON-structured slog lines) are visible in `docker compose` output.

## Fixtures and project mapping

The mock server in `mock-gitlab/main.go` maps the requested `project_id` to a fixture file:

| project_id | fixture                            | what's in it                                      |
|------------|------------------------------------|---------------------------------------------------|
| 1          | `fixtures/diff-proceed.json`       | docs only (`README.md`, `docs/usage.md`)          |
| 2          | `fixtures/diff-review.json`        | handler change without matching spec update      |
| 3          | `fixtures/diff-hold.json`          | `config/secrets.yaml` + handler drift (critical)  |

Edit any fixture and re-run — the mock reads the file on every request, no rebuild needed.

If you change the **mock server code** (`mock-gitlab/main.go`), you do need a rebuild:

```bash
docker compose build mock-gitlab
docker compose up
```

## Resetting

```bash
docker compose down -v
rm -rf artifacts/*
```

## Enabling AI Reviewer (optional)

Set a real Anthropic key in `docker-compose.yaml` (under `x-analyzer-env`):

```yaml
x-analyzer-env: &analyzer-env
  AI_PROVIDER_KEY: "sk-ant-..."
  RG_AGENT_AI_REVIEWER_ENABLED: "true"
```

Note: the analyzer expects `PROJECTS_DIR` to contain `.md` prompt files. If you enable AI Reviewer
without a `projects/` mount, the loader will return an empty prompt set and the LLM call may produce
uncalibrated output.

## Files

- `docker-compose.yaml` — service definitions (1 mock + 3 analyzer instances via YAML anchors)
- `mock-gitlab/main.go` — minimal Go HTTP server, project_id-aware fixture routing
- `mock-gitlab/Dockerfile` — mock server image
- `mock-gitlab/fixtures/diff-proceed.json` — docs-only diff (PROCEED)
- `mock-gitlab/fixtures/diff-review.json`  — handler-without-spec drift (REVIEW)
- `mock-gitlab/fixtures/diff-hold.json`    — secrets + handler drift (HOLD)
- `artifacts/` — output directory (created on first run, gitignored)
