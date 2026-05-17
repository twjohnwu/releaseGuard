package result

import (
	"context"
	"os"
	"strings"

	"github.com/acme/releaseguard/internal/gitlab"
)

type Poster struct {
	gitlab        *gitlab.Client
	projectID     int
	mrIID         int
	minConfidence float64
}

func NewPoster(c *gitlab.Client, projectID, mrIID int, minConfidence float64) *Poster {
	return &Poster{gitlab: c, projectID: projectID, mrIID: mrIID, minConfidence: minConfidence}
}

func (p *Poster) Post(ctx context.Context, comment string, requiredTests []string, writeArtifact bool) error {
	if comment != "" {
		if err := p.gitlab.PostMRNote(p.projectID, p.mrIID, comment); err != nil {
			return err
		}
	}
	if writeArtifact && len(requiredTests) > 0 {
		if err := os.WriteFile("selective-tests.txt",
			[]byte(strings.Join(requiredTests, "|")), 0644); err != nil {
			return err
		}
	}
	return nil
}
