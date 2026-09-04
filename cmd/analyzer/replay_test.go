package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunReplayCases(t *testing.T) {
	t.Chdir("../..")

	metrics, err := runReplayCases("testdata/replay", 60)
	if err != nil {
		t.Fatalf("runReplayCases() error = %v", err)
	}
	if metrics.Cases != 4 {
		t.Fatalf("Cases = %d, want 4", metrics.Cases)
	}
	if metrics.HoldPrecisionPct == nil {
		t.Fatal("HoldPrecisionPct is nil, want 100")
	}
	if *metrics.HoldPrecisionPct != 100 {
		t.Fatalf("HoldPrecisionPct = %v, want 100", *metrics.HoldPrecisionPct)
	}

	byName := make(map[string]replayCaseResult, len(metrics.PerCase))
	for _, result := range metrics.PerCase {
		byName[result.Case] = result
	}
	for _, name := range []string{"01-proceed", "02-review", "03-hold"} {
		result, ok := byName[name]
		if !ok {
			t.Errorf("case %q is missing", name)
			continue
		}
		if !result.Match || result.Actual != result.Expected {
			t.Errorf("case %q = expected %q, actual %q, match %t; want a match", name, result.Expected, result.Actual, result.Match)
		}
	}
	if _, ok := byName["04-t0demo"]; !ok {
		t.Error("case \"04-t0demo\" is missing")
	}
}

func TestRunReplayJSON(t *testing.T) {
	t.Chdir("../..")

	output, err := captureStdout(t, func() error {
		return runReplay([]string{"--dataset", "testdata/replay", "--json"})
	})
	if err != nil {
		t.Fatalf("runReplay() error = %v", err)
	}

	var metrics replayMetrics
	if err := json.Unmarshal(output, &metrics); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %q", err, output)
	}
	if metrics.Cases != 4 {
		t.Errorf("Cases = %d, want 4", metrics.Cases)
	}
}

func captureStdout(t *testing.T, run func() error) ([]byte, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdout")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	original := os.Stdout
	os.Stdout = file
	runErr := run()
	os.Stdout = original
	if err := file.Close(); err != nil {
		t.Fatalf("file.Close() error = %v", err)
	}
	output, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	return output, runErr
}
