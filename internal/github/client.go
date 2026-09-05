package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/twjohnwu/releaseGuard/internal/gitlab"
)

const defaultAPIBase = "https://api.github.com"

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(apiBase, token string) *Client {
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	return &Client{
		base:  strings.TrimRight(apiBase, "/"),
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, body []byte) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("close GitHub response body: %v", err)
		}
	}()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return nil, fmt.Errorf("github rate limit exceeded (set GITHUB_TOKEN to raise the limit): %s %s", method, path)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github %s %s: %d %s", method, path, resp.StatusCode, string(out))
	}
	return out, nil
}

type MergedPR struct {
	Number   int
	MergedAt string
}

func (c *Client) ListMergedPRs(owner, repo, sinceISO string, perPage, maxPages int) ([]MergedPR, error) {
	if perPage <= 0 {
		perPage = 20
	}
	if maxPages <= 0 {
		maxPages = 1
	}

	var cutoff time.Time
	if sinceISO != "" {
		var err error
		cutoff, err = time.Parse(time.RFC3339, sinceISO)
		if err != nil {
			return nil, fmt.Errorf("parse github merged PR cutoff: %w", err)
		}
	}

	var all []MergedPR
	for page := 1; page <= maxPages; page++ {
		q := url.Values{}
		q.Set("state", "closed")
		q.Set("sort", "updated")
		q.Set("direction", "desc")
		q.Set("per_page", fmt.Sprintf("%d", perPage))
		q.Set("page", fmt.Sprintf("%d", page))
		path := fmt.Sprintf("/repos/%s/%s/pulls?%s", url.PathEscape(owner), url.PathEscape(repo), q.Encode())
		body, err := c.do(http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var batch []struct {
			Number    int     `json:"number"`
			MergedAt  *string `json:"merged_at"`
			UpdatedAt *string `json:"updated_at"`
		}
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("unmarshal merged prs: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		// The list is sorted by updated_at desc, so once every item on a
		// page is older than the cutoff, nothing on a later page can
		// qualify either. Unmerged items on a page never signal "stop" —
		// a page full of closed-unmerged PRs can still be followed by a
		// page of qualifying merged PRs (both share the same updated_at
		// ordering).
		allOlderThanSince := sinceISO != ""
		for _, pr := range batch {
			if pr.MergedAt != nil {
				mergedAt, err := time.Parse(time.RFC3339, *pr.MergedAt)
				if err != nil {
					return nil, fmt.Errorf("parse github PR #%d merged_at: %w", pr.Number, err)
				}
				if sinceISO == "" || !mergedAt.Before(cutoff) {
					all = append(all, MergedPR{Number: pr.Number, MergedAt: *pr.MergedAt})
				}
			}
			if allOlderThanSince {
				if pr.UpdatedAt == nil {
					allOlderThanSince = false
					continue
				}
				updatedAt, err := time.Parse(time.RFC3339, *pr.UpdatedAt)
				if err != nil {
					return nil, fmt.Errorf("parse github PR #%d updated_at: %w", pr.Number, err)
				}
				if !updatedAt.Before(cutoff) {
					allOlderThanSince = false
				}
			}
		}
		if allOlderThanSince {
			break
		}
	}
	return all, nil
}

func (c *Client) GetPRFiles(owner, repo string, number int) ([]gitlab.DiffFile, error) {
	var all []gitlab.DiffFile
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("per_page", "100")
		q.Set("page", fmt.Sprintf("%d", page))
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files?%s", url.PathEscape(owner), url.PathEscape(repo), number, q.Encode())
		body, err := c.do(http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var batch []struct {
			Filename         string  `json:"filename"`
			PreviousFilename string  `json:"previous_filename"`
			Patch            *string `json:"patch"`
			Status           string  `json:"status"`
		}
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("unmarshal pr files: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, file := range batch {
			oldPath := file.Filename
			if file.PreviousFilename != "" {
				oldPath = file.PreviousFilename
			}
			diff := ""
			if file.Patch != nil {
				diff = *file.Patch
			}
			all = append(all, gitlab.DiffFile{
				OldPath:     oldPath,
				NewPath:     file.Filename,
				Diff:        diff,
				NewFile:     file.Status == "added",
				RenamedFile: file.Status == "renamed",
				DeletedFile: file.Status == "removed",
			})
		}
	}
	return all, nil
}
