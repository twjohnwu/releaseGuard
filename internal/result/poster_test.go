package result

import (
	"context"
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
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		got = string(buf[:n])
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := gitlab.NewClient(srv.URL, "tok")

	tmp := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(old)

	p := NewPoster(c, 123, 45, 0.85)
	err := p.Post(context.Background(), "## hello\n", []string{"TestA", "TestB"}, true)
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
