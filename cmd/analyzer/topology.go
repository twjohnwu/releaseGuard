package main

import (
	"fmt"

	"github.com/twjohnwu/releaseGuard/internal/config"
)

// validateTopology returns "0" (full) or "1" (script-only) or error for invalid combinations.
func validateTopology(c *config.Config) (string, error) {
	hasPG := c.PostgresURL != ""
	if !hasPG {
		// SelectiveTest L1 (Plan B PoC) does not require POSTGRES_URL.
		// L2/L3 (Plan C) will require it; enforce when implemented.
		if c.Agents.Ownership {
			return "", fmt.Errorf("RG_AGENT_OWNERSHIP_ENABLED requires POSTGRES_URL")
		}
		if c.RAGEnabled {
			return "", fmt.Errorf("RG_RAG_ENABLED requires POSTGRES_URL")
		}
		return "1", nil
	}
	return "0", nil
}
