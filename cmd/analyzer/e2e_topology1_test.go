// cmd/analyzer/e2e_topology1_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2ETopology1FullFlow(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", "/tmp/rg-e2e-t1", "./").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %s", out)
	}
	defer os.Remove("/tmp/rg-e2e-t1")

	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path":"foo.go","new_path":"foo.go","diff":"-x\n+y"}]`))
		case strings.HasSuffix(r.URL.Path, "/notes"):
			posted = true
			w.WriteHeader(201)
			w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cmd := exec.Command("/tmp/rg-e2e-t1")
	cmd.Env = []string{
		"AI_PROVIDER_KEY=test",
		"GITLAB_TOKEN=t",
		"GITLAB_API_BASE=" + srv.URL,
		"CI_PROJECT_ID=1",
		"CI_MERGE_REQUEST_IID=1",
		"RG_AGENT_SELECTIVE_TEST_ENABLED=true",
		"RG_AGENT_ROLLOUT_RISK_ENABLED=true",
		"RG_AGENT_OWNERSHIP_ENABLED=false",
		"RG_AGENT_AI_REVIEWER_ENABLED=false", // skip LLM call in this test
		"RG_RAG_ENABLED=false",
	}
	gotOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %s", gotOut)
	}
	if !posted {
		t.Fatalf("MR comment not posted; output=%s", gotOut)
	}
}
