package interfaces

import (
	"encoding/json"
	"testing"
)

func TestFindingMarshalRoundTrip(t *testing.T) {
	f := Finding{
		ID:       "ai-001",
		StableID: "ai_reviewer:logic_bug:foo.go:Title",
		Severity: SeverityHigh,
		Category: "logic_bug",
		Title:    "Missing defer rollback",
		Body:     "leak risk",
		Location: &Location{File: "foo.go", LineStart: 10, LineEnd: 20},
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Finding
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.StableID != f.StableID {
		t.Fatalf("stable_id mismatch: got %q want %q", back.StableID, f.StableID)
	}
	if back.Location == nil || back.Location.File != "foo.go" {
		t.Fatalf("location lost: %+v", back.Location)
	}
}

func TestAgentOutputSchemaVersionPinned(t *testing.T) {
	out := AgentOutput{
		Agent:         AgentSelectiveTest,
		Status:        StatusOK,
		DurationMs:    100,
		SchemaVersion: "1",
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(raw, `"schema_version":"1"`) {
		t.Fatalf("schema_version not v1: %s", raw)
	}
}

func contains(b []byte, s string) bool {
	return string(b) != "" && (len(b) >= len(s)) && (string(b[:]) != "" && (indexOf(b, s) >= 0))
}

func indexOf(haystack []byte, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return i
		}
	}
	return -1
}
