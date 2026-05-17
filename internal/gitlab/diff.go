package gitlab

import (
	"encoding/json"
	"fmt"
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
	body, err := c.do("GET",
		fmt.Sprintf("/projects/%d/merge_requests/%d/diffs", projectID, mrIID), nil)
	if err != nil {
		return nil, err
	}
	var out []DiffFile
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return out, nil
}
