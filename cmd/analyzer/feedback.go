package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/twjohnwu/releaseGuard/internal/config"
	"github.com/twjohnwu/releaseGuard/internal/gitlab"
	"github.com/twjohnwu/releaseGuard/internal/report"
)

// decisionMarker is the stable substring the renderer emits in the MR comment
// heading (see internal/report/renderer.go Render). We recover the decision by
// scanning notes for this marker.
const decisionMarker = "ReleaseGuard recommendation:"

// mrDecision is one merged MR's ReleaseGuard outcome, as reconstructed from its
// comment + labels.
type mrDecision struct {
	IID           int    `json:"iid"`
	Title         string `json:"title"`
	Decision      string `json:"decision"` // HOLD | REVIEW | PROCEED | "" (none found)
	FalsePositive bool   `json:"false_positive"`
}

// precisionReport is the MVP output: how many HOLDs fired and how many were
// flagged as false positives.
type precisionReport struct {
	MergedScanned      int          `json:"merged_scanned"`
	HoldCount          int          `json:"hold_count"`
	HoldFalsePositives int          `json:"hold_false_positives"`
	PrecisionPct       float64      `json:"precision_pct"` // 100 * (hold - fp) / hold; -1 when no HOLDs
	Decisions          []mrDecision `json:"decisions"`
}

// parseDecision extracts the decision token (HOLD/REVIEW/PROCEED) from a set of
// MR note bodies. Returns "" when no ReleaseGuard comment is present.
func parseDecision(notes []string) string {
	for _, body := range notes {
		idx := strings.Index(body, decisionMarker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(body[idx+len(decisionMarker):])
		for _, tok := range []string{"HOLD", "REVIEW", "PROCEED"} {
			if strings.HasPrefix(rest, tok) {
				return tok
			}
		}
	}
	return ""
}

// computePrecision tallies HOLD decisions vs false-positive labels.
func computePrecision(decisions []mrDecision) precisionReport {
	rep := precisionReport{MergedScanned: len(decisions), Decisions: decisions, PrecisionPct: -1}
	for _, d := range decisions {
		if d.Decision == "HOLD" {
			rep.HoldCount++
			if d.FalsePositive {
				rep.HoldFalsePositives++
			}
		}
	}
	if rep.HoldCount > 0 {
		rep.PrecisionPct = 100 * float64(rep.HoldCount-rep.HoldFalsePositives) / float64(rep.HoldCount)
	}
	return rep
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func runFeedback(args []string) error {
	fs := flag.NewFlagSet("feedback", flag.ContinueOnError)
	project := fs.Int("project", 0, "GitLab project ID (defaults to CI_PROJECT_ID)")
	since := fs.String("since", "", "only scan MRs updated after this RFC3339 timestamp")
	perPage := fs.Int("per-page", 20, "MRs per API page")
	maxPages := fs.Int("max-pages", 5, "maximum API pages to scan")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	projectID := *project
	if projectID == 0 {
		projectID = cfg.CIProjectID
	}
	if projectID == 0 {
		return fmt.Errorf("project id required: pass --project or set CI_PROJECT_ID")
	}

	gl := gitlab.NewClient(cfg.GitLabAPIBase, cfg.GitLabToken)
	mrs, err := gl.ListMergedMRs(projectID, *since, *perPage, *maxPages)
	if err != nil {
		return err
	}

	decisions := make([]mrDecision, 0, len(mrs))
	for _, mr := range mrs {
		notes, err := gl.GetMRNotes(projectID, mr.IID)
		if err != nil {
			return err
		}
		decisions = append(decisions, mrDecision{
			IID:           mr.IID,
			Title:         mr.Title,
			Decision:      parseDecision(notes),
			FalsePositive: hasLabel(mr.Labels, report.FalsePositiveLabel),
		})
	}

	rep := computePrecision(decisions)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	printPrecisionTable(rep)
	return nil
}

func printPrecisionTable(rep precisionReport) {
	fmt.Printf("ReleaseGuard feedback — precision report\n")
	fmt.Printf("  merged MRs scanned:      %d\n", rep.MergedScanned)
	fmt.Printf("  HOLD decisions:          %d\n", rep.HoldCount)
	fmt.Printf("  HOLD false-positives:    %d\n", rep.HoldFalsePositives)
	if rep.PrecisionPct < 0 {
		fmt.Printf("  HOLD precision:          n/a (no HOLDs)\n")
	} else {
		fmt.Printf("  HOLD precision:          %.1f%%\n", rep.PrecisionPct)
	}
}
