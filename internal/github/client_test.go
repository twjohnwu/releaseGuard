package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewUsesDefaultAPIBaseAndTimeout(t *testing.T) {
	client := New("", "")
	if client.base != "https://api.github.com" {
		t.Errorf("base = %q", client.base)
	}
	if client.http.Timeout != 30*time.Second {
		t.Errorf("timeout = %s", client.http.Timeout)
	}
}

func TestListMergedPRsFiltersByMergeStatusAndCutoff(t *testing.T) {
	var sawAuthorization bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/widgets/pulls" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got, ok := r.Header["Authorization"]; ok {
			sawAuthorization = true
			t.Errorf("Authorization = %q, want no header", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version = %q", got)
		}
		if got := r.URL.Query().Get("state"); got != "closed" {
			t.Errorf("state = %q", got)
		}
		if got := r.URL.Query().Get("sort"); got != "updated" {
			t.Errorf("sort = %q", got)
		}
		if got := r.URL.Query().Get("direction"); got != "desc" {
			t.Errorf("direction = %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		var body []byte
		switch r.URL.Query().Get("page") {
		case "1":
			body = []byte(`[
				{"number":11,"merged_at":"2026-08-20T10:00:00Z"},
				{"number":12,"merged_at":null},
				{"number":13,"merged_at":"2026-07-01T10:00:00Z"}
			]`)
		default:
			body = []byte(`[]`)
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	client := New(srv.URL, "")
	prs, err := client.ListMergedPRs("acme", "widgets", "2026-08-01T00:00:00Z", 3, 4)
	if err != nil {
		t.Fatalf("ListMergedPRs() error = %v", err)
	}
	if sawAuthorization {
		t.Fatal("unauthenticated client sent Authorization header")
	}
	if len(prs) != 1 || prs[0].Number != 11 || prs[0].MergedAt != "2026-08-20T10:00:00Z" {
		t.Fatalf("ListMergedPRs() = %+v, want PR 11", prs)
	}
}

func TestGetPRFilesMapsRenameAndBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widgets/pulls/42/files" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			if _, err := w.Write([]byte(`[
				{"filename":"new/name.go","previous_filename":"old/name.go","patch":"@@ -1 +1 @@\n-old\n+new","status":"renamed"},
				{"filename":"assets/logo.png","status":"modified"}
			]`)); err != nil {
				t.Errorf("write response: %v", err)
			}
			return
		}
		if _, err := w.Write([]byte(`[]`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	files, err := New(srv.URL, "secret-token").GetPRFiles("acme", "widgets", 42)
	if err != nil {
		t.Fatalf("GetPRFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2: %+v", len(files), files)
	}
	if files[0].OldPath != "old/name.go" || files[0].NewPath != "new/name.go" || !files[0].RenamedFile {
		t.Errorf("renamed file = %+v", files[0])
	}
	if files[0].NewFile || files[0].DeletedFile {
		t.Errorf("renamed file has conflicting flags: %+v", files[0])
	}
	if files[1].OldPath != "assets/logo.png" || files[1].NewPath != "assets/logo.png" || files[1].Diff != "" {
		t.Errorf("binary file = %+v", files[1])
	}
}

func TestRateLimitErrorMentionsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "API rate limit exceeded", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	_, err := New(srv.URL, "").ListMergedPRs("acme", "widgets", "", 20, 1)
	if err == nil {
		t.Fatal("ListMergedPRs() error = nil, want rate-limit error")
	}
	if !strings.Contains(err.Error(), "rate limit") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %q, want rate limit and GITHUB_TOKEN", err)
	}
}

func TestListMergedPRsRespectsMaxPages(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		number, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Errorf("invalid page: %v", err)
		}
		if _, err := fmt.Fprintf(w, `[{"number":%d,"merged_at":"2026-08-20T10:00:00Z"}]`, number); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	prs, err := New(srv.URL, "token").ListMergedPRs("acme", "widgets", "", 1, 2)
	if err != nil {
		t.Fatalf("ListMergedPRs() error = %v", err)
	}
	if pages != 2 || len(prs) != 2 {
		t.Fatalf("pages = %d, prs = %+v; want two pages", pages, prs)
	}
}
