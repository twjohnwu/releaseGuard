package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/config"
	"github.com/twjohnwu/releaseGuard/internal/gitlab"
	"github.com/twjohnwu/releaseGuard/internal/report"
)

type envOverride struct {
	apiBase string
	token   string
}

type importSummary struct {
	Imported         int
	SkippedUnlabeled int
	Failed           int
}

type importCase struct {
	IID    int
	Labels []string
}

type caseSource interface {
	Name() string
	ListMergedCases(projectID int, since string, perPage, maxPages int) ([]importCase, error)
	GetDiff(projectID, iid int) ([]gitlab.DiffFile, error)
	GetNotes(projectID, iid int) ([]string, error)
}

type gitLabCaseSource struct {
	client *gitlab.Client
}

func (s gitLabCaseSource) Name() string {
	return "gitlab"
}

func (s gitLabCaseSource) ListMergedCases(projectID int, since string, perPage, maxPages int) ([]importCase, error) {
	mrs, err := s.client.ListMergedMRs(projectID, since, perPage, maxPages)
	if err != nil {
		return nil, err
	}
	cases := make([]importCase, 0, len(mrs))
	for _, mr := range mrs {
		cases = append(cases, importCase{IID: mr.IID, Labels: mr.Labels})
	}
	return cases, nil
}

func (s gitLabCaseSource) GetDiff(projectID, iid int) ([]gitlab.DiffFile, error) {
	return s.client.GetMRDiff(projectID, iid)
}

func (s gitLabCaseSource) GetNotes(projectID, iid int) ([]string, error) {
	return s.client.GetMRNotes(projectID, iid)
}

type replayImportManifestEntry struct {
	Source  string `json:"source"`
	Project int    `json:"project"`
	IID     int    `json:"iid"`
}

func runReplayImport(args []string, override envOverride) (importSummary, error) {
	fs := flag.NewFlagSet("replay-import", flag.ContinueOnError)
	sourceName := fs.String("source", "gitlab", "case source")
	projectID := fs.Int("project", 0, "GitLab project ID")
	since := fs.String("since", "", "only import MRs updated after this RFC3339 timestamp")
	perPage := fs.Int("per-page", 20, "MRs per API page")
	maxPages := fs.Int("max-pages", 5, "maximum API pages to scan")
	outDir := fs.String("out", ".replay", "output replay dataset directory")
	allowUnlabeled := fs.Bool("allow-unlabeled", false, "import MRs without a ReleaseGuard decision")
	if err := fs.Parse(args); err != nil {
		return importSummary{}, err
	}

	if *sourceName != "gitlab" {
		return importSummary{}, fmt.Errorf("unsupported replay import source %q", *sourceName)
	}
	if *projectID <= 0 {
		return importSummary{}, fmt.Errorf("project id required: pass --project <id>")
	}
	if strings.TrimSpace(*since) != "" {
		if _, err := time.Parse(time.RFC3339, *since); err != nil {
			return importSummary{}, fmt.Errorf("since must be RFC3339: %w", err)
		}
	}

	gitLabConfig := config.GitLabConfig{APIBase: override.apiBase, Token: override.token}
	if override.apiBase == "" && override.token == "" {
		var err error
		gitLabConfig, err = config.LoadGitLab()
		if err != nil {
			return importSummary{}, err
		}
	}

	var source caseSource = gitLabCaseSource{
		client: gitlab.NewClient(gitLabConfig.APIBase, gitLabConfig.Token),
	}
	cases, err := source.ListMergedCases(*projectID, *since, *perPage, *maxPages)
	if err != nil {
		return importSummary{}, err
	}
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return importSummary{}, fmt.Errorf("create replay output directory: %w", err)
	}

	summary := importSummary{}
	manifest := make(map[string]replayImportManifestEntry)
	for _, replayCase := range cases {
		diff, err := source.GetDiff(*projectID, replayCase.IID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "replay-import: %s project %d MR !%d diff: %v\n", source.Name(), *projectID, replayCase.IID, err)
			summary.Failed++
			continue
		}
		notes, err := source.GetNotes(*projectID, replayCase.IID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "replay-import: %s project %d MR !%d notes: %v\n", source.Name(), *projectID, replayCase.IID, err)
			summary.Failed++
			continue
		}

		decision := parseDecision(notes)
		if decision == "" && !*allowUnlabeled {
			summary.SkippedUnlabeled++
			continue
		}

		expected := replayExpected{Recommendation: decision, Source: "gitlab-comment"}
		if decision == "" {
			expected.NeedsLabel = true
			expected.Source = "unlabeled"
		} else if (decision == "HOLD" || decision == "REVIEW") && hasLabel(replayCase.Labels, report.FalsePositiveLabel) {
			expected.Recommendation = "PROCEED"
			expected.Source = "gitlab-comment+fp-label"
		}

		caseName := replayImportCaseName(source.Name(), *projectID, replayCase.IID)
		caseDir := filepath.Join(*outDir, caseName)
		if err := os.MkdirAll(caseDir, 0755); err != nil {
			return summary, fmt.Errorf("create replay case directory %q: %w", caseName, err)
		}
		if err := writeReplayImportJSON(filepath.Join(caseDir, "diff.json"), diff); err != nil {
			return summary, fmt.Errorf("write case %q diff: %w", caseName, err)
		}
		if err := writeReplayImportJSON(filepath.Join(caseDir, "expected.json"), expected); err != nil {
			return summary, fmt.Errorf("write case %q expected recommendation: %w", caseName, err)
		}

		manifest[caseName] = replayImportManifestEntry{
			Source:  source.Name(),
			Project: *projectID,
			IID:     replayCase.IID,
		}
		summary.Imported++
	}

	if err := writeReplayImportJSON(filepath.Join(*outDir, ".manifest.json"), manifest); err != nil {
		return summary, fmt.Errorf("write replay manifest: %w", err)
	}
	fmt.Printf("imported: %d, skipped-unlabeled: %d, failed: %d\n", summary.Imported, summary.SkippedUnlabeled, summary.Failed)
	return summary, nil
}

func replayImportCaseName(source string, projectID, iid int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s:%d:%d", source, projectID, iid)))
	return hex.EncodeToString(sum[:])[:12]
}

func writeReplayImportJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
