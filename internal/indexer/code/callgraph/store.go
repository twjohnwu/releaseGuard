package callgraph

import (
	"context"
	"fmt"

	"github.com/acme/releaseguard/internal/storage"
)

func UpsertResult(ctx context.Context, pool *storage.Pool, repoID int64, r *BuildResult) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, s := range r.Symbols {
		if _, err := tx.Exec(ctx, `
			INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, signature, hash)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (id) DO UPDATE SET
				kind=EXCLUDED.kind, language=EXCLUDED.language, file=EXCLUDED.file,
				line_start=EXCLUDED.line_start, line_end=EXCLUDED.line_end,
				signature=EXCLUDED.signature, hash=EXCLUDED.hash, indexed_at=now()`,
			s.ID, repoID, s.Kind, s.Language, s.File, s.LineStart, s.LineEnd, s.Signature, s.Hash); err != nil {
			return fmt.Errorf("upsert symbol %s: %w", s.ID, err)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM edges WHERE caller IN (SELECT id FROM symbols WHERE repo_id=$1)", repoID); err != nil {
		return fmt.Errorf("clear edges: %w", err)
	}
	for _, e := range r.Edges {
		if _, err := tx.Exec(ctx, `
			INSERT INTO edges(caller, callee, call_file, call_line, kind)
			VALUES($1,$2,$3,$4,$5)`,
			e.Caller, e.Callee, e.CallFile, e.CallLine, e.Kind); err != nil {
			return fmt.Errorf("insert edge: %w", err)
		}
	}
	return tx.Commit(ctx)
}
