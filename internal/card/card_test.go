package card

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
)

func load(t *testing.T, name string) *recipe.Recipe {
	t.Helper()
	b, err := os.ReadFile("../recipe/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var r recipe.Recipe
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if ps := recipe.Validate(&r); ps != nil {
		t.Fatal(ps)
	}
	return &r
}

func TestSVGRenders(t *testing.T) {
	r := load(t, "pancakes.json")
	svg := string(SVG(r, Options{ID: "abc"}))
	for _, want := range []string{"<svg", "Buttermilk Pancakes — recipe card", `class="stain"`, "<use href=", "</svg>"} {
		if !strings.Contains(svg, want) {
			t.Errorf("missing %q", want)
		}
	}
	if dir := os.Getenv("CARD_OUT"); dir != "" {
		os.WriteFile(dir+"/pancakes.svg", []byte(svg), 0o644)
		os.WriteFile(dir+"/pancakes-print.svg", SVG(r, Options{Print: true}), 0o644)
		os.WriteFile(dir+"/pancakes-worn.svg", SVG(r, Options{ID: "worn-card", Views: 99}), 0o644)
	}
}

func TestPrintHasNoStains(t *testing.T) {
	svg := string(SVG(load(t, "pancakes.json"), Options{ID: "abc", Print: true}))
	if strings.Contains(svg, "stain") || strings.Contains(svg, "grain") {
		t.Fatal("print card carries decoration")
	}
}

func TestStainsAreStable(t *testing.T) {
	r := load(t, "pancakes.json")
	a, b := SVG(r, Options{ID: "same"}), SVG(r, Options{ID: "same"})
	if string(a) != string(b) {
		t.Fatal("same id, different card")
	}
}

func TestFitPancakes(t *testing.T) {
	if f := Fit(load(t, "pancakes.json")); !f.OK {
		t.Fatalf("pancakes should fit: %+v", f)
	}
}

func TestFitRejectsHuge(t *testing.T) {
	r := load(t, "pancakes.json")
	long := strings.Repeat("stir and keep stirring patiently over gentle heat ", 3)
	for i := range r.Steps {
		r.Steps[i].Text = long
	}
	for i := 0; i < 30; i++ {
		id := "x" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		r.Ingredients = append(r.Ingredients, recipe.Ingredient{ID: id, Qty: "1", Unit: "tbsp", Item: "something with a rather long name", Note: "finely chopped and divided"})
		r.Steps[0].Inputs = append(r.Steps[0].Inputs, id)
	}
	if ps := recipe.Validate(r); ps != nil {
		t.Fatal(ps)
	}
	f := Fit(r)
	if f.OK {
		t.Fatalf("expected a fit rejection: %+v", f)
	}
	if !strings.Contains(f.Message, "too") {
		t.Fatal(f.Message)
	}
	t.Log(f.Message)
}

func TestWrapRespectsWidth(t *testing.T) {
	ls := wrap(prose("ladle a quarter cup onto the buttered griddle over medium heat", faceSerif, "ink"), 10, 80)
	if len(ls) < 2 {
		t.Fatal("did not wrap")
	}
	for _, l := range ls {
		if l.w > 80 && len(l.words) > 1 {
			t.Fatalf("line too wide: %v", l.w)
		}
	}
}
