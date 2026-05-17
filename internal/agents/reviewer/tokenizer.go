package reviewer

import "strings"

// Approximate token count: 1 token ≈ 4 characters of English (good enough for PoC).
// Use github.com/tiktoken-go/tokenizer for production calibration.
func ApproxTokens(s string) int {
	return len(s) / 4
}

// TruncateToBudget joins prompts in order. If joined exceeds budget,
// drops trailing prompts (keeps the first N that fit).
// Returns the joined prompt and a bool indicating whether truncation happened.
func TruncateToBudget(prompts []string, budgetTokens int) (string, bool) {
	used := 0
	out := []string{}
	for _, p := range prompts {
		t := ApproxTokens(p)
		if used+t > budgetTokens {
			return strings.Join(out, "\n\n"), true
		}
		used += t
		out = append(out, p)
	}
	return strings.Join(out, "\n\n"), false
}
