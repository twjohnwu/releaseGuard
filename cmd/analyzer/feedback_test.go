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

func TestComputePrecision(t *testing.T) {
	decisions := []mrDecision{
		{IID: 1, Decision: "HOLD", FalsePositive: false},
		{IID: 2, Decision: "HOLD", FalsePositive: true},
		{IID: 3, Decision: "REVIEW", FalsePositive: true},
		{IID: 4, Decision: "PROCEED"},
		{IID: 5, Decision: ""},
	}
	rep := computePrecision(decisions)
	if rep.MergedScanned != 5 {
		t.Errorf("scanned=%d", rep.MergedScanned)
	}
	if rep.HoldCount != 2 {
		t.Errorf("hold=%d", rep.HoldCount)
	}
	if rep.HoldFalsePositives != 1 {
		t.Errorf("fp=%d", rep.HoldFalsePositives)
	}
	if rep.PrecisionPct != 50.0 {
		t.Errorf("precision=%v want 50.0", rep.PrecisionPct)
	}
}

func TestComputePrecisionNoHolds(t *testing.T) {
	rep := computePrecision([]mrDecision{{IID: 1, Decision: "PROCEED"}})
	if rep.PrecisionPct != -1 {
		t.Errorf("precision should be -1 (n/a) with no HOLDs, got %v", rep.PrecisionPct)
	}
}
