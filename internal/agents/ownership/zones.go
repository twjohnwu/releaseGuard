package ownership

import (
	"context"
	"strings"

	"github.com/acme/releaseguard/internal/storage"
)

type Zone struct {
	Path                     string
	RecentActiveAuthorsCount int
	LookbackDays             int
}

func computeZones(ctx context.Context, pool *storage.Pool, repoID int64, files []string) []Zone {
	zones := map[string]map[string]bool{}
	for _, f := range files {
		zone := topLevel(f)
		if zones[zone] == nil {
			zones[zone] = map[string]bool{}
		}
		rows, err := pool.Query(ctx,
			"SELECT DISTINCT author FROM ownership_signals WHERE repo_id=$1 AND file_path LIKE $2",
			repoID, zone+"%")
		if err != nil {
			continue
		}
		for rows.Next() {
			var a string
			rows.Scan(&a)
			zones[zone][a] = true
		}
		rows.Close()
	}
	out := []Zone{}
	for z, authors := range zones {
		out = append(out, Zone{Path: z, RecentActiveAuthorsCount: len(authors), LookbackDays: 90})
	}
	return out
}

func topLevel(path string) string {
	idx := strings.IndexByte(path, '/')
	if idx < 0 {
		return path
	}
	return path[:idx+1]
}
