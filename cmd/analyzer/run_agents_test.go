package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
	"github.com/twjohnwu/releaseGuard/internal/logger"
)

// fakeAgent is a minimal interfaces.IAgent for exercising runAgentsParallel's
// panic isolation and per-agent timeout behavior.
type fakeAgent struct {
	name        interfaces.AgentName
	run         func(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error)
	doPanic     bool
	block       bool
	panicOnName bool
}

func (f fakeAgent) Name() interfaces.AgentName {
	if f.panicOnName {
		panic("name exploded")
	}
	return f.name
}

func (f fakeAgent) Run(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
	if f.doPanic {
		panic("agent exploded")
	}
	if f.block {
		<-ctx.Done()
		return interfaces.AgentOutput{}, ctx.Err()
	}
	if f.run != nil {
		return f.run(ctx, in)
	}
	return interfaces.AgentOutput{Agent: f.name, Status: interfaces.StatusOK, SchemaVersion: "1"}, nil
}

func TestRunAgentsParallel_PanicIsolatedFromOtherAgents(t *testing.T) {
	agents := []interfaces.IAgent{
		fakeAgent{name: interfaces.AgentRolloutRisk, doPanic: true},
		fakeAgent{name: interfaces.AgentOwnership},
	}
	log := logger.New()
	out := runAgentsParallel(context.Background(), log, time.Second, agents, interfaces.AgentInput{})

	if out[0].Status != interfaces.StatusFailed || !strings.Contains(out[0].Summary, "panic") {
		t.Fatalf("panicking agent slot: %+v", out[0])
	}
	if out[1].Status != interfaces.StatusOK {
		t.Fatalf("other agent slot should be intact: %+v", out[1])
	}
}

func TestRunAgentsParallel_SlowAgentTimesOutIndependently(t *testing.T) {
	agents := []interfaces.IAgent{
		fakeAgent{name: interfaces.AgentRolloutRisk, block: true},
	}
	log := logger.New()

	start := time.Now()
	out := runAgentsParallel(context.Background(), log, 50*time.Millisecond, agents, interfaces.AgentInput{})
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("runAgentsParallel took too long: %s", elapsed)
	}
	if out[0].Status != interfaces.StatusFailed || !strings.Contains(out[0].Summary, "timeout") {
		t.Fatalf("timed-out agent slot: %+v", out[0])
	}
}

func TestRunAgentsParallel_NamePanicDoesNotCrashProcess(t *testing.T) {
	// Name() panicking must not crash the process (recover already fired
	// once for the isolated name-capture); Run() itself still succeeds, so
	// the agent's own result stands, just tagged with the fallback name.
	agents := []interfaces.IAgent{
		fakeAgent{name: interfaces.AgentRolloutRisk, panicOnName: true},
	}
	log := logger.New()
	out := runAgentsParallel(context.Background(), log, time.Second, agents, interfaces.AgentInput{})

	if out[0].Status != interfaces.StatusOK {
		t.Fatalf("expected Run's own success to stand despite Name() panicking: %+v", out[0])
	}
}

func TestRunAgentsParallel_SuccessLogsDuration(t *testing.T) {
	agents := []interfaces.IAgent{
		fakeAgent{name: interfaces.AgentRolloutRisk, run: func(ctx context.Context, in interfaces.AgentInput) (interfaces.AgentOutput, error) {
			time.Sleep(2 * time.Millisecond)
			return interfaces.AgentOutput{Agent: interfaces.AgentRolloutRisk, Status: interfaces.StatusOK, SchemaVersion: "1"}, nil
		}},
	}
	log := logger.New()
	out := runAgentsParallel(context.Background(), log, time.Second, agents, interfaces.AgentInput{})

	if out[0].Status != interfaces.StatusOK {
		t.Fatalf("expected ok status: %+v", out[0])
	}
	if out[0].DurationMs <= 0 {
		t.Fatalf("expected DurationMs > 0: %+v", out[0])
	}
}
