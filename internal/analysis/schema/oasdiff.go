package schema

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type Zone struct {
	Type     string
	Detail   string
	Severity string // critical | high | medium | low
}

// runOasdiff is a function-typed seam for tests.
var runOasdiff = func(base, head string) ([]byte, error) {
	cmd := exec.Command("oasdiff", "diff", base, head, "--breaking-only", "-f", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("oasdiff: %w", err)
	}
	return out, nil
}

type oasdiffEntry struct {
	ID        string `json:"id"`
	Level     int    `json:"level"` // 3=critical, 2=warn, 1=info
	Text      string `json:"text"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
}

func OasdiffBreaking(baseFile, headFile string) ([]Zone, error) {
	out, err := runOasdiff(baseFile, headFile)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	var entries []oasdiffEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parse oasdiff: %w", err)
	}
	zones := make([]Zone, 0, len(entries))
	for _, e := range entries {
		sev := "low"
		if e.Level >= 3 {
			sev = "critical"
		} else if e.Level == 2 {
			sev = "high"
		}
		zones = append(zones, Zone{
			Type:     "breaking_api",
			Detail:   fmt.Sprintf("%s %s: %s", e.Operation, e.Path, e.Text),
			Severity: sev,
		})
	}
	return zones, nil
}
