// cover2lcov converts a Go `cover.out` (coverprofile) to an LCOV-formatted
// stream, attributing all hit functions to a single test name supplied via -tn.
//
// Usage:
//   cover2lcov -tn=TestX -module=github.com/twjohnwu/releaseGuard cover.out
//
// Output (stdout):
//   TN:TestX
//   SF:internal/report/arbitration.go
//   FN:33,Arbitrate
//   FN:43,arbitrateInner
//   end_of_record
//
// Only functions with non-zero coverage are emitted. Module-prefixed file
// paths are stripped to repo-relative form so they match `symbols.file`.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

func main() {
	tn := flag.String("tn", "", "test name to use for TN: records (required)")
	mod := flag.String("module", "", "Go module path to strip from file paths (default: detect via go.mod in cwd)")
	flag.Parse()
	if *tn == "" {
		fmt.Fprintln(os.Stderr, "cover2lcov: -tn=<TestName> is required")
		os.Exit(2)
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "cover2lcov: exactly one cover.out path required")
		os.Exit(2)
	}
	module := *mod
	if module == "" {
		module = detectModule()
	}
	if err := run(flag.Arg(0), *tn, module, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(coverPath, testName, modulePrefix string, out io.Writer) error {
	cmd := exec.Command("go", "tool", "cover", "-func="+coverPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	type fnRec struct {
		Line int
		Name string
	}
	bySF := map[string][]fnRec{}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "total:") {
			continue
		}
		// format: <path>:<line>:\t<funcname>\t<pct>%
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		path := parts[0]
		var lineNum int
		if _, err := fmt.Sscanf(parts[1], "%d", &lineNum); err != nil {
			continue
		}
		fields := strings.Fields(parts[2])
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		pct := fields[len(fields)-1]
		// keep only non-zero coverage
		if pct == "0.0%" {
			continue
		}
		sf := stripModulePrefix(path, modulePrefix)
		bySF[sf] = append(bySF[sf], fnRec{Line: lineNum, Name: name})
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("go tool cover failed: %w", err)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(bySF) == 0 {
		// empty cover output: emit nothing (caller's `>>` produces no rows)
		return nil
	}
	sortedSFs := make([]string, 0, len(bySF))
	for sf := range bySF {
		sortedSFs = append(sortedSFs, sf)
	}
	sort.Strings(sortedSFs)
	w := bufio.NewWriter(out)
	defer w.Flush()
	for _, sf := range sortedSFs {
		fns := bySF[sf]
		sort.Slice(fns, func(i, j int) bool { return fns[i].Line < fns[j].Line })
		fmt.Fprintf(w, "TN:%s\n", testName)
		fmt.Fprintf(w, "SF:%s\n", sf)
		for _, fn := range fns {
			fmt.Fprintf(w, "FN:%d,%s\n", fn.Line, fn.Name)
		}
		fmt.Fprintln(w, "end_of_record")
	}
	return nil
}

func stripModulePrefix(path, modulePrefix string) string {
	if modulePrefix != "" && strings.HasPrefix(path, modulePrefix+"/") {
		return strings.TrimPrefix(path, modulePrefix+"/")
	}
	return path
}

func detectModule() string {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "module"))
		}
	}
	return ""
}
