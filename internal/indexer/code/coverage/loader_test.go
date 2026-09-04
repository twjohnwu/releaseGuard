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
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../../migrations")

	pool.Exec(ctx, "INSERT INTO repos(name) VALUES('cov-test') ON CONFLICT DO NOTHING")
	var rid int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='cov-test'").Scan(&rid)
	pool.Exec(ctx, `INSERT INTO symbols(id, repo_id, kind, language, file, line_start, line_end, hash)
		VALUES('foo.Bar', $1, 'function', 'go', 'foo.go', 1, 5, 'h')
		ON CONFLICT (id) DO NOTHING`, rid)

	entries := []Entry{{TestID: "TestX", File: "foo.go", FunctionName: "Bar", LineStart: 1}}
	if err := Load(ctx, pool, rid, "abc123", entries); err != nil {
		t.Fatalf("load: %v", err)
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM coverage_map WHERE test_id='TestX'").Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}
