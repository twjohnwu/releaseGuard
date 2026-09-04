package coverage

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

type LCOV struct{}

func NewLCOV() *LCOV { return &LCOV{} }

func (l *LCOV) Parse(r io.Reader) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(r)
	var curTest, curFile string
	var curLine int
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "TN:"):
			curTest = strings.TrimPrefix(line, "TN:")
		case strings.HasPrefix(line, "SF:"):
			curFile = strings.TrimPrefix(line, "SF:")
		case strings.HasPrefix(line, "FN:"):
			rest := strings.TrimPrefix(line, "FN:")
			parts := strings.SplitN(rest, ",", 2)
			if len(parts) == 2 {
				if n, err := strconv.Atoi(parts[0]); err == nil {
					curLine = n
				}
				out = append(out, Entry{
					TestID: curTest, File: curFile,
					FunctionName: parts[1], LineStart: curLine, LineEnd: curLine,
				})
			}
		case line == "end_of_record":
			curFile, curLine = "", 0
		}
	}
	return out, sc.Err()
}
