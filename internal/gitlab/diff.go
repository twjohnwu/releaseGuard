package gitlab

import (
	"encoding/json"
	"fmt"
	"net/url"
)

type DiffFile struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
}

func (c *Client) GetMRDiff(projectID, mrIID int) ([]DiffFile, error) {
	const perPage = 100
	var all []DiffFile
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("per_page", fmt.Sprintf("%d", perPage))
		q.Set("page", fmt.Sprintf("%d", page))
		path := fmt.Sprintf("/projects/%d/merge_requests/%d/diffs?%s", projectID, mrIID, q.Encode())
		body, err := c.do("GET", path, nil)
		if err != nil {
			return nil, err
		}
		var batch []DiffFile
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("unmarshal: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			break
		}
	}
	return all, nil
}
