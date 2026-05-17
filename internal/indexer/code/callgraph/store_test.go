package callgraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestUpsertSymbolsAndEdgesIdempotent(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../../migrations")

	pool.Exec(ctx, "INSERT INTO repos(name, languages) VALUES('test', '[\"go\"]') ON CONFLICT DO NOTHING")
	var repoID int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='test'").Scan(&repoID)

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
	pool.QueryRow(ctx, "SELECT count(*) FROM symbols WHERE id='x.Foo'").Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
}
