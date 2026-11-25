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
						"id":    str("Unique id, e.g. i1, i2…"),
						"qty":   str("Quantity exactly as written: \"2\", \"1 ½\", \"a pinch\"."),
						"unit":  str("Unit as written: \"cups\", \"g\", \"tbsp\"; empty if none."),
						"item":  str("The ingredient: \"all-purpose flour\"."),
						"note":  str("Short prep note: \"sifted\", \"room temperature\"."),
						"canon": str("Matching key: the ingredient in plain English, lowercase, singular, without prep or brand — \"chicken\" for chicken thighs or pechuga de pollo, \"tomato\" for canned diced tomatoes, \"oil\" for olive oil. Never shown."),
					},
					"required": []string{"id", "item", "canon"},
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
			"lang":  str("The source's language as a two-letter ISO 639-1 code: \"es\", \"en\". The card is written in it."),
			"label": str("A few words, in lang, that tell this version of the dish apart from others: \"with coconut milk & spinach\", \"con papas, estilo argentino\". For the first card of a dish, what is characteristic of it."),
			"dish": map[string]any{
				"type":        "object",
				"description": "Which dish this is. Use an existing dish's id when this is a version of it (in any language); otherwise give a new dish name.",
				"properties": map[string]any{
					"id":   str("An existing dish id from the list you were given."),
					"name": str("For a new dish only: its everyday name in lang, without the variant (\"Chicken curry\", not \"Creamy chicken curry\")."),
				},
			},
			"notes": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "At most a few short paraphrased notes: preheating, resting, substitutions. Never copied prose.",
			},
		},
		"required": []string{"title", "lang", "label", "dish", "ingredients", "steps"},
	}
}
