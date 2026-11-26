package dish

import (
	"strings"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
)

// Change is one ingredient of a Variant that the Reference also has, in
// another kind or amount.
type Change struct {
	Now, Was recipe.Ingredient
	// ItemChanged is true when the words differ (same language only);
	// otherwise only the amount does.
	ItemChanged bool
}

// Diff is what a Variant does differently from the Reference, by
// canonical name (ADR 0017). Steps are never compared.
type Diff struct {
	Added   []recipe.Ingredient // in the Variant, not the Reference
	Changed []Change
	Missing []recipe.Ingredient // in the Reference, not the Variant
	Same    int                 // identical ingredients
	// Marks maps the Variant's ingredient ids to "add" or "chg".
	Marks map[string]Change
}

// Compare diffs v against ref. An ingredient used twice (butter for the
// pan and the batter) pairs up in order. Items are compared only when
// both cards are in the same language: "pollo" is not a change from
// "chicken".
func Compare(ref, v *recipe.Recipe) Diff {
	d := Diff{Marks: map[string]Change{}}
	sameLang := ref.Lang == v.Lang
	byCanon := map[string][]recipe.Ingredient{}
	for _, in := range ref.Ingredients {
		c := canon(in)
		byCanon[c] = append(byCanon[c], in)
	}
	for _, in := range v.Ingredients {
		c := canon(in)
		list := byCanon[c]
		if len(list) == 0 {
			d.Added = append(d.Added, in)
			d.Marks[in.ID] = Change{Now: in}
			continue
		}
		was := list[0]
		byCanon[c] = list[1:]
		ch := Change{Now: in, Was: was}
		ch.ItemChanged = sameLang && !strings.EqualFold(strings.TrimSpace(in.Item), strings.TrimSpace(was.Item))
		if ch.ItemChanged || Amount(in.Qty, in.Unit) != Amount(was.Qty, was.Unit) {
			d.Changed = append(d.Changed, ch)
			d.Marks[in.ID] = ch
		} else {
			d.Same++
		}
	}
	for _, in := range ref.Ingredients { // what is left over, in card order
		c := canon(in)
		for _, left := range byCanon[c] {
			if left.ID == in.ID {
				d.Missing = append(d.Missing, in)
			}
		}
	}
	return d
}

// Added reports whether the mark is a new ingredient.
func (c Change) Added() bool { return c.Was.ID == "" }

func canon(in recipe.Ingredient) string {
	if c := strings.TrimSpace(in.Canon); c != "" {
		return c
	}
	return strings.ToLower(strings.TrimSpace(in.Item)) // a card from before canon
}

// AmountText is how a quantity reads on a chip: "1 ½ cup".
func AmountText(in recipe.Ingredient) string {
	return strings.TrimSpace(strings.TrimSpace(in.Qty) + " " + strings.TrimSpace(in.Unit))
}
