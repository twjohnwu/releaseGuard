package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMainSmokeTopologyCheck(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", "/tmp/rg-analyzer", "./").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %s", out)
	}
	defer os.Remove("/tmp/rg-analyzer")

	// Missing required env → exit 1, error message.
	cmd := exec.Command("/tmp/rg-analyzer")
	cmd.Env = []string{}
	got, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit 1; got success: %s", got)
	}
	if !strings.Contains(string(got), "AI_PROVIDER_KEY required") {
		t.Fatalf("unexpected error: %s", got)
	}
}
