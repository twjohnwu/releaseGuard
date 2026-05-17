package ownership

import (
	"strings"
	"testing"
)

func TestPhraseDoesNotContainBannedWords(t *testing.T) {
	c := candidate{Author: "alice", Source: "blame", Reason: "owns orders/"}
	got := phraseContext(c)
	for _, banned := range []string{"score", "expert", "top owner", "highest", "best"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Errorf("phrase contains banned word %q: %s", banned, got)
		}
	}
}

func TestPhraseUsesPassiveLanguage(t *testing.T) {
	c := candidate{Author: "alice", Source: "blame", Reason: "orders/handler.go", Score: 0.92}
	got := phraseContext(c)
	if !strings.Contains(got, "has recent changes") && !strings.Contains(got, "co-changes") {
		t.Errorf("expected passive phrase, got: %s", got)
	}
}
