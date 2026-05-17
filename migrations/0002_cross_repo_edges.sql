-- migrations/0002_cross_repo_edges.sql
CREATE TABLE IF NOT EXISTS cross_repo_edges (
  id                  BIGSERIAL PRIMARY KEY,
  source_repo_id      BIGINT NULL REFERENCES repos(id),
  target_repo_id      BIGINT NULL REFERENCES repos(id),
  source_alias        TEXT NOT NULL,
  target_alias        TEXT NOT NULL,
  call_kind           TEXT NOT NULL,
  source_diagram_path TEXT NOT NULL,
  indexed_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_xrepo_source ON cross_repo_edges(source_repo_id);
CREATE INDEX IF NOT EXISTS idx_xrepo_target ON cross_repo_edges(target_repo_id);
CREATE INDEX IF NOT EXISTS idx_xrepo_unresolved ON cross_repo_edges(source_alias, target_alias)
  WHERE source_repo_id IS NULL OR target_repo_id IS NULL;
