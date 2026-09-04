package main

import (
	"context"
	"errors"
	"os"

	"github.com/twjohnwu/releaseGuard/internal/indexer/code/coverage"
	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func stepCoverage(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	path := os.Getenv("COVERAGE_ARTIFACT_PATH")
	if path == "" {
		return nil // skip if not configured
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	p := coverage.NewLCOV()
	entries, err := p.Parse(f)
	if err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	sha := os.Getenv("CI_COMMIT_SHA")
	if sha == "" {
		sha = "unknown"
	}
	return coverage.Load(ctx, pool, repoID, sha, entries)
}
