// Mock GitLab API server for local end-to-end testing of releaseguard analyzer.
// Implements only the endpoints analyzer hits:
//   GET  /api/v4/projects/:id/merge_requests/:iid/diffs   → fixture JSON
//   POST /api/v4/projects/:id/merge_requests/:iid/notes   → 201 + log body to /artifacts/notes.log
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultPort       = "8080"
	defaultArtifacts  = "/artifacts"
	defaultFixtureDir = "/fixtures"
)

var (
	diffPathRe = regexp.MustCompile(`^/api/v4/projects/(\d+)/merge_requests/(\d+)/diffs/?$`)
	notePathRe = regexp.MustCompile(`^/api/v4/projects/(\d+)/merge_requests/(\d+)/notes/?$`)
)

func fixtureForProject(projectID string) string {
	switch projectID {
	case "1":
		return "diff-proceed.json"
	case "2":
		return "diff-review.json"
	case "3":
		return "diff-hold.json"
	case "4":
		return "diff-t0demo.json"
	default:
		return "diff-review.json"
	}
}

func main() {
	port := envOr("MOCK_PORT", defaultPort)
	fixtureDir := envOr("MOCK_FIXTURE_DIR", defaultFixtureDir)
	artifactsDir := envOr("MOCK_ARTIFACTS_DIR", defaultArtifacts)

	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		log.Fatalf("mkdir artifacts: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && diffPathRe.MatchString(r.URL.Path):
			handleDiff(w, r, fixtureDir)
		case r.Method == http.MethodPost && notePathRe.MatchString(r.URL.Path):
			handleNote(w, r, artifactsDir)
		default:
			log.Printf("[404] %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})

	log.Printf("mock-gitlab listening on :%s", port)
	log.Printf("  fixtures dir: %s", fixtureDir)
	log.Printf("  artifacts dir: %s", artifactsDir)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func handleDiff(w http.ResponseWriter, r *http.Request, fixtureDir string) {
	tok := r.Header.Get("PRIVATE-TOKEN")
	if tok == "" {
		http.Error(w, "missing PRIVATE-TOKEN", http.StatusUnauthorized)
		return
	}
	projectID := ""
	if m := diffPathRe.FindStringSubmatch(r.URL.Path); len(m) >= 2 {
		projectID = m[1]
	}
	fixture := fixtureForProject(projectID)
	body, err := os.ReadFile(filepath.Join(fixtureDir, fixture))
	if err != nil {
		log.Printf("[500] read fixture %s: %v", fixture, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[200] %s %s -> served %s (%d bytes)", r.Method, r.URL.Path, fixture, len(body))
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func handleNote(w http.ResponseWriter, r *http.Request, artifactsDir string) {
	tok := r.Header.Get("PRIVATE-TOKEN")
	if tok == "" {
		http.Error(w, "missing PRIVATE-TOKEN", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var payload struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(body, &payload)

	projectID, mrIID := "", ""
	if m := notePathRe.FindStringSubmatch(r.URL.Path); len(m) >= 3 {
		projectID, mrIID = m[1], m[2]
	}

	stamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	logPath := filepath.Join(artifactsDir, "notes.log")
	stampSafe := strings.ReplaceAll(stamp, ":", "-")
	mdPath := filepath.Join(artifactsDir, fmt.Sprintf("note-proj%s-mr%s-%s.md", projectID, mrIID, stampSafe))

	header := fmt.Sprintf("=== %s proj=%s mr=%s %s %s ===\n", stamp, projectID, mrIID, r.Method, r.URL.Path)
	logEntry := header + payload.Body + "\n\n"
	if err := appendFile(logPath, logEntry); err != nil {
		log.Printf("[500] write log: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(mdPath, []byte(payload.Body), 0644); err != nil {
		log.Printf("[500] write md: %v", err)
	}

	log.Printf("[201] %s %s -> note saved (%d bytes md, snippet: %q)",
		r.Method, r.URL.Path, len(payload.Body), truncate(payload.Body, 80))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"id":1}`))
}

func appendFile(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
