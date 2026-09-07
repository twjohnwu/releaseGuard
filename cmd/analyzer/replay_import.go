package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/config"
	"github.com/twjohnwu/releaseGuard/internal/github"
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
	PreservedLabels  int
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

type gitHubCaseSource struct {
	owner  string
	repo   string
	client *github.Client
}

func (s gitHubCaseSource) Name() string {
	return "github"
}

func (s gitHubCaseSource) ListMergedCases(_ int, since string, perPage, maxPages int) ([]importCase, error) {
	prs, err := s.client.ListMergedPRs(s.owner, s.repo, since, perPage, maxPages)
	if err != nil {
		return nil, err
	}
	cases := make([]importCase, 0, len(prs))
	for _, pr := range prs {
		cases = append(cases, importCase{IID: pr.Number, Labels: nil})
	}
	return cases, nil
}

func (s gitHubCaseSource) GetDiff(_, iid int) ([]gitlab.DiffFile, error) {
	return s.client.GetPRFiles(s.owner, s.repo, iid)
}

func (s gitHubCaseSource) GetNotes(_, _ int) ([]string, error) {
	return nil, nil
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
	Repo    string `json:"repo,omitempty"`
}

func runReplayImport(args []string, override envOverride) (importSummary, error) {
	fs := flag.NewFlagSet("replay-import", flag.ContinueOnError)
	sourceName := fs.String("source", "gitlab", "case source")
	projectID := fs.Int("project", 0, "GitLab project ID")
	repoFlag := fs.String("repo", "", "GitHub owner/repo (source=github only)")
	since := fs.String("since", "", "only import MRs updated after this RFC3339 timestamp")
	perPage := fs.Int("per-page", 20, "MRs per API page")
	maxPages := fs.Int("max-pages", 5, "maximum API pages to scan")
	outDir := fs.String("out", ".replay", "output replay dataset directory")
	allowUnlabeled := fs.Bool("allow-unlabeled", false, "import MRs without a ReleaseGuard decision")
	force := fs.Bool("force", false, "overwrite explicit hand-labelled expected.json files too")
	sample := fs.Int("sample", 0, "stratified sample size across HOLD/REVIEW/PROCEED verdicts (0 = import everything)")
	seed := fs.Int64("seed", 1, "random seed for --sample (deterministic)")
	if err := fs.Parse(args); err != nil {
		return importSummary{}, err
	}
	if *sourceName == "github" {
		*allowUnlabeled = true
	}

	if *sourceName != "gitlab" && *sourceName != "github" {
		return importSummary{}, fmt.Errorf("unsupported replay import source %q", *sourceName)
	}
	if *sourceName == "gitlab" && *repoFlag != "" {
		return importSummary{}, fmt.Errorf("--repo is only valid with --source github")
	}
	if *sourceName == "github" && *projectID > 0 {
		return importSummary{}, fmt.Errorf("--project is only valid with --source gitlab")
	}
	if *sourceName == "gitlab" && *projectID <= 0 {
		return importSummary{}, fmt.Errorf("project id required: pass --project <id>")
	}
	var owner, repo string
	if *sourceName == "github" {
		parts := strings.Split(*repoFlag, "/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return importSummary{}, fmt.Errorf("--repo required as owner/name for --source github")
		}
		owner, repo = parts[0], parts[1]
	}
	if strings.TrimSpace(*since) != "" {
		if _, err := time.Parse(time.RFC3339, *since); err != nil {
			return importSummary{}, fmt.Errorf("since must be RFC3339: %w", err)
		}
	}

	var source caseSource
	if *sourceName == "gitlab" {
		gitLabConfig := config.GitLabConfig{APIBase: override.apiBase, Token: override.token}
		if override.apiBase == "" && override.token == "" {
			var err error
			gitLabConfig, err = config.LoadGitLab()
			if err != nil {
				return importSummary{}, err
			}
		}
		source = gitLabCaseSource{
			client: gitlab.NewClient(gitLabConfig.APIBase, gitLabConfig.Token),
		}
	} else {
		gitHubConfig := config.GitHubConfig{APIBase: override.apiBase, Token: override.token}
		if override.apiBase == "" && override.token == "" {
			var err error
			gitHubConfig, err = config.LoadGitHub()
			if err != nil {
				return importSummary{}, err
			}
		}
		source = gitHubCaseSource{
			owner:  owner,
			repo:   repo,
			client: github.New(gitHubConfig.APIBase, gitHubConfig.Token),
		}
		fmt.Println("source github: --allow-unlabeled implied (GitHub PRs carry no ReleaseGuard decision)")
	}
	cases, err := source.ListMergedCases(*projectID, *since, *perPage, *maxPages)
	if err != nil {
		return importSummary{}, err
	}
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return importSummary{}, fmt.Errorf("create replay output directory: %w", err)
	}

	summary := importSummary{}
	manifestPath := filepath.Join(*outDir, ".manifest.json")
	manifest := make(map[string]replayImportManifestEntry)
	manifestData, err := os.ReadFile(manifestPath)
	if err == nil {
		var rawManifest map[string]json.RawMessage
		if err := json.Unmarshal(manifestData, &rawManifest); err != nil {
			return summary, fmt.Errorf("unmarshal existing replay manifest: %w", err)
		}
		for key, raw := range rawManifest {
			if key == "sample" {
				continue
			}
			var entry replayImportManifestEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				return summary, fmt.Errorf("unmarshal existing replay manifest: %w", err)
			}
			manifest[key] = entry
		}
	} else if !os.IsNotExist(err) {
		return summary, fmt.Errorf("read existing replay manifest: %w", err)
	}
	manifestRepo := ""
	if source.Name() == "github" {
		manifestRepo = owner + "/" + repo
	}
	type replayImportCandidate struct {
		replayCase importCase
		notes      []string
		decision   string
	}
	candidates := make([]replayImportCandidate, 0, len(cases))
	for _, replayCase := range cases {
		notes, err := source.GetNotes(*projectID, replayCase.IID)
		if err != nil {
			if source.Name() == "github" {
				fmt.Fprintf(os.Stderr, "replay-import: %s repo %s PR #%d notes: %v\n", source.Name(), manifestRepo, replayCase.IID, err)
			} else {
				fmt.Fprintf(os.Stderr, "replay-import: %s project %d MR !%d notes: %v\n", source.Name(), *projectID, replayCase.IID, err)
			}
			summary.Failed++
			continue
		}
		candidates = append(candidates, replayImportCandidate{
			replayCase: replayCase,
			notes:      notes,
			decision:   parseDecision(notes),
		})
	}

	selected := candidates
	strataCounts := make(map[string]int)
	if *sample > 0 {
		groups := make(map[string][]int)
		for i, candidate := range candidates {
			groups[candidate.decision] = append(groups[candidate.decision], i)
		}
		selected = nil
		if len(groups) > 0 {
			perStratum := (*sample + len(groups) - 1) / len(groups)
			keys := make([]string, 0, len(groups))
			for key := range groups {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			rng := mathrand.New(mathrand.NewSource(*seed))
			selectedIndices := make([]int, 0, *sample)
			for _, key := range keys {
				indices := append([]int(nil), groups[key]...)
				rng.Shuffle(len(indices), func(i, j int) {
					indices[i], indices[j] = indices[j], indices[i]
				})
				count := min(perStratum, len(indices))
				selectedIndices = append(selectedIndices, indices[:count]...)
				strataCounts[key] = count
			}
			sort.Ints(selectedIndices)
			selected = make([]replayImportCandidate, 0, len(selectedIndices))
			for _, index := range selectedIndices {
				selected = append(selected, candidates[index])
			}
		}
	}

	for _, item := range selected {
		replayCase := item.replayCase
		diff, err := source.GetDiff(*projectID, replayCase.IID)
		if err != nil {
			if source.Name() == "github" {
				fmt.Fprintf(os.Stderr, "replay-import: %s repo %s PR #%d diff: %v\n", source.Name(), manifestRepo, replayCase.IID, err)
			} else {
				fmt.Fprintf(os.Stderr, "replay-import: %s project %d MR !%d diff: %v\n", source.Name(), *projectID, replayCase.IID, err)
			}
			summary.Failed++
			continue
		}

		decision := item.decision
		if decision == "" && !*allowUnlabeled {
			summary.SkippedUnlabeled++
			continue
		}

		expected := replayExpected{Recommendation: decision, Source: "gitlab-comment"}
		if decision == "" {
			expected.Label = &report.Label{HumanOutcome: report.HumanOutcomeUnlabeled}
			if source.Name() == "github" {
				expected.Source = "github-unlabeled"
			} else {
				expected.Source = "unlabeled"
			}
		} else if (decision == "HOLD" || decision == "REVIEW") && hasLabel(replayCase.Labels, report.FalsePositiveLabel) {
			expected.Recommendation = "PROCEED"
			expected.Source = "gitlab-comment+fp-label"
			expected.Label = &report.Label{
				HumanOutcome:   report.HumanOutcomeOvercautious,
				EvidenceSource: report.EvidenceSourceReviewerComment,
				Derivation:     report.DerivationInferred,
				Confidence:     report.ConfidenceMedium,
				EvidenceRef:    "mr-note",
			}
		} else {
			expected.Label = &report.Label{HumanOutcome: report.HumanOutcomeUnlabeled}
		}

		var caseName string
		if source.Name() == "github" {
			caseName = replayImportGitHubCaseName(owner, repo, replayCase.IID)
		} else {
			caseName = replayImportCaseName(source.Name(), *projectID, replayCase.IID)
		}
		caseDir := filepath.Join(*outDir, caseName)
		if err := os.MkdirAll(caseDir, 0755); err != nil {
			return summary, fmt.Errorf("create replay case directory %q: %w", caseName, err)
		}
		if err := writeReplayImportJSON(filepath.Join(caseDir, "diff.json"), diff); err != nil {
			return summary, fmt.Errorf("write case %q diff: %w", caseName, err)
		}
		expectedPath := filepath.Join(caseDir, "expected.json")
		preserveExpected := false
		if !*force {
			existingData, err := os.ReadFile(expectedPath)
			if err == nil {
				var existing replayExpected
				if json.Unmarshal(existingData, &existing) == nil && existing.Label != nil && existing.Label.Derivation == report.DerivationExplicit {
					preserveExpected = true
				}
			} else if !os.IsNotExist(err) {
				return summary, fmt.Errorf("read case %q expected recommendation: %w", caseName, err)
			}
		}
		if preserveExpected {
			summary.PreservedLabels++
		} else if err := writeReplayImportJSON(expectedPath, expected); err != nil {
			return summary, fmt.Errorf("write case %q expected recommendation: %w", caseName, err)
		}

		manifest[caseName] = replayImportManifestEntry{
			Source:  source.Name(),
			Project: *projectID,
			IID:     replayCase.IID,
			Repo:    manifestRepo,
		}
		summary.Imported++
	}

	out := make(map[string]any, len(manifest)+1)
	for key, entry := range manifest {
		out[key] = entry
	}
	if *sample > 0 {
		out["sample"] = struct {
			N            int            `json:"n"`
			Seed         int64          `json:"seed"`
			StrataCounts map[string]int `json:"strata_counts"`
		}{
			N:            *sample,
			Seed:         *seed,
			StrataCounts: strataCounts,
		}
	}
	if err := writeReplayImportJSON(manifestPath, out); err != nil {
		return summary, fmt.Errorf("write replay manifest: %w", err)
	}
	fmt.Printf("imported: %d, skipped-unlabeled: %d, failed: %d, preserved-labels: %d\n", summary.Imported, summary.SkippedUnlabeled, summary.Failed, summary.PreservedLabels)
	return summary, nil
}

func replayImportCaseName(source string, projectID, iid int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s:%d:%d", source, projectID, iid)))
	return hex.EncodeToString(sum[:])[:12]
}

func replayImportGitHubCaseName(owner, repo string, number int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("github:%s/%s:%d", owner, repo, number)))
	return hex.EncodeToString(sum[:])[:12]
}

func writeReplayImportJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
