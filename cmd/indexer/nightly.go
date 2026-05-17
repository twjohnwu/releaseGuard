package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/acme/releaseguard/internal/storage"
)

func init() {
	subcommands["nightly"] = NightlyImpl
}

func NightlyImpl(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("nightly", flag.ContinueOnError)
	repo := fs.String("repo", "", "repo name (matches repos.name)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return fmt.Errorf("--repo required")
	}
	pgURL := os.Getenv("POSTGRES_URL")
	if pgURL == "" {
		return fmt.Errorf("POSTGRES_URL required")
	}
	pool, err := storage.NewPool(ctx, pgURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "/migrations"
	}
	if err := storage.MigrateUp(ctx, pool, migrationsDir); err != nil {
		return err
	}

	var repoID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name=$1", *repo).Scan(&repoID); err != nil {
		return fmt.Errorf("repo %s not registered: %w", *repo, err)
	}

	repoPath := os.Getenv("REPO_CHECKOUT_DIR")
	if repoPath == "" {
		repoPath = "/checkout/" + *repo
	}

	steps := []struct {
		name string
		fn   stepFn
	}{
		{"callgraph", stepCallgraph},
		{"coverage", stepCoverage},
		{"cochange", stepCochange},
		{"plantuml", stepPlantUML},
	}
	for _, s := range steps {
		if err := s.fn(ctx, pool, repoID, repoPath); err != nil {
			fmt.Fprintf(os.Stderr, "%s step: %v\n", s.name, err)
		}
	}
	return nil
}

type stepFn func(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error
