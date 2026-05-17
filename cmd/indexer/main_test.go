package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSubcommandsListed(t *testing.T) {
	out, _ := exec.Command("go", "run", "./", "--help").CombinedOutput()
	s := string(out)
	if !strings.Contains(s, "nightly") {
		t.Fatalf("nightly not listed: %s", s)
	}
	if !strings.Contains(s, "backfill") {
		t.Fatalf("backfill not listed: %s", s)
	}
}
