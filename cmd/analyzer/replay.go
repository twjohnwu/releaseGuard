package main

import (
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
	Cases            int                `json:"cases"`
	ExactMatch       int                `json:"exact_match"`
	ExactMatchPct    float64            `json:"exact_match_pct"`
	HoldPrecisionPct *float64           `json:"hold_precision_pct"`
	FalsePositivePct *float64           `json:"false_positive_pct"`
	PerCase          []replayCaseResult `json:"per_case"`
}

type replayExpected struct {
	Recommendation string `json:"recommendation"`
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
		Cases:   len(caseNames),
		PerCase: make([]replayCaseResult, 0, len(caseNames)),
	}
	predictedHolds := 0
	correctHolds := 0
	expectedProceeds := 0
	falsePositives := 0

	for i, caseName := range caseNames {
		caseDir := filepath.Join(dataset, caseName)
		diffFiles, err := readReplayDiff(filepath.Join(caseDir, "diff.json"))
		if err != nil {
			return replayMetrics{}, fmt.Errorf("case %q: %w", caseName, err)
		}
		expected, err := readReplayExpected(filepath.Join(caseDir, "expected.json"))
		if err != nil {
			return replayMetrics{}, fmt.Errorf("case %q: %w", caseName, err)
		}

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

		if result.Match {
			metrics.ExactMatch++
		}
		if actual == "HOLD" {
			predictedHolds++
			if expected.Recommendation == "HOLD" {
				correctHolds++
			}
		}
		if expected.Recommendation == "PROCEED" {
			expectedProceeds++
			if actual == "HOLD" || actual == "REVIEW" {
				falsePositives++
			}
		}
	}

	if metrics.Cases > 0 {
		metrics.ExactMatchPct = 100 * float64(metrics.ExactMatch) / float64(metrics.Cases)
	}
	if predictedHolds > 0 {
		v := 100 * float64(correctHolds) / float64(predictedHolds)
		metrics.HoldPrecisionPct = &v
	}
	if expectedProceeds > 0 {
		v := 100 * float64(falsePositives) / float64(expectedProceeds)
		metrics.FalsePositivePct = &v
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
	if err := json.Unmarshal(data, &expected); err != nil {
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
	fmt.Printf("exact match:         %d/%d (%.1f%%)\n", metrics.ExactMatch, metrics.Cases, metrics.ExactMatchPct)
	fmt.Printf("HOLD precision:      %s\n", replayPct(metrics.HoldPrecisionPct))
	fmt.Printf("false-positive rate: %s\n", replayPct(metrics.FalsePositivePct))
}

func replayPct(value *float64) string {
	if value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", *value)
}
