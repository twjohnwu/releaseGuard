package report

import (
	"fmt"
	"strings"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
)

type ImpactScopeReport struct {
	Recommendation       Recommendation
	RiskLevel            string
	RiskZones            []ZoneInfo
	SelectiveTests       *TestPlan
	AIReviewFindings     []interfaces.Finding
	HighSeverityFindings []interfaces.Finding
	SuggestedReviewers   []ReviewerInfo
	ImpactZones          []ImpactZone
	HiddenDeps           []DependencyHint
	EnabledFlags         map[string]bool
	FailedAgents         []string
}

type ZoneInfo struct {
	Type     string
	Detail   string
	Severity string
}

type TestPlan struct {
	Required      []string
	Skippable     []string
	Confidence    float64
	AnalysisLevel string
	Reason        string
}

type ReviewerInfo struct {
	Name    string
	Context string
}

type ImpactZone struct {
	Path                     string
	RecentActiveAuthorsCount int
	LookbackDays             int
}

type DependencyHint struct {
	From, To string
	Context  string
}

func emoji(rec string) string {
	switch rec {
	case "HOLD":
		return "🔴"
	case "REVIEW":
		return "🟡"
	default:
		return "✅"
	}
}

func Render(r ImpactScopeReport) string {
	var sb strings.Builder
	rec := r.Recommendation.Recommendation
	if rec == "" {
		rec = "PROCEED"
	}
	sb.WriteString(fmt.Sprintf("## %s ReleaseGuard recommendation: %s\n", emoji(rec), rec))
	for _, s := range r.Recommendation.TriggeredSignals {
		sb.WriteString(fmt.Sprintf("> %s [%s] %s: %s\n", iconForKind(s.Kind), s.Agent, s.Kind, s.Detail))
	}
	sb.WriteString("\n---\n\n")
	sb.WriteString("## Impact Scope Report\n\n")
	if r.RiskLevel != "" {
		sb.WriteString("### Risk level: " + r.RiskLevel + "\n")
		for _, z := range r.RiskZones {
			sb.WriteString(fmt.Sprintf("- %s %s · %s: %s\n",
				severityEmoji(z.Severity), z.Severity, z.Type, z.Detail))
		}
		sb.WriteString("\n")
	}
	if r.SelectiveTests != nil {
		sb.WriteString("### Selective test plan\n")
		sb.WriteString(fmt.Sprintf("- analysis_level: %s\n", r.SelectiveTests.AnalysisLevel))
		sb.WriteString(fmt.Sprintf("- confidence: %.2f\n", r.SelectiveTests.Confidence))
		if r.SelectiveTests.Reason != "" {
			sb.WriteString(fmt.Sprintf("- reason: %s\n", r.SelectiveTests.Reason))
		}
		sb.WriteString(fmt.Sprintf("- required (%d):\n", len(r.SelectiveTests.Required)))
		const requiredPreview = 5
		all := r.SelectiveTests.Required
		head := all
		if len(head) > requiredPreview {
			head = head[:requiredPreview]
		}
		for _, t := range head {
			sb.WriteString(fmt.Sprintf("  - `%s`\n", t))
		}
		if len(all) > requiredPreview {
			rest := all[requiredPreview:]
			sb.WriteString(fmt.Sprintf("\n  <details><summary>… %d more</summary>\n\n", len(rest)))
			for _, t := range rest {
				sb.WriteString(fmt.Sprintf("  - `%s`\n", t))
			}
			sb.WriteString("  </details>\n")
		}
		sb.WriteString(fmt.Sprintf("- skippable: %d\n\n", len(r.SelectiveTests.Skippable)))
	}
	if len(r.SuggestedReviewers) > 0 {
		sb.WriteString("### 可考慮邀請 review 的人\n> 系統提供相關背景，最終 reviewer 由 MR 作者決定。\n")
		for _, x := range r.SuggestedReviewers {
			sb.WriteString(fmt.Sprintf("- @%s — %s\n", x.Name, x.Context))
		}
		sb.WriteString("\n")
	}
	if len(r.AIReviewFindings) > 0 {
		sb.WriteString("<details><summary>📝 AI review findings</summary>\n\n")
		for _, f := range r.AIReviewFindings {
			loc := ""
			if f.Location != nil {
				loc = fmt.Sprintf(" (%s:%d)", f.Location.File, f.Location.LineStart)
			}
			sb.WriteString(fmt.Sprintf("- **%s**%s: %s\n", f.Severity, loc, f.Title))
		}
		sb.WriteString("\n</details>\n")
	}
	if rec == "HOLD" || rec == "REVIEW" {
		sb.WriteString("\n---\n")
		sb.WriteString(fmt.Sprintf("_Think this %s is wrong? Add the label `%s` to this MR to flag a false positive — it feeds gate-precision calibration._\n", rec, FalsePositiveLabel))
	}
	return sb.String()
}

// FalsePositiveLabel is the GitLab MR label reviewers add when they believe a
// HOLD/REVIEW gate misfired. The `analyzer feedback` command counts these
// against emitted decisions to produce a precision report.
const FalsePositiveLabel = "releaseguard:false-positive"

func severityEmoji(sev string) string {
	switch sev {
	case "critical":
		return "🔴"
	case "high":
		return "🟠"
	case "medium":
		return "🟡"
	case "low":
		return "⚪"
	default:
		return "·"
	}
}

func iconForKind(k string) string {
	switch k {
	case "critical_finding", "high_risk":
		return "🚨"
	case "agent_failure":
		return "⚠️"
	default:
		return "•"
	}
}
