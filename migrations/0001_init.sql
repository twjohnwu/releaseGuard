-- migrations/0001_init.sql
CREATE TABLE IF NOT EXISTS repos (
  id              BIGSERIAL PRIMARY KEY,
  name            TEXT UNIQUE NOT NULL,
  languages       JSONB NOT NULL DEFAULT '[]'::jsonb,
  indexer_config  JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
