package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/acme/releaseguard/internal/agents/rollout"
	"github.com/acme/releaseguard/internal/agents/testselect"
	"github.com/acme/releaseguard/internal/config"
	"github.com/acme/releaseguard/internal/interfaces"
	"github.com/acme/releaseguard/internal/storage"
)

func TestBuildAgentsRespectsFlags(t *testing.T) {
	flags := config.AgentFlags{SelectiveTest: true, RolloutRisk: true}
	agents := buildAgents(flags, buildAgentsDeps{})
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	for _, a := range agents {
		if _, ok := a.(*testselect.Agent); !ok {
			if _, ok := a.(*rollout.Agent); !ok {
				t.Fatalf("unexpected agent: %T", a)
			}
		}
	}
	_ = context.Background()
	_ = interfaces.AgentName("")
}

func TestBuildAgentsTopology0WithDB(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := storage.NewPool(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	deps := buildAgentsDeps{pool: pool, repoID: 1}
	flags := config.AgentFlags{SelectiveTest: true, RolloutRisk: true, Ownership: true, AIReviewer: true}
	agents := buildAgents(flags, deps)
	if len(agents) != 4 {
		t.Fatalf("expected 4 agents, got %d", len(agents))
	}
}
