package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/report"
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
	if metrics.ConfirmedHoldPrecisionPct != nil {
		t.Errorf("ConfirmedHoldPrecisionPct = %v, want nil", *metrics.ConfirmedHoldPrecisionPct)
	}
	if metrics.WeakSignalHoldPrecisionPct != nil {
		t.Errorf("WeakSignalHoldPrecisionPct = %v, want nil", *metrics.WeakSignalHoldPrecisionPct)
	}
	if metrics.ConfirmedLabelCoveragePct == nil || *metrics.ConfirmedLabelCoveragePct != 0 {
		t.Errorf("ConfirmedLabelCoveragePct = %v, want 0", metrics.ConfirmedLabelCoveragePct)
	}
	if metrics.UnlabeledRatePct == nil || *metrics.UnlabeledRatePct != 100 {
		t.Errorf("UnlabeledRatePct = %v, want 100", metrics.UnlabeledRatePct)
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

func TestReplayExpectedWithoutLabelIsEffectivelyUnlabeled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "expected.json")
	if err := os.WriteFile(path, []byte(`{"recommendation":"PROCEED"}`), 0644); err != nil {
		t.Fatalf("write expected.json: %v", err)
	}
	expected, err := readReplayExpected(path)
	if err != nil {
		t.Fatalf("readReplayExpected() error = %v", err)
	}
	outcome, err := replayEffectiveOutcome(expected)
	if err != nil {
		t.Fatalf("replayEffectiveOutcome() error = %v", err)
	}
	if outcome != report.HumanOutcomeUnlabeled {
		t.Errorf("outcome = %q, want %q", outcome, report.HumanOutcomeUnlabeled)
	}
}

func TestRunReplayCasesHumanLabelMetrics(t *testing.T) {
	t.Chdir("../..")

	dir := t.TempDir()
	tests := []struct {
		name  string
		label *report.Label
	}{
		{
			name: "01-explicit-correct",
			label: &report.Label{
				HumanOutcome:   report.HumanOutcomeCorrect,
				EvidenceSource: report.EvidenceSourceReleaseDecision,
				Derivation:     report.DerivationExplicit,
				Confidence:     report.ConfidenceHigh,
			},
		},
		{
			name: "02-inferred-overcautious",
			label: &report.Label{
				HumanOutcome:   report.HumanOutcomeOvercautious,
				EvidenceSource: report.EvidenceSourceReviewerComment,
				Derivation:     report.DerivationInferred,
				Confidence:     report.ConfidenceMedium,
			},
		},
		{name: "03-unlabeled"},
	}
	for _, tt := range tests {
		caseDir := filepath.Join(dir, tt.name)
		if err := os.MkdirAll(caseDir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", tt.name, err)
		}
		copyFile(t, "testdata/replay/03-hold/diff.json", filepath.Join(caseDir, "diff.json"))
		expected := replayExpected{Recommendation: "HOLD", Label: tt.label}
		data, err := json.Marshal(expected)
		if err != nil {
			t.Fatalf("marshal expected for %s: %v", tt.name, err)
		}
		if err := os.WriteFile(filepath.Join(caseDir, "expected.json"), data, 0644); err != nil {
			t.Fatalf("write expected for %s: %v", tt.name, err)
		}
	}

	metrics, err := runReplayCases(dir, 60)
	if err != nil {
		t.Fatalf("runReplayCases() error = %v", err)
	}
	if metrics.ConfirmedHoldPrecisionPct == nil || *metrics.ConfirmedHoldPrecisionPct != 100 {
		t.Errorf("ConfirmedHoldPrecisionPct = %v, want 100", metrics.ConfirmedHoldPrecisionPct)
	}
	if metrics.WeakSignalHoldPrecisionPct == nil || *metrics.WeakSignalHoldPrecisionPct != 50 {
		t.Errorf("WeakSignalHoldPrecisionPct = %v, want 50", metrics.WeakSignalHoldPrecisionPct)
	}
}

func TestRunReplayCasesRejectsInvalidLabel(t *testing.T) {
	dir := t.TempDir()
	caseName := "invalid-label-case"
	caseDir := filepath.Join(dir, caseName)
	if err := os.MkdirAll(caseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "expected.json"), []byte(`{
		"recommendation":"HOLD",
		"label":{"human_outcome":"unknown","evidence_source":"release_decision","derivation":"explicit","confidence":"high"}
	}`), 0644); err != nil {
		t.Fatalf("write expected.json: %v", err)
	}

	_, err := runReplayCases(dir, 60)
	if err == nil {
		t.Fatal("runReplayCases() error = nil, want invalid label error")
	}
	if !strings.Contains(err.Error(), `case "invalid-label-case"`) {
		t.Errorf("error = %q, want case directory name", err)
	}
}

func TestRunReplayCasesRejectsUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	caseName := "unknown-key-case"
	caseDir := filepath.Join(dir, caseName)
	if err := os.MkdirAll(caseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "expected.json"), []byte(
		`{"expected":"HOLD","label":{"human_outcome":"correct","evidence_source":"reviewer_comment","derivation":"explicit","confidence":"high"}}`,
	), 0644); err != nil {
		t.Fatalf("write expected.json: %v", err)
	}

	_, err := runReplayCases(dir, 60)
	if err == nil {
		t.Fatal("runReplayCases() error = nil, want unknown-field error")
	}
	if !strings.Contains(err.Error(), `case "unknown-key-case"`) {
		t.Errorf("error = %q, want case directory name", err)
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Errorf("error = %q, want offending key %q", err, "expected")
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

func TestRunReplayCasesTreatsEmptyRecommendationAsUnlabeled(t *testing.T) {
	t.Chdir("../..")

	dir := t.TempDir()

	normalDir := filepath.Join(dir, "01-normal")
	if err := os.MkdirAll(normalDir, 0755); err != nil {
		t.Fatalf("mkdir 01-normal: %v", err)
	}
	copyFile(t, "testdata/replay/03-hold/diff.json", filepath.Join(normalDir, "diff.json"))
	normalExpected := replayExpected{
		Recommendation: "HOLD",
		Label: &report.Label{
			HumanOutcome:   report.HumanOutcomeCorrect,
			EvidenceSource: report.EvidenceSourceReleaseDecision,
			Derivation:     report.DerivationExplicit,
			Confidence:     report.ConfidenceHigh,
		},
	}
	normalData, err := json.Marshal(normalExpected)
	if err != nil {
		t.Fatalf("marshal expected for 01-normal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(normalDir, "expected.json"), normalData, 0644); err != nil {
		t.Fatalf("write expected for 01-normal: %v", err)
	}

	githubDir := filepath.Join(dir, "02-github-unlabeled")
	if err := os.MkdirAll(githubDir, 0755); err != nil {
		t.Fatalf("mkdir 02-github-unlabeled: %v", err)
	}
	copyFile(t, "testdata/replay/03-hold/diff.json", filepath.Join(githubDir, "diff.json"))
	if err := os.WriteFile(filepath.Join(githubDir, "expected.json"), []byte(
		`{"recommendation":"","source":"github:o/r:1","label":{"human_outcome":"unlabeled"}}`,
	), 0644); err != nil {
		t.Fatalf("write expected for 02-github-unlabeled: %v", err)
	}

	metrics, err := runReplayCases(dir, 60)
	if err != nil {
		t.Fatalf("runReplayCases() error = %v", err)
	}
	if metrics.Cases != 1 {
		t.Errorf("Cases = %d, want 1", metrics.Cases)
	}
	if metrics.Unlabeled != 1 {
		t.Errorf("Unlabeled = %d, want 1", metrics.Unlabeled)
	}
	if metrics.ExactMatch != 1 {
		t.Errorf("ExactMatch = %d, want 1", metrics.ExactMatch)
	}
	if metrics.UnlabeledRatePct == nil || *metrics.UnlabeledRatePct != 50 {
		t.Errorf("UnlabeledRatePct = %v, want 50", metrics.UnlabeledRatePct)
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
