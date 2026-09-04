package testselect

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func TestL3ReverseBFSAndConfidence(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := storage.NewPool(ctx, url)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()
	if err := storage.MigrateUp(ctx, pool, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := pool.Exec(ctx, "INSERT INTO repos(name) VALUES('l3-test') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	var rid int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='l3-test'").Scan(&rid); err != nil {
		t.Fatalf("select repo: %v", err)
	}
	for _, id := range []string{"l3.A", "l3.B", "l3.C"} {
		if _, err := pool.Exec(ctx, `INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, hash)
			VALUES($1, $2, 'function', 'go', $3, 1, 5, 'h') ON CONFLICT DO NOTHING`, id, rid, id+".go"); err != nil {
			t.Fatalf("insert symbol: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, "DELETE FROM edges WHERE caller IN ('l3.A','l3.B','l3.C')"); err != nil {
		t.Fatalf("delete edges: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO edges(caller, callee, call_file, call_line, kind) VALUES
		('l3.A','l3.B','x',1,'direct'),
		('l3.B','l3.C','y',1,'direct')`); err != nil {
		t.Fatalf("insert edges: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO coverage_map(repo_id, test_id, covered_symbol_id, last_seen_sha)
		VALUES($1, 'TestA', 'l3.A', 'sha1') ON CONFLICT DO NOTHING`, rid); err != nil {
		t.Fatalf("insert coverage: %v", err)
	}

	required, conf, err := L3QueryRequired(ctx, pool, rid, []string{"l3.C"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(required) != 1 || required[0] != "TestA" {
		t.Fatalf("expected TestA, got %v", required)
	}
	if conf < 0.85 {
		t.Fatalf("confidence too low: %f", conf)
	}
}
