package result

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/twjohnwu/releaseGuard/internal/interfaces"
	"github.com/twjohnwu/releaseGuard/internal/report"
)

// Report is the machine-readable CI artifact written alongside the MR comment.
// It captures the arbitration decision plus the raw per-agent outputs so that
// downstream tooling (dashboards, the feedback command, audits) can consume the
// run without scraping the rendered markdown.
type Report struct {
	SchemaVersion    string                   `json:"schema_version"`
	Decision         string                   `json:"decision"`
	Rationale        string                   `json:"rationale"`
	TriggeredSignals []report.Signal          `json:"triggered_signals"`
	Agents           []interfaces.AgentOutput `json:"agents"`
}

// WriteReport serializes the decision + agent outputs to path as JSON.
// An empty path disables writing (returns nil). The write is atomic: it goes to
// a temp file in the same directory and is renamed into place, so a reader never
// sees a partial file. Callers should treat a non-nil error as non-fatal — the
// MR comment is the primary output.
func WriteReport(path string, rec report.Recommendation, outs []interfaces.AgentOutput) error {
	if path == "" {
		return nil
	}
	rep := Report{
		SchemaVersion:    "1",
		Decision:         rec.Recommendation,
		Rationale:        rec.Rationale,
		TriggeredSignals: rec.TriggeredSignals,
		Agents:           outs,
	}
	if rep.TriggeredSignals == nil {
		rep.TriggeredSignals = []report.Signal{}
	}
	if rep.Agents == nil {
		rep.Agents = []interfaces.AgentOutput{}
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".releaseguard-report-*.json")
	if err != nil {
		return fmt.Errorf("create temp report: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		closeErr := tmp.Close()
		removeErr := os.Remove(tmpName)
		return fmt.Errorf("write temp report: %w", errors.Join(err, closeErr, removeErr))
	}
	if err := tmp.Close(); err != nil {
		removeErr := os.Remove(tmpName)
		return fmt.Errorf("close temp report: %w", errors.Join(err, removeErr))
	}
	if err := os.Rename(tmpName, path); err != nil {
		removeErr := os.Remove(tmpName)
		return fmt.Errorf("rename report: %w", errors.Join(err, removeErr))
	}
	return nil
}
