// Package recipe is the stored shape of a Recipe — a Tree of Ingredients
// (leaves) and Steps (nodes) with one Root — and the rules a Submission
// must satisfy (ADR 0002). Layout lives here too, because it is a pure
// function of the Tree; drawing it is internal/card's job.
package recipe

import (
	"fmt"
	"sort"
	"strings"
)

// Recipe is what the agent submits through submit_recipe and what the
// store keeps. JSON names are the tool's input schema (see Schema).
type Recipe struct {
	Title       string       `json:"title"`
	Credit      Credit       `json:"credit"`
	Servings    string       `json:"servings,omitempty"`
	Time        string       `json:"time,omitempty"`
	Ingredients []Ingredient `json:"ingredients"`
	Steps       []Step       `json:"steps"`
	Notes       []string     `json:"notes,omitempty"`
	// Lang is the language the text is in (ISO 639-1, "es"): the source's,
	// or an Overlay's once translated (ADR 0015). The Card's fixed labels
	// follow it (ADR 0016). Empty means English.
	Lang string `json:"lang,omitempty"`
	// Label tells this Variant apart within its Dish ("with coconut milk
	// & spinach"), in Lang (ADR 0017).
	Label string `json:"label,omitempty"`
}

// Credit says where the Recipe came from (ADR 0012).
type Credit struct {
	SourceTitle string `json:"source_title,omitempty"`
	Author      string `json:"author,omitempty"`
	URL         string `json:"url,omitempty"`
}

// Ingredient is a leaf. Quantities are strings exactly as written.
type Ingredient struct {
	ID   string `json:"id"`
	Qty  string `json:"qty,omitempty"`
	Unit string `json:"unit,omitempty"`
	Item string `json:"item"`
	Note string `json:"note,omitempty"`
	// Canon is the matching key across Variants (ADR 0017): English,
	// lowercase, singular ("chicken"), whatever the source language.
	// Never shown. Empty on cards filed before dishes existed.
	Canon string `json:"canon,omitempty"`
}

// Step is a node: a short verb phrase and the ids it consumes.
type Step struct {
	ID     string   `json:"id"`
	Text   string   `json:"text"`
	Inputs []string `json:"inputs"`
}

// Limits keep a Submission bounded before layout ever runs.
const (
	MaxIngredients = 40
	MaxSteps       = 40
	MaxTextLen     = 200
	MaxNotes       = 8
)

// Problem is one reason a Submission is rejected, phrased for the agent.
type Problem struct {
	Kind    string // "schema" or "tree"
	Message string
}

// Validate checks the Tree rules and returns every problem found (nil
// when the Recipe is acceptable). It never mutates r.
func Validate(r *Recipe) []Problem {
	var ps []Problem
	add := func(kind, f string, a ...any) { ps = append(ps, Problem{kind, fmt.Sprintf(f, a...)}) }

	if strings.TrimSpace(r.Title) == "" {
		add("schema", "title is empty")
	}
	if len(r.Ingredients) == 0 {
		add("schema", "there are no ingredients")
	}
	if len(r.Steps) == 0 {
		add("schema", "there are no steps")
	}
	if len(r.Ingredients) > MaxIngredients {
		add("schema", "%d ingredients is more than the %d allowed; group minor items (e.g. \"spice mix: …\")", len(r.Ingredients), MaxIngredients)
	}
	if len(r.Steps) > MaxSteps {
		add("schema", "%d steps is more than the %d allowed; merge consecutive steps", len(r.Steps), MaxSteps)
	}
	if len(r.Notes) > MaxNotes {
		add("schema", "%d notes is more than the %d allowed", len(r.Notes), MaxNotes)
	}
	for _, n := range r.Notes {
		if len(n) > 2*MaxTextLen {
			add("schema", "a note is longer than %d characters; shorten it", 2*MaxTextLen)
			break
		}
	}

	kind := map[string]string{} // id -> "ingredient" | "step"
	for _, in := range r.Ingredients {
		if in.ID == "" {
			add("schema", "an ingredient (%q) has no id", in.Item)
			continue
		}
		if strings.TrimSpace(in.Item) == "" {
			add("schema", "ingredient %s has no item", in.ID)
		}
		if len(in.Item)+len(in.Note)+len(in.Qty)+len(in.Unit) > MaxTextLen {
			add("schema", "ingredient %s is longer than %d characters", in.ID, MaxTextLen)
		}
		if _, dup := kind[in.ID]; dup {
			add("schema", "id %s is used more than once", in.ID)
		}
		kind[in.ID] = "ingredient"
	}
	for _, s := range r.Steps {
		if s.ID == "" {
			add("schema", "a step (%q) has no id", s.Text)
			continue
		}
		if strings.TrimSpace(s.Text) == "" {
			add("schema", "step %s has no text", s.ID)
		}
		if len(s.Text) > MaxTextLen {
			add("schema", "step %s is longer than %d characters", s.ID, MaxTextLen)
		}
		if _, dup := kind[s.ID]; dup {
			add("schema", "id %s is used more than once", s.ID)
		}
		kind[s.ID] = "step"
	}
	if len(ps) > 0 {
		return ps // structural checks below assume unique, present ids
	}

	consumedBy := map[string]string{}
	for _, s := range r.Steps {
		if len(s.Inputs) == 0 {
			add("tree", "step %s (%q) has no inputs; every step must combine or transform something — put standalone actions like \"preheat oven\" in the first step that needs them or in notes", s.ID, s.Text)
		}
		for _, in := range s.Inputs {
			if _, ok := kind[in]; !ok {
				add("tree", "step %s uses %s, which is not an ingredient or step id", s.ID, in)
				continue
			}
			if in == s.ID {
				add("tree", "step %s uses itself", s.ID)
				continue
			}
			if prev, dup := consumedBy[in]; dup {
				add("tree", "%s is used by both %s and %s; each ingredient and step feeds exactly one later step (split the ingredient into two rows, e.g. \"butter (for the pan)\" and \"butter (for the batter)\")", in, prev, s.ID)
				continue
			}
			consumedBy[in] = s.ID
		}
	}
	for _, in := range r.Ingredients {
		if _, ok := consumedBy[in.ID]; !ok {
			add("tree", "ingredient %s (%s) is never used by any step", in.ID, in.Item)
		}
	}
	var roots []string
	for _, s := range r.Steps {
		if _, ok := consumedBy[s.ID]; !ok {
			roots = append(roots, s.ID)
		}
	}
	switch {
	case len(roots) == 0:
		add("tree", "every step is used by another step, so there is a cycle and no final step")
	case len(roots) > 1:
		add("tree", "there are %d final steps (%s); exactly one step must be the finished dish, so connect the others into it", len(roots), strings.Join(roots, ", "))
	}
	if len(ps) > 0 {
		return ps
	}
	// One root and every other node consumed exactly once: it is a tree
	// unless some cycle is detached from the root.
	reach := map[string]bool{}
	steps := r.stepIndex()
	var walk func(id string)
	walk = func(id string) {
		if reach[id] {
			return
		}
		reach[id] = true
		if s, ok := steps[id]; ok {
			for _, in := range s.Inputs {
				walk(in)
			}
		}
	}
	walk(roots[0])
	var lost []string
	for id := range kind {
		if !reach[id] {
			lost = append(lost, id)
		}
	}
	if len(lost) > 0 {
		sort.Strings(lost)
		add("tree", "%s form a cycle not connected to the final step", strings.Join(lost, ", "))
	}
	return ps
}

func (r *Recipe) stepIndex() map[string]*Step {
	m := make(map[string]*Step, len(r.Steps))
	for i := range r.Steps {
		m[r.Steps[i].ID] = &r.Steps[i]
	}
	return m
}

// Root returns the one Step no other Step consumes. Only meaningful on a
// Recipe that passed Validate.
func (r *Recipe) Root() *Step {
	used := map[string]bool{}
	for _, s := range r.Steps {
		for _, in := range s.Inputs {
			used[in] = true
		}
	}
	for i := range r.Steps {
		if !used[r.Steps[i].ID] {
			return &r.Steps[i]
		}
	}
	return nil
}

// IngredientLine is the ingredient as one line of prose: "2 cups flour, sifted".
func (in Ingredient) Line() string {
	parts := []string{}
	if q := strings.TrimSpace(strings.TrimSpace(in.Qty) + " " + strings.TrimSpace(in.Unit)); q != "" {
		parts = append(parts, q)
	}
	parts = append(parts, strings.TrimSpace(in.Item))
	s := strings.Join(parts, " ")
	if n := strings.TrimSpace(in.Note); n != "" {
		s += ", " + n
	}
	return s
}

// MaxLabelLen bounds a Label: a few words.
const MaxLabelLen = 60

// ValidateGrouping checks the fields a new Submission carries for its
// Dish (ADR 0017): the language, a Label, and a canonical name on every
// Ingredient. Cards filed before dishes lack them, so Validate does not.
func ValidateGrouping(r *Recipe) []Problem {
	var ps []Problem
	add := func(f string, a ...any) { ps = append(ps, Problem{"schema", fmt.Sprintf(f, a...)}) }
	if !isLang(r.Lang) {
		add("lang must be the source's two-letter ISO 639-1 code, e.g. \"es\" or \"en\" (got %q)", r.Lang)
	}
	if l := strings.TrimSpace(r.Label); l == "" {
		add("label is empty: give a few words that tell this version apart, e.g. \"with coconut milk\"")
	} else if len([]rune(l)) > MaxLabelLen {
		add("label is longer than %d characters; a few words", MaxLabelLen)
	}
	var missing, bad []string
	for _, in := range r.Ingredients {
		switch c := in.Canon; {
		case strings.TrimSpace(c) == "":
			missing = append(missing, in.ID)
		case c != strings.ToLower(c) || !isASCII(c):
			bad = append(bad, in.ID)
		}
	}
	if len(missing) > 0 {
		add("ingredients %s have no canon (the English, lowercase, singular name, e.g. \"chicken\")", strings.Join(missing, ", "))
	}
	if len(bad) > 0 {
		add("canon must be plain English in lowercase ASCII (\"bell pepper\", not \"Pimiento\"); fix %s", strings.Join(bad, ", "))
	}
	return ps
}

func isLang(s string) bool {
	return len(s) == 2 && s[0] >= 'a' && s[0] <= 'z' && s[1] >= 'a' && s[1] <= 'z'
}

func isASCII(s string) bool {
	for _, c := range s {
		if c > 127 {
			return false
		}
	}
	return true
}

// TreeChanges lists what differs between two Recipes in everything a
// backfill must leave alone (ADR 0017): title, servings, time, notes, and
// every Ingredient and Step except the new grouping fields. Empty when
// they are the same card.
func TreeChanges(was, now *Recipe) []string {
	var out []string
	if was.Title != now.Title {
		out = append(out, fmt.Sprintf("title changed from %q", was.Title))
	}
	if was.Servings != now.Servings || was.Time != now.Time {
		out = append(out, "servings or time changed")
	}
	if strings.Join(was.Notes, "\n") != strings.Join(now.Notes, "\n") {
		out = append(out, "notes changed")
	}
	if len(was.Ingredients) != len(now.Ingredients) {
		out = append(out, fmt.Sprintf("there were %d ingredients, now %d", len(was.Ingredients), len(now.Ingredients)))
	} else {
		for i, a := range was.Ingredients {
			b := now.Ingredients[i]
			if a.ID != b.ID || a.Qty != b.Qty || a.Unit != b.Unit || a.Item != b.Item || a.Note != b.Note {
				out = append(out, fmt.Sprintf("ingredient %s changed (only add canon)", a.ID))
			}
		}
	}
	if len(was.Steps) != len(now.Steps) {
		out = append(out, fmt.Sprintf("there were %d steps, now %d", len(was.Steps), len(now.Steps)))
	} else {
		for i, a := range was.Steps {
			b := now.Steps[i]
			if a.ID != b.ID || a.Text != b.Text || strings.Join(a.Inputs, ",") != strings.Join(b.Inputs, ",") {
				out = append(out, fmt.Sprintf("step %s changed", a.ID))
			}
		}
	}
	return out
}
