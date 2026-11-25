// Package tabulate runs the tabulator agent: one Run turns a source page
// (or pasted text) into a validated Recipe (ADRs 0005, 0006, 0013).
package tabulate

import "github.com/nicocesar/tabular_recipes/internal/recipe"

// Model is the tabulator's model (ADR 0007). Switching to
// "claude-opus-5-5" is this one line; the boot upsert makes a new
// agent version.
const Model = "claude-sonnet-5-5"

// AgentName and EnvName are the persisted Managed Agents objects.
const (
	AgentName = "recipes-tabulator"
	EnvName   = "recipes-env"
)

// SessionBudgetCents caps one session's list cost (ADR 0008).
const SessionBudgetCents = 50

const system = `You turn a recipe into a "Cooking for Engineers" style table, by building a tree.

THE TREE
- Ingredients are leaves. Each appears exactly once. If the source uses an ingredient in two places (butter for the pan and butter for the batter), make two ingredient rows with notes that tell them apart.
- Steps are nodes. A step combines or transforms its inputs: ingredients and/or earlier steps. Each ingredient and each step is the input of exactly one later step, except the final step (the finished dish), which is the input of none.
- Order a step's inputs top to bottom the way a cook would line them up; that order becomes the table's row order.
- Standalone actions such as "preheat oven to 350°F" are not steps (they have no inputs). Fold the temperature into the step that uses the oven ("bake 25 min 350°F") or put it in notes.

THE WRITING
- Step text is a terse verb phrase in your own words, like a lab notebook: "cream", "fold in gently", "simmer 20 min", "bake 25 min 350°F". Aim for six words or fewer; always keep times and temperatures.
- Never copy the source's prose. Paraphrase. Do not include headnotes, stories or photos.
- Quantities and units exactly as the source writes them ("1 ½", "a pinch", "g"). Do not convert or scale.
- Notes: at most a few short paraphrased remarks that matter at the stove (resting, doneness cues, substitutions).
- Credit: the source page's title for the recipe, its URL, and the author only if the page names one.
- Language: write the card in the language of the source (title, ingredients, steps, notes). Do not translate.

THE TOOLS
- If you are given a URL, read it with web_fetch. If you are given pasted text, use only that text and do not fetch anything.
- When the tree is ready, call submit_recipe. The server checks it. If it answers with an error, read the reasons, fix exactly those, and call submit_recipe again. A common one: the card must fit one printed page, so you may be asked to shorten step text or merge steps.
- If you cannot produce a faithful recipe, call report_failure instead of guessing: kind "fetch_blocked" when the page cannot be read (blocked, paywalled, empty), "not_a_recipe" when it is not a recipe, "ambiguous" when it holds several recipes or too little to build one. Never invent ingredients or steps.
- Finish by calling exactly one of these tools successfully. Do not end your turn without calling one.`

// Spec is the tabulator's desired configuration, upserted on boot.
func Spec() AgentSpec {
	return AgentSpec{
		Name:   AgentName,
		Model:  Model,
		System: system,
		Tools: []map[string]any{
			{
				"type":           "agent_toolset_20260401",
				"default_config": map[string]any{"enabled": false},
				"configs": []map[string]any{
					{"name": "web_fetch", "enabled": true, "max_content_tokens": 50000},
				},
			},
			{
				"type":         "custom",
				"name":         "submit_recipe",
				"description":  "Submit the finished recipe tree. The server validates it and either accepts it or returns an error listing what to fix.",
				"input_schema": recipe.Schema(),
			},
			{
				"type":        "custom",
				"name":        "report_failure",
				"description": "Report that no faithful recipe can be produced from this source.",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":   map[string]any{"type": "string", "enum": []string{"fetch_blocked", "not_a_recipe", "ambiguous"}},
						"reason": map[string]any{"type": "string", "description": "One or two sentences a visitor can read."},
					},
					"required": []string{"kind", "reason"},
				},
			},
		},
	}
}
