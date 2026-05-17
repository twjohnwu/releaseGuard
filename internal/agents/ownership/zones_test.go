package ownership

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/storage"
)

func TestZonesGroupsByTopLevelDir(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _ := storage.NewPool(ctx, url)
	defer pool.Close()
	storage.MigrateUp(ctx, pool, "../../../migrations")

	pool.Exec(ctx, "INSERT INTO repos(name) VALUES('zones-test') ON CONFLICT DO NOTHING")
	var rid int64
	pool.QueryRow(ctx, "SELECT id FROM repos WHERE name='zones-test'").Scan(&rid)
	for _, a := range []string{"a@x.com", "b@x.com", "c@x.com"} {
		pool.Exec(ctx, `INSERT INTO ownership_signals(repo_id, file_path, author, blame_weight, recency_score)
			VALUES($1, 'orders/handler.go', $2, 0.3, 0.9) ON CONFLICT DO NOTHING`, rid, a)
	}
	zs := computeZones(ctx, pool, rid, []string{"orders/handler.go", "payments/svc.go"})
	if len(zs) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(zs))
	}
	for _, z := range zs {
		if z.Path == "orders/" && z.RecentActiveAuthorsCount != 3 {
			t.Errorf("orders/: expected 3 authors, got %d", z.RecentActiveAuthorsCount)
		}
	}
}
