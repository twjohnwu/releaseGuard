package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func newFakeGitHub(t *testing.T, sawAuthorization *atomic.Bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			sawAuthorization.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/name/pulls":
			if r.URL.Query().Get("page") == "1" {
				if _, err := w.Write([]byte(`[
					{"number":21,"merged_at":"2026-08-20T10:00:00Z"},
					{"number":22,"merged_at":null}
				]`)); err != nil {
					t.Errorf("write response: %v", err)
				}
				return
			}
			if _, err := w.Write([]byte(`[]`)); err != nil {
				t.Errorf("write response: %v", err)
			}
		case "/repos/owner/name/pulls/21/files":
			if r.URL.Query().Get("page") == "1" {
				if _, err := w.Write([]byte(`[
					{"filename":"internal/new.go","previous_filename":"internal/old.go","patch":"@@ -1 +1 @@\n-old\n+new","status":"renamed"}
				]`)); err != nil {
					t.Errorf("write response: %v", err)
				}
				return
			}
			if _, err := w.Write([]byte(`[]`)); err != nil {
				t.Errorf("write response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReplayImportGitHub(t *testing.T) {
	var sawAuthorization atomic.Bool
	srv := newFakeGitHub(t, &sawAuthorization)
	out := t.TempDir()

	summary, err := runReplayImport([]string{
		"--source", "github",
		"--repo", "owner/name",
		"--since", "2026-08-01T00:00:00Z",
		"--out", out,
	}, envOverride{apiBase: srv.URL, token: ""})
	if err != nil {
		t.Fatalf("runReplayImport() error = %v", err)
	}
	if summary.Imported != 1 {
		t.Fatalf("Imported = %d, want 1", summary.Imported)
	}
	if summary.SkippedUnlabeled != 0 || summary.Failed != 0 {
		t.Errorf("summary = %+v, want no skipped or failed cases", summary)
	}
	if sawAuthorization.Load() {
		t.Fatal("unauthenticated import sent Authorization header")
	}

	caseName := replayImportGitHubCaseName("owner", "name", 21)
	caseDir := filepath.Join(out, caseName)
	diff, err := readReplayDiff(filepath.Join(caseDir, "diff.json"))
	if err != nil {
		t.Fatalf("readReplayDiff() error = %v", err)
	}
	if len(diff) != 1 || diff[0].OldPath != "internal/old.go" || diff[0].NewPath != "internal/new.go" || !diff[0].RenamedFile {
		t.Fatalf("diff = %+v, want renamed file", diff)
	}

	expected := readExpectedJSON(t, filepath.Join(caseDir, "expected.json"))
	if expected.Recommendation != "" || !expected.NeedsLabel || expected.Source != "github-unlabeled" {
		t.Errorf("expected = %+v, want empty/needs_label=true/github-unlabeled", expected)
	}

	manifestData, err := os.ReadFile(filepath.Join(out, ".manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]replayImportManifestEntry
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	entry := manifest[caseName]
	if entry.Source != "github" || entry.Project != 0 || entry.IID != 21 || entry.Repo != "owner/name" {
		t.Errorf("manifest entry = %+v", entry)
	}

	metrics, err := runReplayCases(out, 60)
	if err != nil {
		t.Fatalf("runReplayCases() error = %v", err)
	}
	if metrics.Unlabeled != summary.Imported {
		t.Errorf("Unlabeled = %d, want %d", metrics.Unlabeled, summary.Imported)
	}
	if metrics.Cases != 0 {
		t.Errorf("Cases = %d, want 0", metrics.Cases)
	}
}

func TestReplayImportGitHubDiffErrorUsesRepoAndPRWording(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/name/pulls":
			if r.URL.Query().Get("page") == "1" {
				if _, err := w.Write([]byte(`[{"number":31,"merged_at":"2026-08-20T10:00:00Z"}]`)); err != nil {
					t.Errorf("write response: %v", err)
				}
				return
			}
			if _, err := w.Write([]byte(`[]`)); err != nil {
				t.Errorf("write response: %v", err)
			}
		case "/repos/owner/name/pulls/31/files":
			http.Error(w, "files unavailable", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderrWriter
	t.Cleanup(func() {
		os.Stderr = originalStderr
		if err := stderrReader.Close(); err != nil {
			t.Logf("close stderr reader: %v", err)
		}
		// stderrWriter is already closed on the happy path (below); this is
		// just a safety net for early t.Fatalf exits, so a "file already
		// closed" error here is expected, not a failure.
		if err := stderrWriter.Close(); err != nil {
			t.Logf("close stderr writer: %v", err)
		}
	})

	summary, runErr := runReplayImport([]string{
		"--source", "github",
		"--repo", "owner/name",
		"--since", "2026-08-01T00:00:00Z",
		"--out", t.TempDir(),
	}, envOverride{apiBase: srv.URL})
	os.Stderr = originalStderr
	if err := stderrWriter.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	captured, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if runErr != nil {
		t.Fatalf("runReplayImport() error = %v", runErr)
	}
	if summary.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", summary.Failed)
	}
	line := string(captured)
	if !strings.Contains(line, "repo owner/name PR #31 diff:") {
		t.Errorf("stderr = %q, want repo and PR wording", line)
	}
	for _, forbidden := range []string{"project 0", "MR !"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("stderr = %q, must not contain %q", line, forbidden)
		}
	}
}

func TestReplayImportGitHubValidatesRepoAndSourceFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing repo", args: []string{"--source", "github"}},
		{name: "malformed repo", args: []string{"--source", "github", "--repo", "owner/name/extra"}},
		{name: "github project", args: []string{"--source", "github", "--repo", "owner/name", "--project", "1"}},
		{name: "gitlab repo", args: []string{"--source", "gitlab", "--repo", "owner/name", "--project", "1"}},
		{name: "unknown source", args: []string{"--source", "bitbucket"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := runReplayImport(tt.args, envOverride{apiBase: "http://unused"}); err == nil {
				t.Fatal("runReplayImport() error = nil, want validation error")
			}
		})
	}
}
