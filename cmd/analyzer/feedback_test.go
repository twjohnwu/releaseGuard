package main

import "testing"

func TestParseDecision(t *testing.T) {
	cases := []struct {
		name  string
		notes []string
		want  string
	}{
		{"hold", []string{"## 🔴 ReleaseGuard recommendation: HOLD\n> ..."}, "HOLD"},
		{"review", []string{"## 🟡 ReleaseGuard recommendation: REVIEW"}, "REVIEW"},
		{"proceed", []string{"## ✅ ReleaseGuard recommendation: PROCEED"}, "PROCEED"},
		{"none", []string{"a random comment", "another"}, ""},
		{"first-match-wins", []string{"noise", "ReleaseGuard recommendation: HOLD extra"}, "HOLD"},
	}
	for _, c := range cases {
		if got := parseDecision(c.notes); got != c.want {
			t.Errorf("%s: parseDecision=%q want %q", c.name, got, c.want)
		}
	}
}

func TestParseDecisionNewestFirst(t *testing.T) {
	notes := []string{
		"## ReleaseGuard recommendation: PROCEED (newest)",
		"## ReleaseGuard recommendation: HOLD (older)",
	}
	if got := parseDecision(notes); got != "PROCEED" {
		t.Errorf("parseDecision() = %q, want PROCEED", got)
	}
}

// floatPtr is a small test helper for wanting a specific *float64 in the
// table below.
func floatPtr(v float64) *float64 { return &v }

// closeEnoughPct compares two *float64 percentages with tolerance, since some
// expected values (e.g. 33.33%) are repeating decimals.
func closeEnoughPct(t *testing.T, name string, got, want *float64) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Errorf("%s = %v, want %v", name, got, want)
		return
	}
	if got == nil {
		return
	}
	const eps = 0.01
	diff := *got - *want
	if diff < -eps || diff > eps {
		t.Errorf("%s = %v, want %v", name, *got, *want)
	}
}

func TestComputePrecision(t *testing.T) {
	cases := []struct {
		name          string
		decisions     []mrDecision
		wantHold      int
		wantConfirmed int
		wantFP        int
		wantUnlabeled int
		wantConflict  int
		wantConfPct   *float64
		wantWeakPct   *float64
		wantCoverage  *float64
		wantUnlabPct  *float64
	}{
		{
			name: "one confirmed, one false-positive",
			decisions: []mrDecision{
				{IID: 1, Decision: "HOLD", Confirmed: true},
				{IID: 2, Decision: "HOLD", FalsePositive: true},
				{IID: 3, Decision: "PROCEED"},
			},
			wantHold: 2, wantConfirmed: 1, wantFP: 1, wantUnlabeled: 0, wantConflict: 0,
			wantConfPct:  floatPtr(50),
			wantWeakPct:  floatPtr(50),
			wantCoverage: floatPtr(100),
			wantUnlabPct: floatPtr(0),
		},
		{
			name: "one confirmed, two unlabeled",
			decisions: []mrDecision{
				{IID: 1, Decision: "HOLD", Confirmed: true},
				{IID: 2, Decision: "HOLD"},
				{IID: 3, Decision: "HOLD"},
			},
			wantHold: 3, wantConfirmed: 1, wantFP: 0, wantUnlabeled: 2, wantConflict: 0,
			wantConfPct:  floatPtr(100),
			wantWeakPct:  floatPtr(100),
			wantCoverage: floatPtr(100.0 / 3),
			wantUnlabPct: floatPtr(200.0 / 3),
		},
		{
			name: "one HOLD with both labels is a conflict",
			decisions: []mrDecision{
				{IID: 1, Decision: "HOLD", Confirmed: true, FalsePositive: true},
			},
			wantHold: 1, wantConfirmed: 0, wantFP: 0, wantUnlabeled: 0, wantConflict: 1,
			wantConfPct:  nil,
			wantWeakPct:  nil,
			wantCoverage: floatPtr(0),
			wantUnlabPct: floatPtr(0),
		},
		{
			name:      "no HOLDs at all",
			decisions: []mrDecision{{IID: 1, Decision: "PROCEED"}},
			wantHold:  0, wantConfirmed: 0, wantFP: 0, wantUnlabeled: 0, wantConflict: 0,
			wantConfPct:  nil,
			wantWeakPct:  nil,
			wantCoverage: nil,
			wantUnlabPct: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := computePrecision(c.decisions)
			if rep.MergedScanned != len(c.decisions) {
				t.Errorf("scanned=%d want %d", rep.MergedScanned, len(c.decisions))
			}
			if rep.HoldCount != c.wantHold {
				t.Errorf("hold=%d want %d", rep.HoldCount, c.wantHold)
			}
			if rep.HoldConfirmed != c.wantConfirmed {
				t.Errorf("confirmed=%d want %d", rep.HoldConfirmed, c.wantConfirmed)
			}
			if rep.HoldFalsePositive != c.wantFP {
				t.Errorf("fp=%d want %d", rep.HoldFalsePositive, c.wantFP)
			}
			if rep.HoldUnlabeled != c.wantUnlabeled {
				t.Errorf("unlabeled=%d want %d", rep.HoldUnlabeled, c.wantUnlabeled)
			}
			if rep.HoldConflict != c.wantConflict {
				t.Errorf("conflict=%d want %d", rep.HoldConflict, c.wantConflict)
			}
			closeEnoughPct(t, "confirmed_hold_precision_pct", rep.ConfirmedHoldPrecisionPct, c.wantConfPct)
			closeEnoughPct(t, "weak_signal_hold_precision_pct", rep.WeakSignalHoldPrecisionPct, c.wantWeakPct)
			closeEnoughPct(t, "confirmed_label_coverage_pct", rep.ConfirmedLabelCoveragePct, c.wantCoverage)
			closeEnoughPct(t, "unlabeled_rate_pct", rep.UnlabeledRatePct, c.wantUnlabPct)
		})
	}
}

func TestComputePrecisionOutcomeField(t *testing.T) {
	rep := computePrecision([]mrDecision{
		{IID: 1, Decision: "HOLD", Confirmed: true},
		{IID: 2, Decision: "HOLD", FalsePositive: true},
		{IID: 3, Decision: "HOLD", Confirmed: true, FalsePositive: true},
		{IID: 4, Decision: "HOLD"},
	})
	want := []string{outcomeConfirmed, outcomeFalsePositive, outcomeConflict, outcomeUnlabeled}
	for i, d := range rep.Decisions {
		if d.Outcome != want[i] {
			t.Errorf("decisions[%d].Outcome = %q, want %q", i, d.Outcome, want[i])
		}
	}
}
