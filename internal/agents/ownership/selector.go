package ownership

import (
	"math/rand"
	"sort"
)

type candidate struct {
	Author string
	Score  float64
	Source string // "blame" | "cochange" — internal only, NOT exposed
	Reason string // human-readable, used to build context (no score numbers)
}

func selectAndShuffle(cs []candidate, k int, seed int64) []candidate {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Score > cs[j].Score })
	if len(cs) < k {
		k = len(cs)
	}
	top := append([]candidate{}, cs[:k]...)
	r := rand.New(rand.NewSource(seed))
	r.Shuffle(len(top), func(i, j int) { top[i], top[j] = top[j], top[i] })
	return top
}
