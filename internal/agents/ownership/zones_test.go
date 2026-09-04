package ownership

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func TestZonesGroupsByTopLevelDir(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "INSERT INTO repos(name) VALUES('zones-test') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	var rid int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='zones-test'").Scan(&rid); err != nil {
		t.Fatalf("select repo: %v", err)
	}
	for _, a := range []string{"a@x.com", "b@x.com", "c@x.com"} {
		if _, err := pool.Exec(ctx, `INSERT INTO ownership_signals(repo_id, file_path, author, blame_weight, recency_score)
			VALUES($1, 'orders/handler.go', $2, 0.3, 0.9) ON CONFLICT DO NOTHING`, rid, a); err != nil {
			t.Fatalf("insert ownership signal: %v", err)
		}
	}
	zs, err := computeZones(ctx, pool, rid, []string{"orders/handler.go", "payments/svc.go"})
	if err != nil {
		t.Fatalf("computeZones: %v", err)
	}
	if len(zs) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(zs))
	}
	for _, z := range zs {
		if z.Path == "orders/" && z.RecentActiveAuthorsCount != 3 {
			t.Errorf("orders/: expected 3 authors, got %d", z.RecentActiveAuthorsCount)
		}
	}
}
