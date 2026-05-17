package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2EMockGitLab(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", "/tmp/rg-analyzer-e2e", "./").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %s", out)
	}
	defer os.Remove("/tmp/rg-analyzer-e2e")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.WriteHeader(200)
			w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/notes"):
			w.WriteHeader(201)
			w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cmd := exec.Command("/tmp/rg-analyzer-e2e")
	cmd.Env = []string{
		"AI_PROVIDER_KEY=fake",
		"GITLAB_TOKEN=fake",
		"GITLAB_API_BASE=" + srv.URL,
		"CI_PROJECT_ID=1",
		"CI_MERGE_REQUEST_IID=1",
		"RG_AGENT_SELECTIVE_TEST_ENABLED=false",
		"RG_AGENT_OWNERSHIP_ENABLED=false",
		"RG_AGENT_AI_REVIEWER_ENABLED=false",
		"RG_RAG_ENABLED=false",
	}
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("analyzer failed: %s", got)
	}
	if !strings.Contains(string(got), "topology") {
		t.Fatalf("expected topology log: %s", got)
	}
}
