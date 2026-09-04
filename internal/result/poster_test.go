package result

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twjohnwu/releaseGuard/internal/gitlab"
)

func TestPosterPostsCommentAndWritesArtifact(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		got = string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := gitlab.NewClient(srv.URL, "tok")

	tmp := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	p := NewPoster(c, 123, 45, 0.85)
	err = p.Post(context.Background(), "## hello\n", []string{"TestA", "TestB"}, true)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("comment body lost: %s", got)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "selective-tests.txt"))
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if !strings.Contains(string(b), "TestA|TestB") {
		t.Fatalf("artifact content: %s", b)
	}
}
