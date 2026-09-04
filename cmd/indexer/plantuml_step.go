package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/twjohnwu/releaseGuard/internal/analysis/aliases"
	"github.com/twjohnwu/releaseGuard/internal/analysis/plantuml"
	"github.com/twjohnwu/releaseGuard/internal/storage"
)

func stepPlantUML(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	docs := os.Getenv("DOCS_REPO_NAMES")
	if docs == "" {
		return nil
	}
	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		configDir = "/config"
	}
	lookup, err := aliases.LoadFile(filepath.Join(configDir, "diagram_aliases.yaml"))
	if err != nil {
		return err
	}
	var puml []string
	if err := filepath.Walk(repoPath, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info != nil && !info.IsDir() &&
			(strings.HasSuffix(p, ".puml") || strings.HasSuffix(p, ".plantuml")) {
			puml = append(puml, p)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, p := range puml {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		d, err := plantuml.Parse(f)
		closeErr := f.Close()
		if err != nil {
			continue
		}
		if closeErr != nil {
			continue
		}
		for _, in := range d.Interactions {
			srcRepo, srcOK := lookup.Resolve(in.Source)
			dstRepo, dstOK := lookup.Resolve(in.Target)
			var srcID, dstID interface{}
			if srcOK {
				srcID = lookupRepoID(ctx, pool, srcRepo)
			}
			if dstOK {
				dstID = lookupRepoID(ctx, pool, dstRepo)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO cross_repo_edges(source_repo_id, target_repo_id, source_alias, target_alias, call_kind, source_diagram_path)
				VALUES($1,$2,$3,$4,$5,$6)`,
				srcID, dstID, in.Source, in.Target, in.Kind, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func lookupRepoID(ctx context.Context, pool *storage.Pool, name string) interface{} {
	var id int64
	if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name=$1", name).Scan(&id); err != nil {
		return nil
	}
	return id
}
