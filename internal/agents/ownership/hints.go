package ownership

import "fmt"

type Hint struct {
	From    string
	To      string
	Context string
}

const minRatio = 0.5

func hintsFromMatrix(matrix map[string]map[string]int, totals map[string]int) []Hint {
	var out []Hint
	for from, peers := range matrix {
		fromTotal := totals[from]
		if fromTotal == 0 {
			continue
		}
		for to, count := range peers {
			ratio := float64(count) / float64(fromTotal)
			if ratio >= minRatio {
				out = append(out, Hint{
					From: from, To: to,
					Context: fmt.Sprintf("these two zones changed together in %d/%d past commits", count, fromTotal),
				})
			}
		}
	}
	return out
}
