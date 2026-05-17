package coverage

import (
	"strings"
	"testing"
)

const lcovSample = `TN:
SF:foo.go
FN:10,Foo
DA:10,5
DA:11,5
end_of_record
TN:TestFoo
SF:foo.go
FN:10,Foo
DA:10,3
end_of_record
`

func TestParseLcovExtractsTestSymbols(t *testing.T) {
	p := NewLCOV()
	entries, err := p.Parse(strings.NewReader(lcovSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries")
	}
}
