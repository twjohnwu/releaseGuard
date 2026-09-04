package ownership

import (
	"context"
	"strings"

	"github.com/twjohnwu/releaseGuard/internal/storage"
)

type Zone struct {
	Path                     string
	RecentActiveAuthorsCount int
	LookbackDays             int
}

func computeZones(ctx context.Context, pool *storage.Pool, repoID int64, files []string) ([]Zone, error) {
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
			return nil, err
		}
		for rows.Next() {
			var a string
			if err := rows.Scan(&a); err != nil {
				rows.Close()
				return nil, err
			}
			zones[zone][a] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	out := []Zone{}
	for z, authors := range zones {
		out = append(out, Zone{Path: z, RecentActiveAuthorsCount: len(authors), LookbackDays: 90})
	}
	return out, nil
}

func topLevel(path string) string {
	idx := strings.IndexByte(path, '/')
	if idx < 0 {
		return path
	}
	return path[:idx+1]
}
