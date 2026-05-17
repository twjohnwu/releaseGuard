package coverage

import "io"

type Entry struct {
	TestID       string // empty for unattributed coverage
	File         string
	FunctionName string
	LineStart    int
	LineEnd      int
}

type Parser interface {
	Parse(r io.Reader) ([]Entry, error)
}
