package main

import (
	"testing"

	"github.com/acme/releaseguard/internal/config"
)

func TestValidateTopologyTopology0(t *testing.T) {
	c := &config.Config{
		PostgresURL: "postgres://x",
		Agents:      config.AgentFlags{SelectiveTest: true, RolloutRisk: true, Ownership: true, AIReviewer: true},
		RAGEnabled:  true,
	}
	mode, err := validateTopology(c)
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if mode != "0" {
		t.Fatalf("expected topology 0: %s", mode)
	}
}

func TestValidateTopologyTopology1(t *testing.T) {
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: false, RolloutRisk: true, Ownership: false, AIReviewer: true},
		RAGEnabled:  false,
	}
	mode, err := validateTopology(c)
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if mode != "1" {
		t.Fatalf("expected topology 1: %s", mode)
	}
}

func TestValidateTopologySelectiveL1WithoutDBOK(t *testing.T) {
	// Plan B: SelectiveTest L1 (PoC) does not require POSTGRES_URL.
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: true, RolloutRisk: true, Ownership: false, AIReviewer: true},
		RAGEnabled:  false,
	}
	mode, err := validateTopology(c)
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if mode != "1" {
		t.Fatalf("expected topology 1: %s", mode)
	}
}

func TestValidateTopologyInvalidOwnershipWithoutDB(t *testing.T) {
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: false, RolloutRisk: true, Ownership: true, AIReviewer: true},
		RAGEnabled:  false,
	}
	_, err := validateTopology(c)
	if err == nil {
		t.Fatalf("expected error: ownership requires postgres")
	}
}

func TestValidateTopologyInvalidRAGWithoutDB(t *testing.T) {
	c := &config.Config{
		PostgresURL: "",
		Agents:      config.AgentFlags{SelectiveTest: false, RolloutRisk: true, Ownership: false, AIReviewer: true},
		RAGEnabled:  true,
	}
	_, err := validateTopology(c)
	if err == nil {
		t.Fatalf("expected error: rag requires postgres")
	}
}
