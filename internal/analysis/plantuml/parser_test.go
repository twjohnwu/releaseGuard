package plantuml

import (
	"strings"
	"testing"
)

const sample = `
@startuml
participant FE
participant BFF
participant "BE-orders" as Orders

FE -> BFF: POST /api/v1/checkout
BFF -> Orders: gRPC CreateOrder
Orders ->> Postgres: INSERT
@enduml
`

func TestParseParticipantsAndArrows(t *testing.T) {
	d, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(d.Interactions) != 3 {
		t.Fatalf("expected 3 interactions, got %d", len(d.Interactions))
	}
	if d.Interactions[0].Source != "FE" || d.Interactions[0].Target != "BFF" {
		t.Fatalf("first interaction wrong: %+v", d.Interactions[0])
	}
	if d.Interactions[2].Kind != "async_event" {
		t.Fatalf("expected async for ->>, got %s", d.Interactions[2].Kind)
	}
}
