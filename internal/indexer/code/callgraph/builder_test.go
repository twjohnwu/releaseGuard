package callgraph

import "testing"

func TestSymbolEdgeShape(t *testing.T) {
	s := Symbol{ID: "pkg.Foo", File: "x.go", LineStart: 1, LineEnd: 5}
	e := Edge{Caller: "pkg.Foo", Callee: "pkg.Bar", Kind: "direct"}
	if s.ID == "" || e.Caller == "" {
		t.Fatal("zero")
	}
}
