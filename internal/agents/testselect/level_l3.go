package testselect

import (
	"context"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func L3QueryRequired(ctx context.Context, pool *storage.Pool, repoID int64, changedSymbols []string) ([]string, float64, error) {
	rows, err := pool.Query(ctx, `
		WITH RECURSIVE upstream(sym, depth) AS (
		  SELECT s, 0 FROM unnest($1::text[]) AS s
		  UNION
		  SELECT e.caller, u.depth + 1
		  FROM edges e JOIN upstream u ON e.callee = u.sym
		  WHERE u.depth < 3
		)
		SELECT DISTINCT cm.test_id
		FROM coverage_map cm
		JOIN upstream u ON cm.covered_symbol_id = u.sym
		WHERE cm.repo_id = $2`, changedSymbols, repoID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var tests []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, 0, err
		}
		tests = append(tests, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var dynRatio float64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT count(*) FROM edges WHERE callee = ANY($1) AND kind='dynamic')::float
			/ NULLIF((SELECT count(*) FROM edges WHERE callee = ANY($1)), 0), 0)`,
		changedSymbols).Scan(&dynRatio); err != nil {
		return nil, 0, err
	}
	conf := L3BaseConfidence
	if dynRatio > 0.3 {
		conf -= 0.3
	}
	return tests, conf, nil
}
