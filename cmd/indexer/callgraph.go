package main

import (
	"context"

	"github.com/acme/releaseguard/internal/indexer/code/callgraph"
	"github.com/acme/releaseguard/internal/storage"
)

func stepCallgraph(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	b := callgraph.NewGoBuilder()
	r, err := b.Build(ctx, repoPath)
	if err != nil {
		return err
	}
	return callgraph.UpsertResult(ctx, pool, repoID, r)
}
