package cochange

import (
	"bufio"
	"io"
	"strings"
)

// ParsePorcelain parses `git blame --line-porcelain` and returns author email → line count.
func ParsePorcelain(r io.Reader) (map[string]int, error) {
	out := map[string]int{}
	sc := bufio.NewScanner(r)
	curEmail := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "author-mail "):
			email := strings.TrimPrefix(line, "author-mail ")
			email = strings.Trim(email, "<>")
			curEmail = email
		case strings.HasPrefix(line, "\t"):
			if curEmail != "" {
				out[curEmail]++
			}
		}
	}
	return out, sc.Err()
}
