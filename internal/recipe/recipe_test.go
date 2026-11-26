package recipe

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func load(t *testing.T, name string) *Recipe {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var r Recipe
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestValidPancakes(t *testing.T) {
	r := load(t, "pancakes.json")
	if ps := Validate(r); ps != nil {
		t.Fatalf("unexpected problems: %v", ps)
	}
	if got := r.Root().ID; got != "s4" {
		t.Fatalf("root = %s", got)
	}
}

func TestLayPancakes(t *testing.T) {
	l := Lay(load(t, "pancakes.json"))
	if l.Rows != 8 || l.Cols != 4 {
		t.Fatalf("grid %dx%d, want 8 rows x 4 cols", l.Rows, l.Cols)
	}
	want := map[string][4]int{ // row, rowspan, col, colspan
		"i1": {0, 1, 0, 1}, "i4": {3, 1, 0, 1}, "i7": {6, 1, 0, 1},
		"i8": {7, 1, 0, 3}, // added at the last step: stretches to column 3
		"s1": {0, 4, 1, 1}, "s2": {4, 3, 1, 1},
		"s3": {0, 7, 2, 1}, "s4": {0, 8, 3, 1},
	}
	for _, c := range l.Cells {
		id := ""
		if c.Ingredient != nil {
			id = c.Ingredient.ID
		} else {
			id = c.Step.ID
		}
		w, ok := want[id]
		if !ok {
			continue
		}
		got := [4]int{c.Row, c.RowSpan, c.Col, c.ColSpan}
		if got != w {
			t.Errorf("%s: got %v want %v", id, got, w)
		}
	}
}

func TestTiling(t *testing.T) {
	l := Lay(load(t, "pancakes.json"))
	grid := make([][]int, l.Rows)
	for i := range grid {
		grid[i] = make([]int, l.Cols)
	}
	for _, c := range l.Cells {
		for r := c.Row; r < c.Row+c.RowSpan; r++ {
			for k := c.Col; k < c.Col+c.ColSpan; k++ {
				grid[r][k]++
			}
		}
	}
	for r := range grid {
		for k := range grid[r] {
			if grid[r][k] != 1 {
				t.Fatalf("cell (%d,%d) covered %d times", r, k, grid[r][k])
			}
		}
	}
}

func TestRejections(t *testing.T) {
	cases := map[string]struct {
		edit func(r *Recipe)
		want string
	}{
		"reused ingredient": {func(r *Recipe) { r.Steps[1].Inputs = append(r.Steps[1].Inputs, "i1") }, "used by both"},
		"unused ingredient": {func(r *Recipe) { r.Steps[3].Inputs = []string{"s3"} }, "never used"},
		"two roots":         {func(r *Recipe) { r.Steps[3].Inputs = []string{"i8"} }, "final steps"},
		"unknown id":        {func(r *Recipe) { r.Steps[0].Inputs[0] = "x9" }, "not an ingredient or step"},
		"no inputs":         {func(r *Recipe) { r.Steps = append(r.Steps, Step{ID: "s5", Text: "preheat"}) }, "no inputs"},
		"duplicate id":      {func(r *Recipe) { r.Ingredients[1].ID = "i1" }, "more than once"},
		"cycle": {func(r *Recipe) {
			r.Steps = append(r.Steps, Step{ID: "s5", Text: "a", Inputs: []string{"s6"}}, Step{ID: "s6", Text: "b", Inputs: []string{"s5"}})
		}, "cycle"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := load(t, "pancakes.json")
			tc.edit(r)
			ps := Validate(r)
			var msgs []string
			for _, p := range ps {
				msgs = append(msgs, p.Message)
			}
			if !strings.Contains(strings.Join(msgs, "|"), tc.want) {
				t.Fatalf("want %q in %v", tc.want, msgs)
			}
		})
	}
}

func TestStepsInOrder(t *testing.T) {
	var ids []string
	for _, s := range StepsInOrder(load(t, "pancakes.json")) {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids, ","); got != "s1,s2,s3,s4" {
		t.Fatal(got)
	}
}

func TestOverlay(t *testing.T) {
	r := load(t, "pancakes.json")
	o := &Overlay{Lang: "es", Title: "Panqueques", Servings: r.Servings, Time: r.Time, Label: "x", Dish: "Panqueques"}
	for _, in := range r.Ingredients {
		o.Ingredients = append(o.Ingredients, IngredientWords{ID: in.ID, Item: "es"})
	}
	for _, s := range r.Steps {
		o.Steps = append(o.Steps, StepWords{ID: s.ID, Text: "es"})
	}
	o.Notes = make([]string, len(r.Notes))
	if ps := ValidateOverlay(r, o, "es"); len(ps) != 0 {
		t.Fatalf("a full overlay: %v", ps)
	}
	o.Steps = append(o.Steps, StepWords{ID: "s99", Text: "x"})
	o.Ingredients = o.Ingredients[1:]
	if ps := ValidateOverlay(r, o, "it"); len(ps) != 3 {
		t.Fatalf("want lang, unknown step, missing ingredient: %v", ps)
	}
}
