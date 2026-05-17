package reviewer

func ReviewToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"findings"},
		"properties": map[string]any{
			"summary": map[string]any{"type": "string"},
			"findings": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []string{"severity", "category", "title", "body"},
					"properties": map[string]any{
						"severity": map[string]any{
							"enum": []string{"critical", "high", "medium", "low", "info"},
						},
						"category": map[string]any{"type": "string"},
						"title":    map[string]any{"type": "string"},
						"body":     map[string]any{"type": "string"},
						"location": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file":       map[string]any{"type": "string"},
								"line_start": map[string]any{"type": "integer"},
								"line_end":   map[string]any{"type": "integer"},
							},
						},
						"suggestion": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}
