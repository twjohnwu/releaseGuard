package coverage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func TestLoadEntriesIntoCoverageMap(t *testing.T) {
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
	if err := storage.MigrateUp(ctx, pool, "../../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := pool.Exec(ctx, "INSERT INTO repos(name) VALUES('cov-test') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	var rid int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='cov-test'").Scan(&rid); err != nil {
		t.Fatalf("select repo: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, hash)
		VALUES('foo.Bar', $1, 'function', 'go', 'foo.go', 1, 5, 'h')
		ON CONFLICT (id) DO NOTHING`, rid); err != nil {
		t.Fatalf("insert symbol: %v", err)
	}

	entries := []Entry{{TestID: "TestX", File: "foo.go", FunctionName: "Bar", LineStart: 1}}
	if err := Load(ctx, pool, rid, "abc123", entries); err != nil {
		t.Fatalf("load: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM coverage_map WHERE test_id='TestX'").Scan(&n); err != nil {
		t.Fatalf("count coverage: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}
