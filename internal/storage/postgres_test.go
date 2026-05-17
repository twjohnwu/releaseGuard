package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPoolConnectAndPing(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestNewPoolEmptyURL(t *testing.T) {
	_, err := NewPool(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for empty url")
	}
}
