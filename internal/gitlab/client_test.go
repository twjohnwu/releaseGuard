package gitlab

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAddsAuthHeader(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret-token")
	if _, err := c.do("GET", "/test", nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got != "secret-token" {
		t.Fatalf("expected token header, got %q", got)
	}
}

func TestGetMRDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/123/merge_requests/45/diffs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[
			{"old_path":"foo.go","new_path":"foo.go","diff":"@@ -1,3 +1,3 @@\n-old\n+new","new_file":false,"renamed_file":false,"deleted_file":false}
		]`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	files, err := c.GetMRDiff(123, 45)
	if err != nil {
		t.Fatalf("get diff: %v", err)
	}
	if len(files) != 1 || files[0].NewPath != "foo.go" {
		t.Fatalf("unexpected: %+v", files)
	}
}

func TestPostMRNote(t *testing.T) {
	receivedBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/projects/123/merge_requests/45/notes" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		receivedBody = string(b)
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write([]byte(`{"id": 999}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	if err := c.PostMRNote(123, 45, "hello world"); err != nil {
		t.Fatalf("post: %v", err)
	}
	if !strings.Contains(receivedBody, "hello world") {
		t.Fatalf("body lost: %s", receivedBody)
	}
}
