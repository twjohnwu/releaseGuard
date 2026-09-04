package callgraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func TestUpsertSymbolsAndEdgesIdempotent(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL not set")
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

	if _, err := pool.Exec(ctx, "INSERT INTO repos(name, languages) VALUES('test', '[\"go\"]') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	var repoID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='test'").Scan(&repoID); err != nil {
		t.Fatalf("select repo: %v", err)
	}

	res := &BuildResult{
		Symbols: []Symbol{{ID: "x.Foo", Kind: "function", Language: "go", File: "f", LineStart: 1, LineEnd: 5, Hash: "h1"}},
	}
	if err := UpsertResult(ctx, pool, repoID, res); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := UpsertResult(ctx, pool, repoID, res); err != nil {
		t.Fatalf("idempotent upsert: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM symbols WHERE id='x.Foo'").Scan(&n); err != nil {
		t.Fatalf("count symbols: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
}
