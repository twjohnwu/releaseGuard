package testselect

import (
	"context"

	"github.com/acme/releaseguard/internal/storage"
)

func L2QueryRequired(ctx context.Context, pool *storage.Pool, repoID int64, files []string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT cm.test_id
		FROM coverage_map cm
		JOIN symbols s ON cm.covered_symbol_id = s.id
		WHERE cm.repo_id = $1 AND s.file = ANY($2)`,
		repoID, files)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
