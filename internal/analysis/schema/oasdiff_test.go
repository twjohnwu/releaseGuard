package schema

import (
	"errors"
	"os/exec"
	"testing"
)

func TestOasdiffParsesBreakingJSON(t *testing.T) {
	// fake exec: replace runner with fixture output
	old := runOasdiff
	defer func() { runOasdiff = old }()
	runOasdiff = func(base, head string) ([]byte, error) {
		return []byte(`[{"id":"request-property-removed","level":3,"text":"removed discount_code","operation":"POST","path":"/v2/orders"}]`), nil
	}
	zones, err := OasdiffBreaking("base.yaml", "head.yaml")
	if err != nil {
		t.Fatalf("oasdiff: %v", err)
	}
	if len(zones) != 1 || zones[0].Severity != "critical" {
		t.Fatalf("got %+v", zones)
	}
}

func TestOasdiffPropagatesError(t *testing.T) {
	old := runOasdiff
	defer func() { runOasdiff = old }()
	runOasdiff = func(base, head string) ([]byte, error) {
		return nil, &exec.ExitError{}
	}
	_, err := OasdiffBreaking("a", "b")
	if err == nil || !errors.Is(err, err) /* sanity */ {
		t.Fatalf("expected propagated error")
	}
}
