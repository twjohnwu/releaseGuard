package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSubcommandsListed(t *testing.T) {
	out, err := exec.Command("go", "run", "./", "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("run help: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "nightly") {
		t.Fatalf("nightly not listed: %s", s)
	}
	if !strings.Contains(s, "backfill") {
		t.Fatalf("backfill not listed: %s", s)
	}
}
