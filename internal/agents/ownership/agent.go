package ownership

import (
	"context"
	"time"

	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/storage"
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

	candidates := buildCandidates(ctx, a.pool, a.repoID, files)
	top := selectAndShuffle(candidates, 3, time.Now().UnixNano())

	suggested := []SuggestedReviewer{}
	for _, c := range top {
		suggested = append(suggested, SuggestedReviewer{
			Name: c.Author, Context: phraseContext(c),
		})
	}
	zones := computeZones(ctx, a.pool, a.repoID, files)
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

func buildCandidates(ctx context.Context, pool *storage.Pool, repoID int64, files []string) []candidate {
	var out []candidate
	for _, f := range files {
		rows, err := pool.Query(ctx,
			"SELECT author, blame_weight, recency_score FROM ownership_signals WHERE repo_id=$1 AND file_path=$2",
			repoID, f)
		if err != nil {
			continue
		}
		for rows.Next() {
			var author string
			var weight, recency float64
			rows.Scan(&author, &weight, &recency)
			out = append(out, candidate{
				Author: author,
				Score:  weight * recency,
				Source: "blame",
				Reason: f,
			})
		}
		rows.Close()
	}
	return out
}
