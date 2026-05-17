package cochange

type Matrix map[string]map[string]int

// BuildMatrix counts how often each pair of files appears in the same commit.
func BuildMatrix(commits [][]string) Matrix {
	m := Matrix{}
	for _, files := range commits {
		for i, fi := range files {
			for j, fj := range files {
				if i == j {
					continue
				}
				if m[fi] == nil {
					m[fi] = map[string]int{}
				}
				m[fi][fj]++
				_ = j
			}
		}
	}
	return m
}
