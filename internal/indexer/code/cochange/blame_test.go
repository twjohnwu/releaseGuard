package cochange

import (
	"strings"
	"testing"
)

const blameSample = `abc123 1 1 1
author Alice
author-mail <alice@example.com>
author-time 1700000000
author-tz +0000
committer Alice
filename foo.go
	first line
def456 2 2 1
author Bob
author-mail <bob@example.com>
author-time 1710000000
author-tz +0000
filename foo.go
	second line
abc123 3 3 1
author Alice
author-mail <alice@example.com>
filename foo.go
	third line
`

func TestParseBlamePorcelainCounts(t *testing.T) {
	authors, err := ParsePorcelain(strings.NewReader(blameSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if authors["alice@example.com"] != 2 || authors["bob@example.com"] != 1 {
		t.Fatalf("unexpected: %+v", authors)
	}
}
