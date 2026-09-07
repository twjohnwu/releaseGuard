package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/report"
)

// fakeGitLab serves the three endpoints replay-import needs, in the same
// plain-path style e2e_test.go uses (no /api/v4 prefix baked into the
// mux — that lives in GITLAB_API_BASE).
type fakeGitLabMR struct {
	IID    int      `json:"iid"`
	Title  string   `json:"title"`
	Labels []string `json:"labels"`
}

type replayImportRoundTripFunc func(*http.Request) (*http.Response, error)

func (f replayImportRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
			bodies := append([]string(nil), notesByIID[iid]...)
			if r.URL.Query().Get("sort") != "desc" {
				for left, right := 0, len(bodies)-1; left < right; left, right = left+1, right-1 {
					bodies[left], bodies[right] = bodies[right], bodies[left]
				}
			}
			if _, err := w.Write(notesJSON(bodies)); err != nil {
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
	if expA.Label == nil || expA.Label.HumanOutcome != report.HumanOutcomeUnlabeled {
		t.Errorf("case a label = %+v, want unlabeled", expA.Label)
	}

	// (b) HOLD note + fp label -> expected PROCEED, source gitlab-comment+fp-label.
	caseB := caseDirName(project, 11)
	expB := readExpectedJSON(t, filepath.Join(out, caseB, "expected.json"))
	if expB.Recommendation != "PROCEED" || expB.Source != "gitlab-comment+fp-label" {
		t.Errorf("case b expected = %+v, want PROCEED/gitlab-comment+fp-label", expB)
	}
	if expB.Label == nil || expB.Label.HumanOutcome != report.HumanOutcomeOvercautious || expB.Label.Derivation != report.DerivationInferred {
		t.Errorf("case b label = %+v, want inferred overcautious", expB.Label)
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
	expPath := filepath.Join(out, caseDir, "expected.json")
	exp := readExpectedJSON(t, expPath)
	if exp.Recommendation != "" || exp.NeedsLabel || exp.Source != "unlabeled" {
		t.Errorf("expected = %+v, want empty/needs_label=false/unlabeled", exp)
	}
	if exp.Label == nil || exp.Label.HumanOutcome != report.HumanOutcomeUnlabeled {
		t.Errorf("label = %+v, want unlabeled", exp.Label)
	}
	raw, err := os.ReadFile(expPath)
	if err != nil {
		t.Fatalf("read expected.json: %v", err)
	}
	if strings.Contains(string(raw), "needs_label") {
		t.Errorf("expected.json should not contain needs_label field: %s", raw)
	}
}

func TestReplayImportPreservesHandLabelUnlessForced(t *testing.T) {
	const project = 3
	const iid = 30
	srv := newFakeGitLab(t,
		[]fakeGitLabMR{{IID: iid, Title: "Risky change", Labels: nil}},
		map[int][]string{iid: {"## ReleaseGuard recommendation: HOLD"}},
	)
	out := t.TempDir()
	args := []string{
		"--source", "gitlab",
		"--project", fmt.Sprintf("%d", project),
		"--since", "2020-01-01T00:00:00Z",
		"--out", out,
	}
	if _, err := runReplayImport(args, envOverride{apiBase: srv.URL, token: "fake"}); err != nil {
		t.Fatalf("initial runReplayImport() error = %v", err)
	}

	expectedPath := filepath.Join(out, caseDirName(project, iid), "expected.json")
	fresh, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read fresh expected.json: %v", err)
	}
	manual := []byte(`{"recommendation":"HOLD","source":"manual","label":{"human_outcome":"correct","evidence_source":"release_decision","derivation":"explicit","confidence":"high"}}`)
	if err := os.WriteFile(expectedPath, manual, 0644); err != nil {
		t.Fatalf("write manual expected.json: %v", err)
	}

	summary, err := runReplayImport(args, envOverride{apiBase: srv.URL, token: "fake"})
	if err != nil {
		t.Fatalf("preserving runReplayImport() error = %v", err)
	}
	if summary.Imported != 1 || summary.PreservedLabels != 1 {
		t.Errorf("summary = %+v, want Imported=1 PreservedLabels=1", summary)
	}
	preserved, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read preserved expected.json: %v", err)
	}
	if string(preserved) != string(manual) {
		t.Fatalf("expected.json = %q, want byte-identical %q", preserved, manual)
	}

	forcedSummary, err := runReplayImport(append(args, "--force"), envOverride{apiBase: srv.URL, token: "fake"})
	if err != nil {
		t.Fatalf("forced runReplayImport() error = %v", err)
	}
	if forcedSummary.PreservedLabels != 0 {
		t.Errorf("forced PreservedLabels = %d, want 0", forcedSummary.PreservedLabels)
	}
	forcedData, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read forced expected.json: %v", err)
	}
	if string(forcedData) != string(fresh) {
		t.Errorf("forced expected.json = %q, want freshly generated %q", forcedData, fresh)
	}
	forced := readExpectedJSON(t, expectedPath)
	if forced.Recommendation != "HOLD" || forced.NeedsLabel || forced.Source != "gitlab-comment" {
		t.Errorf("forced expected = %+v, want HOLD/needs_label=false/gitlab-comment", forced)
	}

	inferred := []byte(`{"recommendation":"HOLD","source":"manual","label":{"human_outcome":"correct","evidence_source":"release_decision","derivation":"inferred","confidence":"high"}}`)
	if err := os.WriteFile(expectedPath, inferred, 0644); err != nil {
		t.Fatalf("write inferred expected.json: %v", err)
	}
	reimportedSummary, err := runReplayImport(args, envOverride{apiBase: srv.URL, token: "fake"})
	if err != nil {
		t.Fatalf("reimporting runReplayImport() error = %v", err)
	}
	if reimportedSummary.PreservedLabels != 0 {
		t.Errorf("reimported PreservedLabels = %d, want 0", reimportedSummary.PreservedLabels)
	}
	reimportedData, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read reimported expected.json: %v", err)
	}
	if string(reimportedData) != string(fresh) {
		t.Errorf("reimported expected.json = %q, want freshly generated %q", reimportedData, fresh)
	}
}

func TestReplayImportSampleIsDeterministicAndStratified(t *testing.T) {
	const project = 7
	mrs := []fakeGitLabMR{
		{IID: 1}, {IID: 2}, {IID: 3}, {IID: 4},
		{IID: 5},
		{IID: 6}, {IID: 7}, {IID: 8},
	}
	notes := map[int][]string{
		1: {"## ReleaseGuard recommendation: HOLD"},
		2: {"## ReleaseGuard recommendation: HOLD"},
		3: {"## ReleaseGuard recommendation: HOLD"},
		4: {"## ReleaseGuard recommendation: HOLD"},
		5: {"## ReleaseGuard recommendation: REVIEW"},
		6: {"## ReleaseGuard recommendation: PROCEED"},
		7: {"## ReleaseGuard recommendation: PROCEED"},
		8: {"## ReleaseGuard recommendation: PROCEED"},
	}
	mrsJSON, err := json.Marshal(mrs)
	if err != nil {
		t.Fatalf("marshal MRs: %v", err)
	}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = replayImportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := []byte(nil)
		switch {
		case strings.HasSuffix(req.URL.Path, "/diffs"):
			body = []byte(`[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-old\n+new\n","new_file":false,"renamed_file":false,"deleted_file":false}]`)
		case strings.HasSuffix(req.URL.Path, "/notes"):
			iid := iidFromPath(req.URL.Path, "/notes")
			type note struct {
				Body string `json:"body"`
			}
			items := make([]note, 0, len(notes[iid]))
			for _, value := range notes[iid] {
				items = append(items, note{Body: value})
			}
			body, err = json.Marshal(items)
			if err != nil {
				return nil, err
			}
		case strings.HasSuffix(req.URL.Path, "/merge_requests"):
			body = mrsJSON
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	run := func(out string) {
		t.Helper()
		_, err := runReplayImport([]string{
			"--source", "gitlab",
			"--project", fmt.Sprintf("%d", project),
			"--since", "2020-01-01T00:00:00Z",
			"--out", out,
			"--sample", "6",
			"--seed", "42",
		}, envOverride{apiBase: "http://replay-import.test", token: "fake"})
		if err != nil {
			t.Fatalf("runReplayImport() error = %v", err)
		}
	}
	caseNames := func(out string) []string {
		t.Helper()
		entries, err := os.ReadDir(out)
		if err != nil {
			t.Fatalf("read output directory: %v", err)
		}
		var names []string
		for _, entry := range entries {
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		return names
	}

	outA, outB := t.TempDir(), t.TempDir()
	run(outA)
	run(outB)
	run(outA)
	namesA, namesB := caseNames(outA), caseNames(outB)
	if !reflect.DeepEqual(namesA, namesB) {
		t.Fatalf("same seed selected different cases: %v vs %v", namesA, namesB)
	}
	if len(namesA) != 5 {
		t.Fatalf("selected case count = %d, want 5", len(namesA))
	}

	decisionCounts := map[string]int{}
	for _, name := range namesA {
		expected := readExpectedJSON(t, filepath.Join(outA, name, "expected.json"))
		decisionCounts[expected.Recommendation]++
	}
	wantCounts := map[string]int{"HOLD": 2, "REVIEW": 1, "PROCEED": 2}
	if !reflect.DeepEqual(decisionCounts, wantCounts) {
		t.Errorf("selected decision counts = %v, want %v", decisionCounts, wantCounts)
	}

	manifestData, err := os.ReadFile(filepath.Join(outA, ".manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	var sampleManifest struct {
		N            int            `json:"n"`
		Seed         int64          `json:"seed"`
		StrataCounts map[string]int `json:"strata_counts"`
	}
	if err := json.Unmarshal(manifest["sample"], &sampleManifest); err != nil {
		t.Fatalf("unmarshal sample manifest: %v", err)
	}
	if sampleManifest.N != 6 || sampleManifest.Seed != 42 || !reflect.DeepEqual(sampleManifest.StrataCounts, wantCounts) {
		t.Errorf("sample manifest = %+v, want n=6 seed=42 counts=%v", sampleManifest, wantCounts)
	}
}

func TestReplayImportNewestNoteWins(t *testing.T) {
	const project = 3
	mrs := []fakeGitLabMR{{IID: 21, Title: "Changed verdict", Labels: nil}}
	notes := map[int][]string{
		21: {
			"## ReleaseGuard recommendation: PROCEED",
			"## ReleaseGuard recommendation: HOLD",
		},
	}
	srv := newFakeGitLab(t, mrs, notes)
	out := t.TempDir()

	_, err := runReplayImport([]string{
		"--source", "gitlab",
		"--project", fmt.Sprintf("%d", project),
		"--since", "2020-01-01T00:00:00Z",
		"--out", out,
		"--allow-unlabeled",
	}, envOverride{apiBase: srv.URL, token: "fake"})
	if err != nil {
		t.Fatalf("runReplayImport() error = %v", err)
	}

	expected := readExpectedJSON(t, filepath.Join(out, caseDirName(project, 21), "expected.json"))
	if expected.Recommendation != "PROCEED" || expected.Source != "gitlab-comment" {
		t.Errorf("expected = %+v, want PROCEED/gitlab-comment", expected)
	}
}

func TestReplayImportMergesManifestAcrossRuns(t *testing.T) {
	out := t.TempDir()
	run := func(project int, mrs []fakeGitLabMR, notes map[int][]string) {
		t.Helper()
		srv := newFakeGitLab(t, mrs, notes)
		_, err := runReplayImport([]string{
			"--source", "gitlab",
			"--project", fmt.Sprintf("%d", project),
			"--since", "2020-01-01T00:00:00Z",
			"--out", out,
		}, envOverride{apiBase: srv.URL, token: "fake"})
		if err != nil {
			t.Fatalf("runReplayImport(project=%d) error = %v", project, err)
		}
	}
	readManifest := func() map[string]replayImportManifestEntry {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(out, ".manifest.json"))
		if err != nil {
			t.Fatalf("read .manifest.json: %v", err)
		}
		var manifest map[string]replayImportManifestEntry
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("unmarshal .manifest.json: %v", err)
		}
		return manifest
	}
	assertEntries := func(manifest map[string]replayImportManifestEntry) {
		t.Helper()
		want := []struct {
			project int
			iid     int
		}{
			{project: 3, iid: 1},
			{project: 4, iid: 2},
		}
		for _, item := range want {
			key := caseDirName(item.project, item.iid)
			entry, ok := manifest[key]
			if !ok || entry.Project != item.project || entry.IID != item.iid {
				t.Errorf("manifest[%q] = %+v, ok=%t; want project=%d iid=%d", key, entry, ok, item.project, item.iid)
			}
		}
	}

	run(3,
		[]fakeGitLabMR{{IID: 1, Title: "First", Labels: nil}},
		map[int][]string{1: {"## ReleaseGuard recommendation: HOLD"}},
	)
	run(4,
		[]fakeGitLabMR{{IID: 2, Title: "Second", Labels: nil}},
		map[int][]string{2: {"## ReleaseGuard recommendation: HOLD"}},
	)
	assertEntries(readManifest())

	run(4, []fakeGitLabMR{}, nil)
	assertEntries(readManifest())
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
