package gitlab

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// MergedMR is the minimal view of a merged merge request the feedback command
// needs: its IID, title, and current label set.
type MergedMR struct {
	IID    int      `json:"iid"`
	Title  string   `json:"title"`
	Labels []string `json:"labels"`
}

// GetMRLabels returns the current label list for a single merge request via
// GET /projects/:id/merge_requests/:iid.
func (c *Client) GetMRLabels(projectID, mrIID int) ([]string, error) {
	body, err := c.do("GET",
		fmt.Sprintf("/projects/%d/merge_requests/%d", projectID, mrIID), nil)
	if err != nil {
		return nil, err
	}
	var mr struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("unmarshal mr labels: %w", err)
	}
	return mr.Labels, nil
}

// ListMergedMRs returns merged MRs updated after sinceISO (RFC3339), newest
// first, walking up to maxPages pages of perPage items. sinceISO may be empty
// to omit the filter. Each result carries iid, title, and labels.
func (c *Client) ListMergedMRs(projectID int, sinceISO string, perPage, maxPages int) ([]MergedMR, error) {
	if perPage <= 0 {
		perPage = 20
	}
	if maxPages <= 0 {
		maxPages = 1
	}
	var all []MergedMR
	for page := 1; page <= maxPages; page++ {
		q := url.Values{}
		q.Set("state", "merged")
		q.Set("order_by", "updated_at")
		q.Set("sort", "desc")
		q.Set("per_page", fmt.Sprintf("%d", perPage))
		q.Set("page", fmt.Sprintf("%d", page))
		if sinceISO != "" {
			q.Set("updated_after", sinceISO)
		}
		body, err := c.do("GET",
			fmt.Sprintf("/projects/%d/merge_requests?%s", projectID, q.Encode()), nil)
		if err != nil {
			return nil, err
		}
		var batch []MergedMR
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("unmarshal merged mrs: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			break
		}
	}
	return all, nil
}

// GetMRNotes returns the note bodies for a merge request via
// GET /projects/:id/merge_requests/:iid/notes. Used to recover the decision
// ReleaseGuard emitted from its own MR comment.
func (c *Client) GetMRNotes(projectID, mrIID int) ([]string, error) {
	q := url.Values{}
	q.Set("per_page", "100")
	q.Set("sort", "desc")
	q.Set("order_by", "created_at")
	body, err := c.do("GET",
		fmt.Sprintf("/projects/%d/merge_requests/%d/notes?%s", projectID, mrIID, q.Encode()), nil)
	if err != nil {
		return nil, err
	}
	var notes []struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &notes); err != nil {
		return nil, fmt.Errorf("unmarshal notes: %w", err)
	}
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Body)
	}
	return out, nil
}
