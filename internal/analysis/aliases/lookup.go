package aliases

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const skipMarker = "(skip)"

type Lookup struct {
	m map[string]string
}

type fileShape struct {
	Aliases map[string]string `yaml:"aliases"`
}

func LoadFile(path string) (*Lookup, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f fileShape
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &Lookup{m: f.Aliases}, nil
}

// Resolve returns (repoName, ok). ok=false when alias is unknown OR marked (skip).
func (l *Lookup) Resolve(alias string) (string, bool) {
	v, ok := l.m[alias]
	if !ok {
		return "", false
	}
	if v == skipMarker {
		return "", false
	}
	return v, true
}

// IsSkipMarker tells the caller this alias was deliberately skipped (vs unknown).
func (l *Lookup) IsSkipMarker(alias string) bool {
	return l.m[alias] == skipMarker
}
