package coverage

import (
	"context"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

// Load matches entries to existing symbols by (file, line range overlap) and inserts.
// Skips entries whose file/line has no matching symbol.
func Load(ctx context.Context, pool *storage.Pool, repoID int64, sha string, entries []Entry) error {
	for _, e := range entries {
		if e.TestID == "" {
			continue
		}
		var symID string
		err := pool.QueryRow(ctx, `
			SELECT id FROM symbols
			WHERE repo_id=$1 AND file=$2
			AND line_start <= $3 AND line_end >= $3
			LIMIT 1`,
			repoID, e.File, e.LineStart).Scan(&symID)
		if err != nil {
			continue
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO coverage_map(repo_id, test_id, covered_symbol_id, last_seen_sha)
			VALUES($1,$2,$3,$4)
			ON CONFLICT (repo_id, test_id, covered_symbol_id)
			DO UPDATE SET last_seen_sha=EXCLUDED.last_seen_sha, updated_at=now()`,
			repoID, e.TestID, symID, sha)
		if err != nil {
			return err
		}
	}
	return nil
}
