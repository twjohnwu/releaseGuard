package testselect

import (
	"path/filepath"
	"strings"

	"github.com/acme/releaseguard/internal/interfaces"
)

func runL1(diff []interfaces.DiffFile) (required, skippable []string, confidence float64, reason string) {
	required = []string{}
	skippable = []string{}
	confidence = L1BaseConfidence
	hasGo, hasTS, hasOther := false, false, false
	dirs := map[string]struct{}{}
	for _, f := range diff {
		switch {
		case strings.HasSuffix(f.Path, ".go"):
			hasGo = true
		case strings.HasSuffix(f.Path, ".ts") || strings.HasSuffix(f.Path, ".tsx"):
			hasTS = true
		default:
			hasOther = true
		}
		dir := filepath.Dir(f.Path)
		dirs[dir] = struct{}{}

		// same-file test: foo.go → foo_test.go (only when the file itself is .go)
		if strings.HasSuffix(f.Path, ".go") && !strings.HasSuffix(f.Path, "_test.go") {
			testPath := strings.TrimSuffix(f.Path, ".go") + "_test.go"
			required = appendUnique(required, testPath)
		}
		if strings.HasSuffix(f.Path, "_test.go") {
			required = appendUnique(required, f.Path)
		}
		// same-package: list every *_test.go in same dir
		// (placeholder: actual file listing requires repo checkout; record the dir intent)
		if dir != "." && dir != "" {
			required = appendUnique(required, dir+"/...")
		}
	}

	if hasGo && hasTS {
		confidence -= 0.2
	}
	if hasOther {
		confidence -= 0.1
	}
	if len(dirs) > 3 {
		confidence -= 0.1
	}
	if confidence < 0 {
		confidence = 0
	}

	reason = "L1 file-path mapping; only same-package tests are guaranteed"
	return
}

func appendUnique(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}
