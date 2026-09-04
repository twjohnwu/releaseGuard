package main

import (
	"context"

	"github.com/twjohnwu/releaseGuard/internal/indexer/code/callgraph"
	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func stepCallgraph(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	b := callgraph.NewGoBuilder()
	r, err := b.Build(ctx, repoPath)
	if err != nil {
		return err
	}
	return callgraph.UpsertResult(ctx, pool, repoID, r)
}
