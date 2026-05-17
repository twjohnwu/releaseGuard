package reviewer

import (
	"strings"
	"testing"
)

func TestComposeUserPromptIncludesDiff(t *testing.T) {
	user := composeUserPrompt("commit msg", []string{"foo.go diff body"})
	if !strings.Contains(user, "commit msg") || !strings.Contains(user, "foo.go diff body") {
		t.Fatalf("missing pieces: %s", user)
	}
}

func TestToolSchemaExposesFindings(t *testing.T) {
	s := ReviewToolSchema()
	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("no properties")
	}
	if _, ok := props["findings"]; !ok {
		t.Fatalf("findings property missing")
	}
}
