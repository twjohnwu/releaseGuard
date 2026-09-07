package report

import "fmt"

// ConfirmedLabel is the GitLab MR label reviewers add once they have
// verified ReleaseGuard's verdict against a real outcome (release decision,
// post-merge change, deployment, or incident). It is a stronger, opt-in
// signal than FalsePositiveLabel: it marks a human-confirmed data point for
// replay precision, not just a disagreement flag.
const ConfirmedLabel = "releaseguard:confirmed"

// Label records a human judgment about one replay case's expected outcome.
// It is the schema replay expected.json files use to distinguish
// human-confirmed data from ReleaseGuard's own inferred guesses — only
// explicit labels may feed "confirmed" precision metrics.
type Label struct {
	HumanOutcome   string `json:"human_outcome"`
	EvidenceSource string `json:"evidence_source"`
	Derivation     string `json:"derivation"`
	Confidence     string `json:"confidence"`
	EvidenceRef    string `json:"evidence_ref,omitempty"`
	ReviewedBy     string `json:"reviewed_by,omitempty"`
	ReviewedAt     string `json:"reviewed_at,omitempty"`
}

// HumanOutcome values: what a human (or an inference from human-authored
// signals) determined about a ReleaseGuard verdict.
const (
	HumanOutcomeCorrect      string = "correct"
	HumanOutcomeOvercautious string = "overcautious"
	HumanOutcomeMissedRisk   string = "missed_risk"
	HumanOutcomeUnlabeled    string = "unlabeled"
)

// EvidenceSource values: where the human judgment came from.
const (
	EvidenceSourceReviewerComment string = "reviewer_comment"
	EvidenceSourceReleaseDecision string = "release_decision"
	EvidenceSourcePostMergeChange string = "post_merge_change"
	EvidenceSourceDeployment      string = "deployment"
	EvidenceSourceIncident        string = "incident"
)

// Derivation values: whether the label was stated by a human directly
// (explicit) or reconstructed from other signals (inferred).
const (
	DerivationExplicit string = "explicit"
	DerivationInferred string = "inferred"
)

// Confidence values: how much to trust an inferred (non-explicit) label.
const (
	ConfidenceHigh   string = "high"
	ConfidenceMedium string = "medium"
	ConfidenceLow    string = "low"
)

// Validate rejects unknown enum values. A Label whose HumanOutcome is
// "unlabeled" needs nothing else set — it is the catch-all for "no human
// judgment yet".
func (l Label) Validate() error {
	switch l.HumanOutcome {
	case HumanOutcomeCorrect, HumanOutcomeOvercautious, HumanOutcomeMissedRisk:
	case HumanOutcomeUnlabeled:
		return nil
	default:
		return fmt.Errorf("label: unknown human_outcome %q", l.HumanOutcome)
	}
	switch l.EvidenceSource {
	case EvidenceSourceReviewerComment, EvidenceSourceReleaseDecision, EvidenceSourcePostMergeChange, EvidenceSourceDeployment, EvidenceSourceIncident:
	default:
		return fmt.Errorf("label: unknown evidence_source %q", l.EvidenceSource)
	}
	switch l.Derivation {
	case DerivationExplicit, DerivationInferred:
	default:
		return fmt.Errorf("label: unknown derivation %q", l.Derivation)
	}
	switch l.Confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	default:
		return fmt.Errorf("label: unknown confidence %q", l.Confidence)
	}
	return nil
}

// IsExplicit reports whether this label is a genuine human-confirmed data
// point: derivation "explicit" and not the "unlabeled" catch-all.
func (l Label) IsExplicit() bool {
	return l.Derivation == DerivationExplicit && l.HumanOutcome != HumanOutcomeUnlabeled
}
