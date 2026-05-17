-- migrations/0004_coverage.sql
CREATE TABLE IF NOT EXISTS coverage_map (
  id                  BIGSERIAL PRIMARY KEY,
  repo_id             BIGINT NOT NULL REFERENCES repos(id),
  test_id             TEXT NOT NULL,
  covered_symbol_id   TEXT NOT NULL REFERENCES symbols(id),
  last_seen_sha       TEXT NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (repo_id, test_id, covered_symbol_id)
);
CREATE INDEX IF NOT EXISTS idx_coverage_symbol ON coverage_map(covered_symbol_id);
CREATE INDEX IF NOT EXISTS idx_coverage_repo_test ON coverage_map(repo_id, test_id);
