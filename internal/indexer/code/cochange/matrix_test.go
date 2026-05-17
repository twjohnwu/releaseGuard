package cochange

import "testing"

func TestMatrixCounts(t *testing.T) {
	commits := [][]string{
		{"a.go", "b.go"},
		{"a.go", "c.go"},
		{"a.go", "b.go"},
	}
	m := BuildMatrix(commits)
	if m["a.go"]["b.go"] != 2 {
		t.Fatalf("ab=%d, want 2", m["a.go"]["b.go"])
	}
	if m["a.go"]["c.go"] != 1 {
		t.Fatalf("ac=%d, want 1", m["a.go"]["c.go"])
	}
}
