package recipe

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Overlay is a Translation's words keyed by the ids of the one Tree
// (ADR 0015). It cannot change structure: Apply lays the original Tree
// out with these words.
type Overlay struct {
	Lang        string            `json:"lang"`
	Title       string            `json:"title"`
	Servings    string            `json:"servings,omitempty"`
	Time        string            `json:"time,omitempty"`
	Ingredients []IngredientWords `json:"ingredients"`
	Steps       []StepWords       `json:"steps"`
	Notes       []string          `json:"notes,omitempty"`
	Label       string            `json:"label"`
	Dish        string            `json:"dish"` // the Dish's name in Lang

	// Set by the server when accepted.
	From  string    `json:"from,omitempty"` // the original's language
	Model string    `json:"model,omitempty"`
	Job   string    `json:"job,omitempty"` // the translate job's record id
	At    time.Time `json:"at,omitempty"`
}

// IngredientWords are one Ingredient's words.
type IngredientWords struct {
	ID   string `json:"id"`
	Qty  string `json:"qty,omitempty"`
	Unit string `json:"unit,omitempty"`
	Item string `json:"item"`
	Note string `json:"note,omitempty"`
}

// StepWords is one Step's text.
type StepWords struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Apply is r in o's words: a copy, the Tree untouched. Canonical names
// and Credit stay the original's.
func (r *Recipe) Apply(o *Overlay) *Recipe {
	if o == nil {
		return r
	}
	c := *r
	c.Title, c.Servings, c.Time, c.Lang, c.Label = o.Title, o.Servings, o.Time, o.Lang, o.Label
	c.Notes = append([]string(nil), o.Notes...)
	ing := map[string]IngredientWords{}
	for _, w := range o.Ingredients {
		ing[w.ID] = w
	}
	c.Ingredients = make([]Ingredient, len(r.Ingredients))
	for i, in := range r.Ingredients {
		if w, ok := ing[in.ID]; ok {
			in.Qty, in.Unit, in.Item, in.Note = w.Qty, w.Unit, w.Item, w.Note
		}
		c.Ingredients[i] = in
	}
	st := map[string]string{}
	for _, w := range o.Steps {
		st[w.ID] = w.Text
	}
	c.Steps = make([]Step, len(r.Steps))
	for i, s := range r.Steps {
		if t, ok := st[s.ID]; ok {
			s.Text = t
		}
		s.Inputs = append([]string(nil), s.Inputs...)
		c.Steps[i] = s
	}
	return &c
}

var langCode = regexp.MustCompile(`^[a-z]{2}$`)

// ValidateOverlay checks that o covers r exactly: every ingredient and
// step id once, no other id, the same number of notes, and words in
// every field the original has.
func ValidateOverlay(r *Recipe, o *Overlay, want string) []Problem {
	var ps []Problem
	add := func(f string, a ...any) { ps = append(ps, Problem{Kind: "schema", Message: fmt.Sprintf(f, a...)}) }
	if o.Lang != want {
		add("lang must be %q, the language you are translating into", want)
	}
	if strings.TrimSpace(o.Title) == "" {
		add("title is empty")
	}
	if l := strings.TrimSpace(o.Label); l == "" || len([]rune(l)) > MaxLabelLen {
		add("label must be 1 to %d characters", MaxLabelLen)
	}
	if strings.TrimSpace(o.Dish) == "" || len([]rune(o.Dish)) > 60 {
		add("dish must be the dish's everyday name, at most 60 characters")
	}
	if (r.Servings == "") != (o.Servings == "") {
		add("servings must be given exactly when the original has it")
	}
	if (r.Time == "") != (o.Time == "") {
		add("time must be given exactly when the original has it")
	}
	if len(o.Notes) != len(r.Notes) {
		add("notes: give %d notes, one per original note, in order (got %d)", len(r.Notes), len(o.Notes))
	}
	want1 := map[string]bool{}
	for _, in := range r.Ingredients {
		want1[in.ID] = true
	}
	seen := map[string]bool{}
	for _, w := range o.Ingredients {
		switch {
		case !want1[w.ID]:
			add("ingredients: %q is not an ingredient id of this card", w.ID)
		case seen[w.ID]:
			add("ingredients: %q is given twice", w.ID)
		case strings.TrimSpace(w.Item) == "":
			add("ingredients: %q has no item", w.ID)
		}
		seen[w.ID] = true
	}
	for _, in := range r.Ingredients {
		if !seen[in.ID] {
			add("ingredients: %q is missing", in.ID)
		}
	}
	want2 := map[string]bool{}
	for _, s := range r.Steps {
		want2[s.ID] = true
	}
	seen = map[string]bool{}
	for _, w := range o.Steps {
		switch {
		case !want2[w.ID]:
			add("steps: %q is not a step id of this card", w.ID)
		case seen[w.ID]:
			add("steps: %q is given twice", w.ID)
		case strings.TrimSpace(w.Text) == "":
			add("steps: %q has no text", w.ID)
		}
		seen[w.ID] = true
	}
	for _, s := range r.Steps {
		if !seen[s.ID] {
			add("steps: %q is missing", s.ID)
		}
	}
	return ps
}

// OverlaySchema is submit_translation's input schema.
func OverlaySchema() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lang":     str("Two-letter code of the language you translated into."),
			"title":    str("The title, translated."),
			"servings": str("Servings, translated; only if the original has it."),
			"time":     str("Total time, translated; only if the original has it."),
			"ingredients": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":   str("The ingredient's id, unchanged."),
						"qty":  str("The quantity as written in the original; translate only words in it (\"a pinch\")."),
						"unit": str("The unit in the target language (cup → taza); do not convert."),
						"item": str("The ingredient, in the target cook's words."),
						"note": str("Its note, translated; only if the original has one."),
					},
					"required": []string{"id", "item"},
				},
			},
			"steps": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":   str("The step's id, unchanged."),
						"text": str("The step, a terse verb phrase in the target language; keep times and temperatures."),
					},
					"required": []string{"id", "text"},
				},
			},
			"notes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "One per original note, in order."},
			"label": str("The variant label, translated."),
			"dish":  str("The dish's everyday name in the target language."),
		},
		"required": []string{"lang", "title", "ingredients", "steps", "label", "dish"},
	}
}
