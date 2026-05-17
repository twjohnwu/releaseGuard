package ownership

import "testing"

func TestSelectTopKThenShuffle(t *testing.T) {
	cs := []candidate{
		{Author: "a", Score: 0.9},
		{Author: "b", Score: 0.6},
		{Author: "c", Score: 0.3},
		{Author: "d", Score: 0.5},
		{Author: "e", Score: 0.7},
	}
	out := selectAndShuffle(cs, 3, 1234)
	if len(out) != 3 {
		t.Fatalf("expected 3, got %d", len(out))
	}
	want := map[string]bool{"a": true, "e": true, "b": true}
	for _, c := range out {
		if !want[c.Author] {
			t.Errorf("unexpected author in top3: %s", c.Author)
		}
	}
}

func TestSelectFewerThanK(t *testing.T) {
	out := selectAndShuffle([]candidate{{Author: "x", Score: 1}}, 3, 0)
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
}
