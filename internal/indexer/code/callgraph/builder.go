package callgraph

import "context"

type Symbol struct {
	ID        string
	Kind      string
	Language  string
	File      string
	LineStart int
	LineEnd   int
	Signature string
	Hash      string
}

type Edge struct {
	Caller   string
	Callee   string
	CallFile string
	CallLine int
	Kind     string // direct | interface | dynamic
}

type BuildResult struct {
	Symbols []Symbol
	Edges   []Edge
}

type Builder interface {
	Build(ctx context.Context, repoPath string) (*BuildResult, error)
}
