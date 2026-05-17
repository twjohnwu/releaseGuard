package interfaces

// ParseSeverity normalises a severity string (case-sensitive lower) into the
// typed Severity enum. Unknown / empty strings collapse to SeverityInfo so
// callers don't have to handle an error path for low-stakes data sources
// (config files, lcov artifacts, third-party diff metadata).
func ParseSeverity(s string) Severity {
	switch s {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium":
		return SeverityMedium
	case "low":
		return SeverityLow
	default:
		return SeverityInfo
	}
}
