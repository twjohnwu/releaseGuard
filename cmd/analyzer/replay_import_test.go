package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGitLab serves the three endpoints replay-import needs, in the same
// plain-path style e2e_test.go uses (no /api/v4 prefix baked into the
// mux — that lives in GITLAB_API_BASE).
type fakeGitLabMR struct {
	IID    int      `json:"iid"`
	Title  string   `json:"title"`
	Labels []string `json:"labels"`
}

func newFakeGitLab(t *testing.T, mrs []fakeGitLabMR, notesByIID map[int][]string) *httptest.Server {
	t.Helper()
	notesJSON := func(bodies []string) []byte {
		type note struct {
			Body string `json:"body"`
		}
		out := make([]note, 0, len(bodies))
		for _, b := range bodies {
			out = append(out, note{Body: b})
		}
		data, err := json.Marshal(out)
		if err != nil {
			t.Fatalf("marshal notes: %v", err)
		}
		return data
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/merge_requests") && strings.HasSuffix(r.URL.Path, "/diffs"):
			iid := iidFromPath(r.URL.Path, "/diffs")
			if _, err := fmt.Fprintf(w, `[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-old\n+new %d\n","new_file":false,"renamed_file":false,"deleted_file":false}]`, iid); err != nil {
				t.Errorf("write diff response: %v", err)
			}
		case strings.Contains(r.URL.Path, "/merge_requests") && strings.HasSuffix(r.URL.Path, "/notes"):
			iid := iidFromPath(r.URL.Path, "/notes")
			if _, err := w.Write(notesJSON(notesByIID[iid])); err != nil {
				t.Errorf("write notes response: %v", err)
			}
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			data, err := json.Marshal(mrs)
			if err != nil {
				t.Fatalf("marshal mrs: %v", err)
			}
			if _, err := w.Write(data); err != nil {
				t.Errorf("write mrs response: %v", err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// iidFromPath pulls the merge_requests/<iid>/<suffix> segment out of a path
// like /projects/3/merge_requests/11/notes.
func iidFromPath(path, suffix string) int {
	trimmed := strings.TrimSuffix(path, suffix)
	parts := strings.Split(trimmed, "/")
	var iid int
	if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &iid); err != nil {
		return 0
	}
	return iid
}

func caseDirName(project, iid int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("gitlab:%d:%d", project, iid)))
	return hex.EncodeToString(sum[:])[:12]
}

func TestReplayImportGitLab(t *testing.T) {
	const project = 3
	mrs := []fakeGitLabMR{
		{IID: 10, Title: "Fix bug", Labels: nil},
		{IID: 11, Title: "Risky change", Labels: []string{"releaseguard:false-positive"}},
		{IID: 12, Title: "Undocumented MR", Labels: nil},
	}
	notes := map[int][]string{
		10: {"## ReleaseGuard recommendation: HOLD\n> details"},
		11: {"## ReleaseGuard recommendation: HOLD\n> details"},
		// 12 has no ReleaseGuard note at all.
	}
	srv := newFakeGitLab(t, mrs, notes)
	out := t.TempDir()

	summary, err := runReplayImport([]string{
		"--source", "gitlab",
		"--project", fmt.Sprintf("%d", project),
		"--since", "2020-01-01T00:00:00Z",
		"--out", out,
	}, envOverride{apiBase: srv.URL, token: "fake"})
	if err != nil {
		t.Fatalf("runReplayImport() error = %v", err)
	}
	if summary.Imported != 2 {
		t.Errorf("Imported = %d, want 2", summary.Imported)
	}
	if summary.SkippedUnlabeled != 1 {
		t.Errorf("SkippedUnlabeled = %d, want 1", summary.SkippedUnlabeled)
	}
	if summary.Failed != 0 {
		t.Errorf("Failed = %d, want 0", summary.Failed)
	}

	// (a) HOLD note, no label -> expected HOLD, source gitlab-comment.
	caseA := caseDirName(project, 10)
	expA := readExpectedJSON(t, filepath.Join(out, caseA, "expected.json"))
	if expA.Recommendation != "HOLD" || expA.Source != "gitlab-comment" {
		t.Errorf("case a expected = %+v, want HOLD/gitlab-comment", expA)
	}

	// (b) HOLD note + fp label -> expected PROCEED, source gitlab-comment+fp-label.
	caseB := caseDirName(project, 11)
	expB := readExpectedJSON(t, filepath.Join(out, caseB, "expected.json"))
	if expB.Recommendation != "PROCEED" || expB.Source != "gitlab-comment+fp-label" {
		t.Errorf("case b expected = %+v, want PROCEED/gitlab-comment+fp-label", expB)
	}

	// (c) no note, no --allow-unlabeled -> skipped, no dir written.
	caseC := caseDirName(project, 12)
	if _, err := os.Stat(filepath.Join(out, caseC)); !os.IsNotExist(err) {
		t.Errorf("case c dir should not exist, stat err = %v", err)
	}

	// (d) case dir name matches the sha1 prefix; diff.json carries no identity
	// fields; .manifest.json exists.
	diffBytes, err := os.ReadFile(filepath.Join(out, caseA, "diff.json"))
	if err != nil {
		t.Fatalf("read diff.json: %v", err)
	}
	for _, forbidden := range []string{"title", "author", "web_url", "Fix bug"} {
		if strings.Contains(string(diffBytes), forbidden) {
			t.Errorf("diff.json contains identity field %q: %s", forbidden, diffBytes)
		}
	}
	manifestBytes, err := os.ReadFile(filepath.Join(out, ".manifest.json"))
	if err != nil {
		t.Fatalf("read .manifest.json: %v", err)
	}
	var manifest map[string]struct {
		Source  string `json:"source"`
		Project int    `json:"project"`
		IID     int    `json:"iid"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	entry, ok := manifest[caseA]
	if !ok || entry.Project != project || entry.IID != 10 || entry.Source != "gitlab" {
		t.Errorf("manifest[%q] = %+v, ok=%t; want project=%d iid=10 source=gitlab", caseA, entry, ok, project)
	}
}

func TestReplayImportAllowUnlabeled(t *testing.T) {
	const project = 3
	mrs := []fakeGitLabMR{{IID: 20, Title: "No comment yet", Labels: nil}}
	srv := newFakeGitLab(t, mrs, nil)
	out := t.TempDir()

	summary, err := runReplayImport([]string{
		"--source", "gitlab",
		"--project", fmt.Sprintf("%d", project),
		"--since", "2020-01-01T00:00:00Z",
		"--out", out,
		"--allow-unlabeled",
	}, envOverride{apiBase: srv.URL, token: "fake"})
	if err != nil {
		t.Fatalf("runReplayImport() error = %v", err)
	}
	if summary.Imported != 1 {
		t.Errorf("Imported = %d, want 1", summary.Imported)
	}
	caseDir := caseDirName(project, 20)
	exp := readExpectedJSON(t, filepath.Join(out, caseDir, "expected.json"))
	if exp.Recommendation != "" || !exp.NeedsLabel || exp.Source != "unlabeled" {
		t.Errorf("expected = %+v, want empty/needs_label=true/unlabeled", exp)
	}
}

func TestReplayImportRejectsUnknownSource(t *testing.T) {
	out := t.TempDir()
	_, err := runReplayImport([]string{
		"--source", "github",
		"--project", "1",
		"--since", "2020-01-01T00:00:00Z",
		"--out", out,
	}, envOverride{apiBase: "http://unused", token: "fake"})
	if err == nil {
		t.Fatal("expected error for unsupported --source")
	}
}

func readExpectedJSON(t *testing.T, path string) replayExpected {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var exp replayExpected
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return exp
}

// TestRunReplayCasesUnlabeled is test (e): a dataset with one needs_label
// case reports Unlabeled=1 and computes metrics over the rest only. It
// reuses the checked-in 01-proceed fixture as its one real case.
func TestRunReplayCasesUnlabeled(t *testing.T) {
	t.Chdir("../..")

	dir := t.TempDir()
	copyReplayCase(t, "testdata/replay/01-proceed", filepath.Join(dir, "01-proceed"))

	needsLabelDir := filepath.Join(dir, "02-needs-label")
	if err := os.MkdirAll(needsLabelDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	copyFile(t, "testdata/replay/01-proceed/diff.json", filepath.Join(needsLabelDir, "diff.json"))
	if err := os.WriteFile(filepath.Join(needsLabelDir, "expected.json"),
		[]byte(`{"recommendation":"","needs_label":true,"source":"unlabeled"}`), 0644); err != nil {
		t.Fatalf("write expected.json: %v", err)
	}

	metrics, err := runReplayCases(dir, 60)
	if err != nil {
		t.Fatalf("runReplayCases() error = %v", err)
	}
	if metrics.Unlabeled != 1 {
		t.Errorf("Unlabeled = %d, want 1", metrics.Unlabeled)
	}
	if metrics.Cases != 1 {
		t.Errorf("Cases = %d, want 1 (needs_label case excluded)", metrics.Cases)
	}
	if len(metrics.PerCase) != 1 {
		t.Errorf("len(PerCase) = %d, want 1", len(metrics.PerCase))
	}
}

func copyReplayCase(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	copyFile(t, filepath.Join(src, "diff.json"), filepath.Join(dst, "diff.json"))
	copyFile(t, filepath.Join(src, "expected.json"), filepath.Join(dst, "expected.json"))
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}
