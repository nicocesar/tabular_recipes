package recipe

// Schema is the input_schema of the submit_recipe tool: the Recipe as
// JSON. Validate is still the authority — the schema only steers.
func Schema() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": str("The dish's name, short, as a cook would say it."),
			"credit": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source_title": str("The source page's own title for the recipe."),
					"author":       str("The author's name, only if the page states it."),
					"url":          str("The source URL."),
				},
			},
			"servings": str("e.g. \"4\" or \"one 9-inch pie\"; omit if unknown."),
			"time":     str("Total time, e.g. \"1 h 15 min\"; omit if unknown."),
			"ingredients": map[string]any{
				"type":        "array",
				"description": "Every ingredient once, as leaves of the tree. Split an ingredient used in two places into two rows.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":   str("Unique id, e.g. i1, i2…"),
						"qty":  str("Quantity exactly as written: \"2\", \"1 ½\", \"a pinch\"."),
						"unit": str("Unit as written: \"cups\", \"g\", \"tbsp\"; empty if none."),
						"item": str("The ingredient: \"all-purpose flour\"."),
						"note": str("Short prep note: \"sifted\", \"room temperature\"."),
					},
					"required": []string{"id", "item"},
				},
			},
			"steps": map[string]any{
				"type":        "array",
				"description": "Internal nodes of the tree. Each step consumes ingredients and/or earlier steps; each id is consumed exactly once; exactly one step (the finished dish) is consumed by none.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":     str("Unique id, e.g. s1, s2…"),
						"text":   str("Terse verb phrase in your own words, ≤ 6 words where possible; keep times and temperatures: \"whisk\", \"fold in\", \"bake 25 min 350°F\"."),
						"inputs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Ids of ingredients/steps this step combines, in the top-to-bottom order they should appear."},
					},
					"required": []string{"id", "text", "inputs"},
				},
			},
			"notes": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "At most a few short paraphrased notes: preheating, resting, substitutions. Never copied prose.",
			},
		},
		"required": []string{"title", "ingredients", "steps"},
	}
}
