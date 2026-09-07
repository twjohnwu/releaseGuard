package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/config"
	"github.com/twjohnwu/releaseGuard/internal/gitlab"
	"github.com/twjohnwu/releaseGuard/internal/interfaces"
	"github.com/twjohnwu/releaseGuard/internal/logger"
	"github.com/twjohnwu/releaseGuard/internal/report"
)

type replayCaseResult struct {
	Case     string `json:"case"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Match    bool   `json:"match"`
}

type replayMetrics struct {
	Cases                      int                `json:"cases"`
	Unlabeled                  int                `json:"unlabeled"`
	ExactMatch                 int                `json:"exact_match"`
	ExactMatchPct              float64            `json:"exact_match_pct"`
	ConfirmedHoldPrecisionPct  *float64           `json:"confirmed_hold_precision_pct"`
	WeakSignalHoldPrecisionPct *float64           `json:"weak_signal_hold_precision_pct"`
	ConfirmedLabelCoveragePct  *float64           `json:"confirmed_label_coverage_pct"`
	UnlabeledRatePct           *float64           `json:"unlabeled_rate_pct"`
	MissedRiskCount            int                `json:"missed_risk_count"`
	PerCase                    []replayCaseResult `json:"per_case"`
}

type replayExpected struct {
	Recommendation string        `json:"recommendation"`
	NeedsLabel     bool          `json:"needs_label,omitempty"`
	Source         string        `json:"source"`
	Label          *report.Label `json:"label,omitempty"`
}

func replayEffectiveOutcome(expected replayExpected) (outcome string, err error) {
	if expected.Label != nil {
		if err := expected.Label.Validate(); err != nil {
			return "", err
		}
		return expected.Label.HumanOutcome, nil
	}
	return report.HumanOutcomeUnlabeled, nil
}

func runReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	dataset := fs.String("dataset", "", "directory containing replay cases")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	agentTimeoutSec := fs.Int("agent-timeout-sec", 60, "per-agent timeout in seconds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*dataset) == "" {
		return fmt.Errorf("dataset required: pass --dataset <dir>")
	}

	metrics, err := runReplayCases(*dataset, *agentTimeoutSec)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(metrics)
	}
	printReplayTable(metrics)
	return nil
}

func runReplayCases(dataset string, agentTimeoutSec int) (replayMetrics, error) {
	entries, err := os.ReadDir(dataset)
	if err != nil {
		return replayMetrics{}, fmt.Errorf("read replay dataset %q: %w", dataset, err)
	}

	caseNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			caseNames = append(caseNames, entry.Name())
		}
	}
	sort.Strings(caseNames)

	metrics := replayMetrics{
		PerCase: make([]replayCaseResult, 0, len(caseNames)),
	}
	type replayRunCase struct {
		name        string
		actual      string
		expectedRec string
		outcome     string
		explicit    bool
		weakSignal  bool
	}
	runCases := make([]replayRunCase, 0, len(caseNames))

	for i, caseName := range caseNames {
		caseDir := filepath.Join(dataset, caseName)
		expected, err := readReplayExpected(filepath.Join(caseDir, "expected.json"))
		if err != nil {
			return replayMetrics{}, fmt.Errorf("case %q: %w", caseName, err)
		}
		if expected.NeedsLabel || expected.Recommendation == "" {
			metrics.Unlabeled++
			continue
		}
		outcome, err := replayEffectiveOutcome(expected)
		if err != nil {
			return replayMetrics{}, fmt.Errorf("case %q: %w", caseName, err)
		}

		diffFiles, err := readReplayDiff(filepath.Join(caseDir, "diff.json"))
		if err != nil {
			return replayMetrics{}, fmt.Errorf("case %q: %w", caseName, err)
		}
		metrics.Cases++

		difFiles := make([]interfaces.DiffFile, 0, len(diffFiles))
		for _, diff := range diffFiles {
			difFiles = append(difFiles, interfaces.DiffFile{
				Path:    diff.NewPath,
				OldPath: diff.OldPath,
				Status:  diffStatus(diff),
				Patch:   diff.Diff,
			})
		}
		in := interfaces.AgentInput{
			MRIID:     i + 1,
			Diff:      difFiles,
			CommitSHA: "replay",
			Config:    interfaces.AgentConfig{Topology: "0"},
		}
		flags := config.AgentFlags{SelectiveTest: true, RolloutRisk: true}
		agents := buildAgents(flags, buildAgentsDeps{
			rolloutZones: collectRolloutZones(difFiles),
		})
		outs := runAgentsParallel(
			context.Background(),
			logger.New(),
			time.Duration(agentTimeoutSec)*time.Second,
			agents,
			in,
		)
		actual := report.Arbitrate(outs).Recommendation
		result := replayCaseResult{
			Case:     caseName,
			Expected: expected.Recommendation,
			Actual:   actual,
			Match:    actual == expected.Recommendation,
		}
		metrics.PerCase = append(metrics.PerCase, result)
		runCases = append(runCases, replayRunCase{
			name:        caseName,
			actual:      actual,
			expectedRec: expected.Recommendation,
			outcome:     outcome,
			explicit:    expected.Label != nil && expected.Label.IsExplicit(),
			weakSignal:  expected.Label != nil && expected.Label.HumanOutcome != report.HumanOutcomeUnlabeled,
		})

		if result.Match {
			metrics.ExactMatch++
		}
	}

	if metrics.Cases > 0 {
		metrics.ExactMatchPct = 100 * float64(metrics.ExactMatch) / float64(metrics.Cases)
	}

	allCases := metrics.Cases + metrics.Unlabeled
	effectivelyUnlabeledRan := 0
	explicitCount := 0
	confirmedCorrect := 0
	confirmedOvercautious := 0
	weakCorrect := 0
	weakOvercautious := 0
	for _, runCase := range runCases {
		if runCase.outcome == report.HumanOutcomeUnlabeled {
			effectivelyUnlabeledRan++
		}
		if runCase.explicit {
			explicitCount++
		}
		if runCase.outcome == report.HumanOutcomeMissedRisk {
			metrics.MissedRiskCount++
		}
		if runCase.actual != "HOLD" {
			continue
		}
		if runCase.explicit {
			switch runCase.outcome {
			case report.HumanOutcomeCorrect:
				confirmedCorrect++
			case report.HumanOutcomeOvercautious:
				confirmedOvercautious++
			}
		}
		if runCase.weakSignal {
			switch runCase.outcome {
			case report.HumanOutcomeCorrect:
				weakCorrect++
			case report.HumanOutcomeOvercautious:
				weakOvercautious++
			}
		}
	}
	if allCases > 0 {
		unlabeledRate := 100 * float64(effectivelyUnlabeledRan+metrics.Unlabeled) / float64(allCases)
		metrics.UnlabeledRatePct = &unlabeledRate
		confirmedCoverage := 100 * float64(explicitCount) / float64(allCases)
		metrics.ConfirmedLabelCoveragePct = &confirmedCoverage
	}
	if denominator := confirmedCorrect + confirmedOvercautious; denominator > 0 {
		v := 100 * float64(confirmedCorrect) / float64(denominator)
		metrics.ConfirmedHoldPrecisionPct = &v
	}
	if denominator := weakCorrect + weakOvercautious; denominator > 0 {
		v := 100 * float64(weakCorrect) / float64(denominator)
		metrics.WeakSignalHoldPrecisionPct = &v
	}
	return metrics, nil
}

func readReplayDiff(path string) ([]gitlab.DiffFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read diff: %w", err)
	}
	var diff []gitlab.DiffFile
	if err := json.Unmarshal(data, &diff); err != nil {
		return nil, fmt.Errorf("unmarshal diff: %w", err)
	}
	return diff, nil
}

func readReplayExpected(path string) (replayExpected, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return replayExpected{}, fmt.Errorf("read expected recommendation: %w", err)
	}
	var expected replayExpected
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&expected); err != nil {
		return replayExpected{}, fmt.Errorf("unmarshal expected recommendation: %w", err)
	}
	return expected, nil
}

func printReplayTable(metrics replayMetrics) {
	fmt.Println("ReleaseGuard replay — dataset report")
	fmt.Printf("%-20s  %-8s  %-8s  %s\n", "case", "expected", "actual", "match")
	for _, result := range metrics.PerCase {
		fmt.Printf("%-20s  %-8s  %-8s  %t\n", result.Case, result.Expected, result.Actual, result.Match)
	}
	fmt.Printf("unlabeled:                    %d\n", metrics.Unlabeled)
	fmt.Printf("exact match:                  %d/%d (%.1f%%)\n", metrics.ExactMatch, metrics.Cases, metrics.ExactMatchPct)
	fmt.Printf("confirmed HOLD precision:     %s\n", replayPct(metrics.ConfirmedHoldPrecisionPct))
	fmt.Printf("weak-signal HOLD precision:   %s\n", replayPct(metrics.WeakSignalHoldPrecisionPct))
	fmt.Printf("confirmed label coverage:     %s\n", replayPct(metrics.ConfirmedLabelCoveragePct))
	fmt.Printf("unlabeled rate:               %s\n", replayPct(metrics.UnlabeledRatePct))
	fmt.Printf("missed risk count:            %d\n", metrics.MissedRiskCount)
}

func replayPct(value *float64) string {
	if value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", *value)
}
