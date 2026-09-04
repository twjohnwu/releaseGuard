package ownership

import (
	"context"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
	"github.com/twjohnwu/releaseGuard/internal/storage"
)

type Agent struct {
	pool   *storage.Pool
	repoID int64
}

func New(pool *storage.Pool, repoID int64) *Agent {
	return &Agent{pool: pool, repoID: repoID}
}

func (a *Agent) Name() interfaces.AgentName { return interfaces.AgentOwnership }

type SuggestedReviewer struct {
	Name    string `json:"name"`
	Context string `json:"context"`
}

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	if a.pool == nil {
		return interfaces.AgentOutput{
			Agent: interfaces.AgentOwnership, Status: interfaces.StatusFailed,
			DurationMs:    int(time.Since(start) / time.Millisecond),
			SchemaVersion: "1", Summary: "no Postgres",
		}, nil
	}
	files := []string{}
	for _, f := range in.Diff {
		files = append(files, f.Path)
	}

	candidates, err := buildCandidates(ctx, a.pool, a.repoID, files)
	if err != nil {
		return a.failed(start, "ownership query failed: "+err.Error()), nil
	}
	top := selectAndShuffle(candidates, 3, time.Now().UnixNano())

	suggested := []SuggestedReviewer{}
	for _, c := range top {
		suggested = append(suggested, SuggestedReviewer{
			Name: c.Author, Context: phraseContext(c),
		})
	}
	zones, err := computeZones(ctx, a.pool, a.repoID, files)
	if err != nil {
		return a.failed(start, "ownership zones query failed: "+err.Error()), nil
	}
	hints := []Hint{} // matrix wiring deferred; PoC keeps empty

	return interfaces.AgentOutput{
		Agent: interfaces.AgentOwnership, Status: interfaces.StatusOK,
		DurationMs:    int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1", Findings: []interfaces.Finding{},
		Summary: "ownership: impact + suggestions",
		Metadata: map[string]any{
			"suggested_reviewers":     suggested,
			"impact_zones":            zones,
			"hidden_dependency_hints": hints,
		},
	}, nil
}

// failed builds a degraded output for when a DB read errors out, mirroring the
// "no Postgres" early return so a partial/incorrect result is never emitted.
func (a *Agent) failed(start time.Time, summary string) interfaces.AgentOutput {
	return interfaces.AgentOutput{
		Agent: interfaces.AgentOwnership, Status: interfaces.StatusFailed,
		DurationMs:    int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1", Summary: summary,
	}
}

func buildCandidates(ctx context.Context, pool *storage.Pool, repoID int64, files []string) ([]candidate, error) {
	var out []candidate
	for _, f := range files {
		rows, err := pool.Query(ctx,
			"SELECT author, blame_weight, recency_score FROM ownership_signals WHERE repo_id=$1 AND file_path=$2",
			repoID, f)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var author string
			var weight, recency float64
			if err := rows.Scan(&author, &weight, &recency); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, candidate{
				Author: author,
				Score:  weight * recency,
				Source: "blame",
				Reason: f,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
