package depgraph

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/twjohnwu/releaseGuard/internal/analysis/schema"
)

// reuse schema.Zone type since it's the same shape
type Zone = schema.Zone

func parseGoMod(path string) (deps map[string]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	deps = map[string]string{}
	sc := bufio.NewScanner(f)
	inBlock := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "require (") {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		if inBlock || strings.HasPrefix(line, "require ") {
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) >= 2 {
				deps[parts[0]] = parts[1]
			}
		}
	}
	return deps, sc.Err()
}

func GoModDiff(baseFile, headFile string) ([]Zone, error) {
	base, err := parseGoMod(baseFile)
	if err != nil {
		return nil, err
	}
	head, err := parseGoMod(headFile)
	if err != nil {
		return nil, err
	}
	var zones []Zone
	for mod, headV := range head {
		baseV, ok := base[mod]
		if !ok {
			zones = append(zones, Zone{Type: "dep_added", Detail: fmt.Sprintf("%s @ %s", mod, headV), Severity: "low"})
			continue
		}
		if baseV != headV {
			sev := "low"
			if isMajorBump(baseV, headV) {
				sev = "high"
			} else if isMinorBump(baseV, headV) {
				sev = "medium"
			}
			zones = append(zones, Zone{
				Type:     "dep_bump",
				Detail:   fmt.Sprintf("%s %s → %s", mod, baseV, headV),
				Severity: sev,
			})
		}
	}
	for mod, baseV := range base {
		if _, ok := head[mod]; !ok {
			zones = append(zones, Zone{Type: "dep_removed", Detail: fmt.Sprintf("%s (was %s)", mod, baseV), Severity: "medium"})
		}
	}
	return zones, nil
}

func isMajorBump(a, b string) bool {
	return semverMajor(a) != semverMajor(b)
}

func isMinorBump(a, b string) bool {
	if semverMajor(a) != semverMajor(b) {
		return false
	}
	return semverMinor(a) != semverMinor(b)
}

func semverMajor(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 2)
	return parts[0]
}

func semverMinor(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
