package gitlab

import (
	"encoding/json"
	"fmt"
)

func (c *Client) PostMRNote(projectID, mrIID int, body string) error {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("marshal note: %w", err)
	}
	_, err = c.do("POST",
		fmt.Sprintf("/projects/%d/merge_requests/%d/notes", projectID, mrIID), payload)
	if err != nil {
		return fmt.Errorf("post note: %w", err)
	}
	return nil
}
