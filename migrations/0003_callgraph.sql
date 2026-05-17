-- migrations/0003_callgraph.sql
CREATE TABLE IF NOT EXISTS symbols (
  id          TEXT PRIMARY KEY,
  repo_id     BIGINT NOT NULL REFERENCES repos(id),
  kind        TEXT NOT NULL,
  language    TEXT NOT NULL,
  file        TEXT NOT NULL,
  line_start  INT NOT NULL,
  line_end    INT NOT NULL,
  signature   TEXT,
  hash        TEXT NOT NULL,
  indexed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_symbols_repo_file ON symbols(repo_id, file);
CREATE INDEX IF NOT EXISTS idx_symbols_hash ON symbols(hash);

CREATE TABLE IF NOT EXISTS edges (
  id         BIGSERIAL PRIMARY KEY,
  caller     TEXT NOT NULL REFERENCES symbols(id),
  callee     TEXT NOT NULL REFERENCES symbols(id),
  call_file  TEXT NOT NULL,
  call_line  INT NOT NULL,
  kind       TEXT NOT NULL  -- direct|interface|dynamic
);
CREATE INDEX IF NOT EXISTS idx_edges_callee ON edges(callee);
CREATE INDEX IF NOT EXISTS idx_edges_caller ON edges(caller);
CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(kind);
