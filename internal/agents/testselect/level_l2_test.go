package testselect

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func TestL2QueriesCoverageMap(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "INSERT INTO repos(name) VALUES('l2-test') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	var rid int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='l2-test'").Scan(&rid); err != nil {
		t.Fatalf("select repo: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, hash)
		VALUES('l2.X', $1, 'function', 'go', 'foo.go', 1, 5, 'h') ON CONFLICT DO NOTHING`, rid); err != nil {
		t.Fatalf("insert symbol: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO coverage_map(repo_id, test_id, covered_symbol_id, last_seen_sha)
		VALUES($1, 'TestCovered', 'l2.X', 'sha1') ON CONFLICT DO NOTHING`, rid); err != nil {
		t.Fatalf("insert coverage: %v", err)
	}

	required, err := L2QueryRequired(ctx, pool, rid, []string{"foo.go"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(required) != 1 || required[0] != "TestCovered" {
		t.Fatalf("unexpected: %+v", required)
	}
}
