package ownership

import "testing"

func TestHintsTriggersOnRatio(t *testing.T) {
	matrix := map[string]map[string]int{
		"orders/":   {"payments/": 18},
		"payments/": {"orders/": 18},
	}
	totals := map[string]int{"orders/": 22, "payments/": 22}
	hints := hintsFromMatrix(matrix, totals)
	if len(hints) == 0 {
		t.Fatal("expected hints, got 0")
	}
	if hints[0].Context == "" {
		t.Fatal("context missing")
	}
}

func TestHintsBelowThresholdSuppressed(t *testing.T) {
	matrix := map[string]map[string]int{"a/": {"b/": 1}}
	totals := map[string]int{"a/": 100}
	hints := hintsFromMatrix(matrix, totals)
	if len(hints) != 0 {
		t.Fatalf("expected suppressed: %+v", hints)
	}
}
