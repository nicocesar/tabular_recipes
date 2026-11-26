package dish

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
)

func TestAmount(t *testing.T) {
	for _, c := range [][3]string{
		{"1 ½", "cups", "1.5 cup"},
		{"1½", "cup", "1.5 cup"},
		{"1 1/2", "Cups", "1.5 cup"},
		{"⅔", "cup", "0.667 cup"},
		{"2", "Tbsp", "2 tbsp"},
		{"2", "tablespoons", "2 tbsp"},
		{"1", "teaspoon", "1 tsp"},
		{"1–2", "tsp", "1-2 tsp"},
		{"a pinch", "", "a pinch"},
		{"500", "gramos", "500 g"},
		{"3", "", "3"},
	} {
		if got := Amount(c[0], c[1]); got != c[2] {
			t.Errorf("Amount(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func load(t *testing.T, name string) *recipe.Recipe {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var r recipe.Recipe
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// The day-one pair: the same bran muffins from two sites, tabulated into
// different trees. They fold.
func TestBranMuffinsFold(t *testing.T) {
	a, b := load(t, "bran-cafesucrefarine.json"), load(t, "bran-getrecipecart.json")
	same, report := Same(a, b)
	if !same {
		t.Fatalf("should fold: %s", report)
	}
	if report != "15/15 ingredients shared, 15/15 amounts equal" {
		t.Errorf("report: %s", report)
	}
}

func TestSlack(t *testing.T) {
	a := load(t, "bran-cafesucrefarine.json")
	b := load(t, "bran-cafesucrefarine.json")
	b.Ingredients = b.Ingredients[:len(b.Ingredients)-1] // drop the butter
	b.Ingredients[0].Qty = "⅓"                           // one amount differs
	if same, r := Same(a, b); !same {
		t.Errorf("one ingredient and one amount of slack should fold: %s", r)
	}
	b.Ingredients[1].Qty = "1"
	if same, r := Same(a, b); same {
		t.Errorf("two amounts differ, should not fold: %s", r)
	}
	c := load(t, "bran-cafesucrefarine.json")
	c.Ingredients[3].Canon = ""
	if same, _ := Same(a, c); same {
		t.Error("missing canon must not fold")
	}
}

func TestCompare(t *testing.T) {
	ref := load(t, "bran-cafesucrefarine.json")
	v := load(t, "bran-cafesucrefarine.json")
	if d := Compare(ref, v); len(d.Added)+len(d.Changed)+len(d.Missing) != 0 || d.Same != len(ref.Ingredients) {
		t.Fatalf("identical cards differ: %+v", d)
	}
	gone := v.Ingredients[len(v.Ingredients)-1]
	v.Ingredients = v.Ingredients[:len(v.Ingredients)-1]
	v.Ingredients[0].Qty = "3"
	v.Ingredients = append(v.Ingredients, recipe.Ingredient{ID: "x1", Qty: "1", Unit: "cup", Item: "raisins", Canon: "raisin"})
	d := Compare(ref, v)
	if len(d.Added) != 1 || d.Added[0].Item != "raisins" || !d.Marks["x1"].Added() {
		t.Errorf("added: %+v", d.Added)
	}
	if len(d.Changed) != 1 || d.Changed[0].ItemChanged || d.Changed[0].Was.ID != ref.Ingredients[0].ID {
		t.Errorf("changed: %+v", d.Changed)
	}
	if len(d.Missing) != 1 || d.Missing[0].ID != gone.ID {
		t.Errorf("missing: %+v", d.Missing)
	}
	// Another language: words differ, amounts are what is compared.
	es := load(t, "bran-cafesucrefarine.json")
	es.Lang = "es"
	es.Ingredients[2].Item = "algo en castellano"
	if d := Compare(ref, es); len(d.Changed) != 0 {
		t.Errorf("translation counted as change: %+v", d.Changed)
	}
}
