package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/acme/releaseguard/internal/agents/ownership"
	"github.com/acme/releaseguard/internal/agents/reviewer"
	"github.com/acme/releaseguard/internal/agents/rollout"
	"github.com/acme/releaseguard/internal/agents/testselect"
	"github.com/acme/releaseguard/internal/ai"
	"github.com/acme/releaseguard/internal/analysis/configdrift"
	"github.com/acme/releaseguard/internal/analysis/specdrift"
	"github.com/acme/releaseguard/internal/config"
	"github.com/acme/releaseguard/internal/gitlab"
	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/logger"
	"github.com/acme/releaseguard/internal/result"
	"github.com/acme/releaseguard/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	mode, err := validateTopology(cfg)
	if err != nil {
		return err
	}
	log := logger.New()
	log.Info("analyzer starting", "topology", mode)

	if cfg.CIProjectID == 0 || cfg.CIMergeRequestIID == 0 {
		return fmt.Errorf("CI_PROJECT_ID and CI_MERGE_REQUEST_IID required")
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(cfg.AnalyzeTimeoutSec)*time.Second)
	defer cancel()

	var pool *storage.Pool
	var repoID int64
	if cfg.PostgresURL != "" {
		pool, err = storage.NewPool(ctx, cfg.PostgresURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		if cfg.TargetServiceName != "" {
			if err := pool.QueryRow(ctx, "SELECT id FROM repos WHERE name=$1", cfg.TargetServiceName).Scan(&repoID); err != nil {
				log.Warn("repo not registered, db-backed agents will use repo_id=0", "repo", cfg.TargetServiceName, "err", err)
			}
		}
	}

	prov := ai.NewAnthropic(cfg.AIProviderKey, "claude-3-5-sonnet-20241022")
	gl := gitlab.NewClient(cfg.GitLabAPIBase, cfg.GitLabToken)

	diff, err := gl.GetMRDiff(cfg.CIProjectID, cfg.CIMergeRequestIID)
	if err != nil {
		return err
	}
	difFiles := make([]interfaces.DiffFile, 0, len(diff))
	for _, d := range diff {
		difFiles = append(difFiles, interfaces.DiffFile{
			Path:    d.NewPath,
			OldPath: d.OldPath,
			Status:  diffStatus(d),
			Patch:   d.Diff,
		})
	}
	in := interfaces.AgentInput{
		MRIID:     cfg.CIMergeRequestIID,
		Diff:      difFiles,
		CommitSHA: cfg.CommitSHA,
		Config:    interfaces.AgentConfig{Topology: mode, ProjectDir: cfg.ProjectsDir},
	}

	agents := buildAgents(cfg.Agents, buildAgentsDeps{
		prov:         prov,
		projectsDir:  cfg.ProjectsDir,
		systemName:   cfg.TargetServiceName,
		serviceTypes: cfg.TargetServiceTypes,
		rolloutZones: collectRolloutZones(difFiles),
		pool:         pool,
		repoID:       repoID,
	})

	outs := runAgentsParallel(ctx, agents, in)

	flags := result.Flags{
		SelectiveTest: cfg.Agents.SelectiveTest,
		RolloutRisk:   cfg.Agents.RolloutRisk,
		Ownership:     cfg.Agents.Ownership,
		AIReviewer:    cfg.Agents.AIReviewer,
	}
	md := result.Compose(outs, flags)

	requiredTests, confidence := selectiveTestSummary(outs)
	writeArtifact := flags.SelectiveTest && confidence >= cfg.SelectiveTestMinConfidence

	poster := result.NewPoster(gl, cfg.CIProjectID, cfg.CIMergeRequestIID, cfg.SelectiveTestMinConfidence)
	if err := poster.Post(ctx, md, requiredTests, writeArtifact); err != nil {
		log.Error("post failed", "err", err)
		return err
	}
	log.Info("analyzer done")
	return nil
}

type buildAgentsDeps struct {
	prov         ai.Provider
	projectsDir  string
	systemName   string
	serviceTypes []string
	rolloutZones []rollout.Zone
	pool         *storage.Pool
	repoID       int64
}

func collectRolloutZones(diff []interfaces.DiffFile) []rollout.Zone {
	var zones []rollout.Zone
	zones = append(zones, configdrift.FromDiff(diff)...)
	specPaths := []string{"api/openapi.yaml", "api/openapi.yml"}
	for _, f := range specdrift.Detect(diff, specPaths) {
		zones = append(zones, rollout.Zone{
			Type:     "spec_code_drift",
			Detail:   f.Title,
			Severity: string(f.Severity),
		})
	}
	return zones
}

func buildAgents(flags config.AgentFlags, deps buildAgentsDeps) []interfaces.IAgent {
	hasDB := deps.pool != nil
	var agents []interfaces.IAgent
	if flags.SelectiveTest {
		if hasDB {
			agents = append(agents, testselect.NewWithDB(deps.pool, deps.repoID))
		} else {
			agents = append(agents, testselect.New(false))
		}
	}
	if flags.RolloutRisk {
		agents = append(agents, rollout.New(rollout.Deps{Zones: deps.rolloutZones}))
	}
	if flags.Ownership && hasDB {
		agents = append(agents, ownership.New(deps.pool, deps.repoID))
	}
	if flags.AIReviewer {
		agents = append(agents, reviewer.New(deps.prov, deps.projectsDir, deps.systemName, deps.serviceTypes))
	}
	return agents
}

func runAgentsParallel(ctx context.Context, agents []interfaces.IAgent, in interfaces.AgentInput) []interfaces.AgentOutput {
	var wg sync.WaitGroup
	out := make([]interfaces.AgentOutput, len(agents))
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a interfaces.IAgent) {
			defer wg.Done()
			o, err := a.Run(ctx, in)
			if err != nil {
				out[i] = interfaces.AgentOutput{Agent: a.Name(), Status: interfaces.StatusFailed,
					SchemaVersion: "1", Summary: err.Error()}
				return
			}
			out[i] = o
		}(i, a)
	}
	wg.Wait()
	return out
}

// selectiveTestSummary pulls the required[] / confidence metadata from the
// SelectiveTest agent's output (if present). Returns zero values when absent.
func selectiveTestSummary(outs []interfaces.AgentOutput) (required []string, confidence float64) {
	required = []string{}
	for _, o := range outs {
		if o.Agent != interfaces.AgentSelectiveTest {
			continue
		}
		if v, ok := o.Metadata["required"].([]string); ok {
			required = v
		}
		if v, ok := o.Metadata["confidence"].(float64); ok {
			confidence = v
		}
		break
	}
	return
}

func diffStatus(d gitlab.DiffFile) string {
	switch {
	case d.NewFile:
		return "added"
	case d.DeletedFile:
		return "deleted"
	case d.RenamedFile:
		return "renamed"
	default:
		return "modified"
	}
}
