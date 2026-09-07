package report

import (
	"strings"
	"testing"
)

func TestLabelValidateExplicit(t *testing.T) {
	label := Label{
		HumanOutcome:   HumanOutcomeCorrect,
		EvidenceSource: EvidenceSourceReleaseDecision,
		Derivation:     DerivationExplicit,
		Confidence:     ConfidenceHigh,
		EvidenceRef:    "release-42",
		ReviewedBy:     "reviewer",
		ReviewedAt:     "2026-09-07T12:00:00Z",
	}
	if err := label.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLabelValidateRejectsUnknownEnums(t *testing.T) {
	valid := Label{
		HumanOutcome:   HumanOutcomeCorrect,
		EvidenceSource: EvidenceSourceReleaseDecision,
		Derivation:     DerivationExplicit,
		Confidence:     ConfidenceHigh,
	}
	tests := []struct {
		name   string
		mutate func(*Label)
		want   string
	}{
		{name: "human outcome", mutate: func(l *Label) { l.HumanOutcome = "unknown" }, want: `"unknown"`},
		{name: "evidence source", mutate: func(l *Label) { l.EvidenceSource = "unknown" }, want: `"unknown"`},
		{name: "derivation", mutate: func(l *Label) { l.Derivation = "unknown" }, want: `"unknown"`},
		{name: "confidence", mutate: func(l *Label) { l.Confidence = "unknown" }, want: `"unknown"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := valid
			tt.mutate(&label)
			err := label.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLabelValidateUnlabeledNeedsNoEvidence(t *testing.T) {
	label := Label{HumanOutcome: HumanOutcomeUnlabeled}
	if err := label.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLabelIsExplicit(t *testing.T) {
	tests := []struct {
		name  string
		label Label
		want  bool
	}{
		{name: "explicit labeled", label: Label{HumanOutcome: HumanOutcomeCorrect, Derivation: DerivationExplicit}, want: true},
		{name: "inferred labeled", label: Label{HumanOutcome: HumanOutcomeCorrect, Derivation: DerivationInferred}, want: false},
		{name: "explicit unlabeled", label: Label{HumanOutcome: HumanOutcomeUnlabeled, Derivation: DerivationExplicit}, want: false},
		{name: "zero", label: Label{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.label.IsExplicit(); got != tt.want {
				t.Errorf("IsExplicit() = %t, want %t", got, tt.want)
			}
		})
	}
}
