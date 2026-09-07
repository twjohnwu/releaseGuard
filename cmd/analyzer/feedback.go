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
	Confirmed     bool   `json:"confirmed"`
	FalsePositive bool   `json:"false_positive"`
	Outcome       string `json:"outcome"` // confirmed | false_positive | conflict | unlabeled
}

// Outcome values: the per-decision label classification. Only an explicit
// releaseguard:confirmed or releaseguard:false-positive label counts —
// everything else is "unlabeled", never silently treated as correct.
const (
	outcomeConfirmed     = "confirmed"
	outcomeFalsePositive = "false_positive"
	outcomeConflict      = "conflict"
	outcomeUnlabeled     = "unlabeled"
)

// classifyOutcome turns the two label flags into one outcome per mrDecision.
func classifyOutcome(confirmed, falsePositive bool) string {
	switch {
	case confirmed && falsePositive:
		return outcomeConflict
	case confirmed:
		return outcomeConfirmed
	case falsePositive:
		return outcomeFalsePositive
	default:
		return outcomeUnlabeled
	}
}

// precisionReport reports HOLD precision two ways: confirmed_hold_precision_pct
// (explicit labels only — the trustworthy number) and
// weak_signal_hold_precision_pct (the old formula, which counts unlabeled
// HOLDs as correct). confirmed_label_coverage_pct says how much of the
// confirmed number to trust; never show one percentage without the other.
type precisionReport struct {
	MergedScanned              int          `json:"merged_scanned"`
	HoldCount                  int          `json:"hold_count"`
	HoldConfirmed              int          `json:"hold_confirmed"`
	HoldFalsePositive          int          `json:"hold_false_positive"`
	HoldUnlabeled              int          `json:"hold_unlabeled"`
	HoldConflict               int          `json:"hold_conflict"`
	ConfirmedHoldPrecisionPct  *float64     `json:"confirmed_hold_precision_pct"`
	WeakSignalHoldPrecisionPct *float64     `json:"weak_signal_hold_precision_pct"`
	ConfirmedLabelCoveragePct  *float64     `json:"confirmed_label_coverage_pct"`
	UnlabeledRatePct           *float64     `json:"unlabeled_rate_pct"`
	Decisions                  []mrDecision `json:"decisions"`
}

// parseDecision extracts the decision token (HOLD/REVIEW/PROCEED) from a set of
// MR note bodies. GetMRNotes returns notes newest-first, so the first matching
// ReleaseGuard verdict is the newest one. Returns "" when no comment is present.
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

// computePrecision tallies HOLD decisions by label outcome and derives the
// confirmed and weak-signal precision percentages alongside label coverage.
func computePrecision(decisions []mrDecision) precisionReport {
	rep := precisionReport{MergedScanned: len(decisions)}
	classified := make([]mrDecision, len(decisions))
	for i, d := range decisions {
		d.Outcome = classifyOutcome(d.Confirmed, d.FalsePositive)
		classified[i] = d
		if d.Decision != "HOLD" {
			continue
		}
		rep.HoldCount++
		switch d.Outcome {
		case outcomeConfirmed:
			rep.HoldConfirmed++
		case outcomeFalsePositive:
			rep.HoldFalsePositive++
		case outcomeConflict:
			rep.HoldConflict++
		case outcomeUnlabeled:
			rep.HoldUnlabeled++
		}
	}
	rep.Decisions = classified

	if denominator := rep.HoldConfirmed + rep.HoldFalsePositive; denominator > 0 {
		v := 100 * float64(rep.HoldConfirmed) / float64(denominator)
		rep.ConfirmedHoldPrecisionPct = &v
	}
	if denominator := rep.HoldCount - rep.HoldConflict; denominator > 0 {
		v := 100 * float64(rep.HoldCount-rep.HoldConflict-rep.HoldFalsePositive) / float64(denominator)
		rep.WeakSignalHoldPrecisionPct = &v
	}
	if rep.HoldCount > 0 {
		coverage := 100 * float64(rep.HoldConfirmed+rep.HoldFalsePositive) / float64(rep.HoldCount)
		rep.ConfirmedLabelCoveragePct = &coverage
		unlabeledRate := 100 * float64(rep.HoldUnlabeled) / float64(rep.HoldCount)
		rep.UnlabeledRatePct = &unlabeledRate
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
			Confirmed:     hasLabel(mr.Labels, report.ConfirmedLabel),
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
	fmt.Printf("  merged MRs scanned:          %d\n", rep.MergedScanned)
	fmt.Printf("  HOLD decisions:              %d\n", rep.HoldCount)
	fmt.Printf("  HOLD confirmed:              %d\n", rep.HoldConfirmed)
	fmt.Printf("  HOLD false-positives:        %d\n", rep.HoldFalsePositive)
	fmt.Printf("  HOLD unlabeled:              %d\n", rep.HoldUnlabeled)
	fmt.Printf("  HOLD label conflicts:        %d\n", rep.HoldConflict)
	fmt.Printf("  confirmed HOLD precision:    %s\n", replayPct(rep.ConfirmedHoldPrecisionPct))
	fmt.Printf("  weak-signal HOLD precision:  %s\n", replayPct(rep.WeakSignalHoldPrecisionPct))
	fmt.Printf("  confirmed label coverage:    %s\n", replayPct(rep.ConfirmedLabelCoveragePct))
	fmt.Printf("  unlabeled rate:              %s\n", replayPct(rep.UnlabeledRatePct))
}
