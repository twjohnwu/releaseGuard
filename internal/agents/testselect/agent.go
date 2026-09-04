package testselect

import (
	"context"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
	"github.com/twjohnwu/releaseGuard/internal/storage"
)

type Agent struct {
	hasDB  bool
	pool   *storage.Pool
	repoID int64
}

func New(hasDB bool) *Agent { return &Agent{hasDB: hasDB} }

// NewWithDB constructs an agent that prefers L2/L3 when DB-backed data is available.
func NewWithDB(pool *storage.Pool, repoID int64) *Agent {
	return &Agent{hasDB: true, pool: pool, repoID: repoID}
}

func (a *Agent) Name() interfaces.AgentName { return interfaces.AgentSelectiveTest }

func (a *Agent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	start := time.Now()
	files := []string{}
	for _, f := range in.Diff {
		files = append(files, f.Path)
	}
	level := "L1"
	required, skippable, conf, reason := runL1(in.Diff)

	if a.hasDB && a.pool != nil {
		// L3 attempt: collect changed symbols from diff files. DB-backed levels
		// are best-effort: on any query error we skip them and keep the L1 result.
		var changedSymbols []string
		if rows, err := a.pool.Query(ctx,
			"SELECT id FROM symbols WHERE repo_id=$1 AND file = ANY($2)", a.repoID, files); err == nil {
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					changedSymbols = nil
					break
				}
				changedSymbols = append(changedSymbols, id)
			}
			if err := rows.Err(); err != nil {
				changedSymbols = nil
			}
			rows.Close()
		}
		if len(changedSymbols) > 0 {
			if l3req, l3conf, err := L3QueryRequired(ctx, a.pool, a.repoID, changedSymbols); err == nil && len(l3req) > 0 && l3conf >= L3AcceptThreshold {
				required = l3req
				level = "L3"
				conf = l3conf
				reason = "L3: reverse BFS on edges"
			}
		}

		// L2 fallback if L3 didn't produce results
		if level == "L1" {
			if l2req, err := L2QueryRequired(ctx, a.pool, a.repoID, files); err == nil && len(l2req) > 0 {
				required = l2req
				level = "L2"
				conf = L2Confidence
				reason = "L2: coverage_map intersection"
			}
		}
	}

	status := interfaces.StatusOK
	if conf < PartialStatusThreshold {
		status = interfaces.StatusPartial
	}
	return interfaces.AgentOutput{
		Agent:         interfaces.AgentSelectiveTest,
		Status:        status,
		DurationMs:    int(time.Since(start) / time.Millisecond),
		SchemaVersion: "1",
		Findings:      []interfaces.Finding{},
		Summary:       "selective test " + level,
		Metadata: map[string]any{
			"analysis_level": level,
			"confidence":     conf,
			"required":       required,
			"skippable":      skippable,
			"reason":         reason,
			"fallback_chain": []string{level},
		},
	}, nil
}
