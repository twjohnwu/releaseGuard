-- migrations/0006_runs.sql
CREATE TABLE IF NOT EXISTS mr_runs (
  id              BIGSERIAL PRIMARY KEY,
  repo_id         BIGINT NOT NULL REFERENCES repos(id),
  mr_iid          INT NOT NULL,
  commit_sha      TEXT NOT NULL,
  started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at     TIMESTAMPTZ NULL,
  status          TEXT NOT NULL DEFAULT 'running',
  risk_level      TEXT NULL,
  recommendation  TEXT NULL,
  agents_enabled  JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_mrruns_repo_mr ON mr_runs(repo_id, mr_iid, started_at DESC);

CREATE TABLE IF NOT EXISTS agent_outputs (
  id            BIGSERIAL PRIMARY KEY,
  mr_run_id     BIGINT NOT NULL REFERENCES mr_runs(id),
  agent         TEXT NOT NULL,
  status        TEXT NOT NULL,
  duration_ms   INT NOT NULL,
  payload       JSONB NOT NULL,
  UNIQUE (mr_run_id, agent)
);
CREATE INDEX IF NOT EXISTS idx_agent_outputs_agent ON agent_outputs(agent, status);

CREATE TABLE IF NOT EXISTS findings (
  id                  BIGSERIAL PRIMARY KEY,
  mr_run_id           BIGINT NOT NULL REFERENCES mr_runs(id),
  agent               TEXT NOT NULL,
  severity            TEXT NOT NULL,
  category            TEXT NOT NULL,
  title               TEXT NOT NULL,
  body                TEXT NOT NULL,
  location_file       TEXT,
  location_line_start INT,
  location_line_end   INT,
  suggestion          TEXT,
  stable_id           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_findings_stable ON findings(stable_id);
CREATE INDEX IF NOT EXISTS idx_findings_run ON findings(mr_run_id, severity);
