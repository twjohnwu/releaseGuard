package reviewer

import (
	"fmt"
	"strings"
)

func ComposeSystemPrompt(basePrompts []string, ragSnippets []string) string {
	var sb strings.Builder
	sb.WriteString("You are a careful code reviewer. ")
	sb.WriteString("You MUST respond by calling the submit_review tool. ")
	sb.WriteString("Do not write findings as plain text.\n\n")
	for _, p := range basePrompts {
		sb.WriteString(p)
		sb.WriteString("\n\n")
	}
	if len(ragSnippets) > 0 {
		sb.WriteString("Relevant context:\n")
		for _, r := range ragSnippets {
			sb.WriteString("- ")
			sb.WriteString(r)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func composeUserPrompt(commitMsg string, diffBlocks []string) string {
	return fmt.Sprintf("Commit message:\n%s\n\nDiff:\n%s",
		commitMsg, strings.Join(diffBlocks, "\n\n"))
}
