package interfaces

import (
	"context"
	"testing"
)

type fakeAgent struct{ name AgentName }

func (f *fakeAgent) Name() AgentName { return f.name }
func (f *fakeAgent) Run(ctx context.Context, in AgentInput) (AgentOutput, error) {
	return AgentOutput{Agent: f.name, Status: StatusOK, SchemaVersion: "1"}, nil
}

func TestIAgentSatisfiable(t *testing.T) {
	var a IAgent = &fakeAgent{name: AgentSelectiveTest}
	out, err := a.Run(context.Background(), AgentInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Agent != AgentSelectiveTest {
		t.Fatalf("agent name lost")
	}
}
