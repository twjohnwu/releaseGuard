-- migrations/0005_ownership.sql
CREATE TABLE IF NOT EXISTS ownership_signals (
  id                BIGSERIAL PRIMARY KEY,
  repo_id           BIGINT NOT NULL REFERENCES repos(id),
  file_path         TEXT NOT NULL,
  author            TEXT NOT NULL,
  blame_weight      NUMERIC(5,4) NOT NULL,
  co_change_files   JSONB NOT NULL DEFAULT '[]'::jsonb,
  recency_score     NUMERIC(5,4) NOT NULL,
  computed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (repo_id, file_path, author)
);
CREATE INDEX IF NOT EXISTS idx_owner_repo_file ON ownership_signals(repo_id, file_path);
CREATE INDEX IF NOT EXISTS idx_owner_repo_author ON ownership_signals(repo_id, author);
