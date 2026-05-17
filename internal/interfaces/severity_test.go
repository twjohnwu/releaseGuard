package interfaces

import "testing"

func TestParseSeverityKnownValues(t *testing.T) {
	cases := map[string]Severity{
		"critical": SeverityCritical,
		"high":     SeverityHigh,
		"medium":   SeverityMedium,
		"low":      SeverityLow,
	}
	for in, want := range cases {
		if got := ParseSeverity(in); got != want {
			t.Errorf("ParseSeverity(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseSeverityUnknownCollapsesToInfo(t *testing.T) {
	for _, in := range []string{"", "warning", "CRITICAL", "info", "Low"} {
		if got := ParseSeverity(in); got != SeverityInfo {
			t.Errorf("ParseSeverity(%q)=%q want SeverityInfo", in, got)
		}
	}
}

func TestSeverityStringConversionIsTransparent(t *testing.T) {
	// Severity is `type Severity string`, so string(s) is the round-trip back.
	if s := string(SeverityCritical); s != "critical" {
		t.Errorf("string(SeverityCritical)=%q want critical", s)
	}
}
