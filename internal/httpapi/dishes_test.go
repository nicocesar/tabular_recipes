package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
)

// fileVariant stores a ready card as the next Variant of dish d.
func fileVariant(t *testing.T, s *Server, d string, r *recipe.Recipe, src string) string {
	t.Helper()
	ctx := context.Background()
	st := s.cfg.Store
	rec := &store.Record{ID: store.NewID(), Status: store.Ready, Recipe: r, SourceURL: src, CreatedAt: time.Now().UTC()}
	if err := st.Create(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDish(ctx, d); err != nil {
		st.CreateDish(ctx, &store.Dish{ID: d, Names: map[string]string{"en": "Bran muffins"}})
	}
	var n int
	st.UpdateDish(ctx, d, func(x *store.Dish) error { n = x.Add(rec.ID); return nil })
	st.Update(ctx, rec.ID, func(x *store.Record) error { x.DishID, x.Variant = d, n; return nil })
	return rec.ID
}

func bran(t *testing.T) *recipe.Recipe {
	b, err := os.ReadFile("../dish/testdata/bran-cafesucrefarine.json")
	if err != nil {
		t.Fatal(err)
	}
	var r recipe.Recipe
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func get(t *testing.T, s *Server, path string, cookies ...*http.Cookie) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func ownerCookie(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest("POST", "/login", strings.NewReader("password=pw"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Value != "" {
			return c
		}
	}
	t.Fatal("no owner cookie")
	return nil
}

func post(t *testing.T, s *Server, path string, form url.Values, c *http.Cookie) int {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(c)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w.Code
}

func TestDishPages(t *testing.T) {
	s := newServer(t)
	ref := bran(t)
	ref.Label = "one bowl, buttermilk"
	v2 := bran(t)
	v2.Label = "with raisins"
	last := len(v2.Ingredients) - 1
	gone := ref.Ingredients[last]
	v2.Ingredients[last] = recipe.Ingredient{ID: gone.ID, Qty: "1", Unit: "cup", Item: "raisins", Canon: "raisin"}
	a := fileVariant(t, s, "d1", ref, "https://a.example/bran")
	b := fileVariant(t, s, "d1", v2, "https://b.example/bran")
	lone := bran(t)
	lone.Title, lone.Label = "Lonely loaf", "the only one"
	c := fileVariant(t, s, "d2", lone, "https://c.example/loaf")

	_, home := get(t, s, "/")
	if !strings.Contains(home, `href="/dishes/d1"`) || !strings.Contains(home, "with raisins") || !strings.Contains(home, "2 variants") {
		t.Errorf("home should list the dish with its labels")
	}
	if !strings.Contains(home, `href="/recipes/`+c+`"`) {
		t.Errorf("a one-card dish links to its card")
	}
	if code, _ := get(t, s, "/dishes/d2"); code != http.StatusSeeOther {
		t.Errorf("one-card dish page: %d", code)
	}
	code, page := get(t, s, "/dishes/d1")
	if code != 200 || !strings.Contains(page, `chip add">&#43; raisins`) || !strings.Contains(page, `chip del">− `+gone.Item) {
		t.Fatalf("dish page chips missing: %d\n%s", code, page[strings.Index(page, "<ol class=\"variants"):])
	}
	if !strings.Contains(page, `/recipes/`+b+`?marks=1`) {
		t.Error("variant links open with marks")
	}
	_, marked := get(t, s, "/recipes/"+b+"?marks=1")
	if !strings.Contains(marked, `class="addcell"`) || !strings.Contains(marked, "Not in this variant:") || !strings.Contains(marked, gone.Item) {
		t.Error("marked card lacks marks or the missing list")
	}
	if _, plain := get(t, s, "/recipes/"+b); strings.Contains(plain, `class="addcell"`) {
		t.Error("marks are only on when asked")
	}
	if _, refPage := get(t, s, "/recipes/"+a+"?marks=1"); strings.Contains(refPage, `class="addcell"`) {
		t.Error("the reference has nothing to mark")
	}
	if _, svg := get(t, s, "/recipes/"+b+"/card.svg?marks=1"); strings.Contains(svg, "addcell\"") {
		t.Error("downloads never carry marks")
	}

	// Owner tools.
	oc := ownerCookie(t, s)
	if code := post(t, s, "/dishes/d1/reference", url.Values{"record": {b}}, oc); code != http.StatusSeeOther {
		t.Fatalf("promote: %d", code)
	}
	if d, _ := s.cfg.Store.GetDish(context.Background(), "d1"); d.Reference != b {
		t.Error("promote did not take")
	}
	if code := post(t, s, "/recipes/"+b+"/label", url.Values{"label": {"raisin bran"}}, oc); code != http.StatusSeeOther {
		t.Fatalf("label: %d", code)
	}
	if code := post(t, s, "/dishes/d1/name", url.Values{"lang": {"es"}, "name": {"Muffins de salvado"}}, oc); code != http.StatusSeeOther {
		t.Fatalf("rename: %d", code)
	}
	if code := post(t, s, "/dishes/d2/merge", url.Values{"into": {"d1"}}, oc); code != http.StatusSeeOther {
		t.Fatalf("merge: %d", code)
	}
	ctx := context.Background()
	if _, err := s.cfg.Store.GetDish(ctx, "d2"); err == nil {
		t.Error("merged dish should be gone")
	}
	if rc, _ := s.cfg.Store.Get(ctx, c); rc.DishID != "d1" || rc.Variant != 3 {
		t.Errorf("merged card: dish %s variant %d", rc.DishID, rc.Variant)
	}
	if code := post(t, s, "/recipes/"+a+"/move", url.Values{"new_name": {"Oat muffins"}}, oc); code != http.StatusSeeOther {
		t.Fatalf("move: %d", code)
	}
	if ra, _ := s.cfg.Store.Get(ctx, a); ra.DishID == "d1" || ra.Variant != 1 {
		t.Errorf("moved card: dish %s variant %d", ra.DishID, ra.Variant)
	}
	if d, _ := s.cfg.Store.GetDish(ctx, "d1"); len(d.Variants) != 2 || d.Next != 4 || d.Names["es"] == "" {
		t.Errorf("d1 after move: %+v", d)
	}
	if code := post(t, s, "/dishes/d1/reference", url.Values{"record": {a}}, &http.Cookie{Name: "x", Value: "y"}); code != http.StatusForbidden {
		t.Errorf("visitors cannot promote: %d", code)
	}
}

func TestUnfold(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	v := fileVariant(t, s, "d1", bran(t), "https://a.example/bran")
	cp := &store.Record{ID: store.NewID(), Status: store.Folded, FoldedInto: v, DishID: "d1", Recipe: bran(t), SourceURL: "https://copy.example/bran", CreatedAt: time.Now()}
	s.cfg.Store.Create(ctx, cp)
	s.cfg.Store.Update(ctx, v, func(x *store.Record) error {
		x.AlsoAt = append(x.AlsoAt, store.Source{URL: cp.SourceURL, Record: cp.ID})
		return nil
	})
	if _, page := get(t, s, "/recipes/"+v); !strings.Contains(page, "copy.example") {
		t.Error("the variant shows where else it was found")
	}
	if code, _ := get(t, s, "/recipes/"+cp.ID); code != http.StatusSeeOther {
		t.Errorf("a copy redirects: %d", code)
	}
	if code := post(t, s, "/recipes/"+cp.ID+"/unfold", nil, ownerCookie(t, s)); code != http.StatusSeeOther {
		t.Fatalf("unfold: %d", code)
	}
	got, _ := s.cfg.Store.Get(ctx, cp.ID)
	if got.Status != store.Ready || got.Variant != 2 || got.DishID != "d1" {
		t.Errorf("unfolded: %s dish %s variant %d", got.Status, got.DishID, got.Variant)
	}
	if vr, _ := s.cfg.Store.Get(ctx, v); len(vr.AlsoAt) != 0 {
		t.Error("the copy should leave Also found at")
	}
}

func TestPrintFurniture(t *testing.T) {
	s := newServer(t)
	a := fileVariant(t, s, "d1", bran(t), "https://a.example/bran")
	v2 := bran(t)
	v2.Label = "with raisins"
	b := fileVariant(t, s, "d1", v2, "https://b.example/bran")
	s.cfg.Store.Update(context.Background(), b, func(x *store.Record) error {
		x.AlsoAt = []store.Source{{URL: "https://copy.example/x", Record: "zz"}}
		return nil
	})
	_ = a
	code, page := get(t, s, "/recipes/"+b+"/print")
	if code != 200 {
		t.Fatal(code)
	}
	for _, want := range []string{`<path class="ink" d="M`, `<title id="card-title">`} {
		if !strings.Contains(page, want) {
			t.Errorf("print lacks %s", want)
		}
	}
	// Glyphs are paths, so check the furniture through the options.
	req := httptest.NewRequest("GET", "/recipes/"+b+"/print", nil)
	rec, _ := s.cfg.Store.Get(context.Background(), b)
	opt := s.printOptions(req, rec)
	if opt.Header != "Bran muffins · variant 2 · with raisins" {
		t.Errorf("header %q", opt.Header)
	}
	if opt.Also != "Also found at: copy.example" || opt.Link != "example.com/r/"+b || len(opt.QR) == 0 {
		t.Errorf("also %q link %q qr %d", opt.Also, opt.Link, len(opt.QR))
	}
	if n := len(qrModules("HTTPS://RECIPES.NICOCESAR.COM/R/ABCDEFGHIJKLM")); n != 29 {
		t.Errorf("the real link should be a version 3 code (29 modules), got %d", n)
	}
	// The QR is uppercase; the short link takes it either way.
	for _, p := range []string{"/r/" + strings.ToUpper(b), "/R/" + strings.ToUpper(b), "/r/" + b} {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/recipes/"+b {
			t.Errorf("%s: %d %s", p, w.Code, w.Header().Get("Location"))
		}
	}
	// The card page and download never carry the QR.
	if _, svg := get(t, s, "/recipes/"+b+"/card.svg"); strings.Contains(svg, "h1.6v1.6") {
		t.Error("the download should have no QR")
	}
}

func TestTranslatePage(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	id := fileVariant(t, s, "d1", bran(t), "https://a.example/bran")
	visitor := &http.Cookie{Name: "lang", Value: "es"}
	_, page := get(t, s, "/recipes/"+id, visitor)
	if !strings.Contains(page, "Traducir al español") || !strings.Contains(page, "Esta ficha está en inglés.") {
		t.Fatal("a Spanish visitor is offered a translation")
	}
	if _, en := get(t, s, "/recipes/"+id); strings.Contains(en, "/translate") {
		t.Error("no translation offered in the card's own language")
	}
	translate := func() *http.Response {
		req := httptest.NewRequest("POST", "/recipes/"+id+"/translate", strings.NewReader("to=es"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w.Result()
	}
	if res := translate(); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/recipes/"+id+"?lang=es" {
		t.Fatalf("translate: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	translate() // a double click
	c, _ := s.cfg.Store.Get(ctx, id)
	jobID := c.Translating["es"]
	if jobID == "" {
		t.Fatal("no job noted on the card")
	}
	if used, _ := s.cfg.Store.Count(ctx, "runs:"+time.Now().UTC().Format("2006-01-02")); used != 1 {
		t.Errorf("a double click should cost one run, used %d", used)
	}
	if _, p := get(t, s, "/recipes/"+id+"?lang=es"); !strings.Contains(p, "El Abuelo está traduciendo esta ficha") || !strings.Contains(p, "/recipes/"+jobID+"/status") {
		t.Error("the card page should poll the translation")
	}
	// The translator's Overlay arrives.
	r := c.Recipe
	o := &recipe.Overlay{Lang: "es", Title: "Muffins de salvado", Label: "un bol", Dish: "Muffins de salvado", From: "en", Servings: r.Servings, Time: r.Time}
	for _, in := range r.Ingredients {
		o.Ingredients = append(o.Ingredients, recipe.IngredientWords{ID: in.ID, Qty: in.Qty, Unit: in.Unit, Item: "es-" + in.Item})
	}
	for _, st := range r.Steps {
		o.Steps = append(o.Steps, recipe.StepWords{ID: st.ID, Text: "batí"})
	}
	o.Notes = make([]string, len(r.Notes))
	s.cfg.Store.Update(ctx, id, func(x *store.Record) error {
		x.Translations = map[string]*recipe.Overlay{"es": o}
		delete(x.Translating, "es")
		return nil
	})
	_, p := get(t, s, "/recipes/"+id, visitor)
	if !strings.Contains(p, "<title>Muffins de salvado — recipes</title>") || !strings.Contains(p, "Traducida del inglés.") || !strings.Contains(p, `/print?lang=es`) {
		t.Error("the Spanish visitor sees the translation")
	}
	if _, orig := get(t, s, "/recipes/"+id+"?orig=1", visitor); !strings.Contains(orig, "Ver la traducción") || strings.Contains(orig, "<title>Muffins de salvado") {
		t.Error("?orig=1 shows the original")
	}
	if _, en := get(t, s, "/recipes/"+id); strings.Contains(en, "Muffins de salvado") {
		t.Error("an English visitor sees the original")
	}
	_, svgEs := get(t, s, "/recipes/"+id+"/card.svg?lang=es")
	_, svgEn := get(t, s, "/recipes/"+id+"/card.svg")
	if svgEs == svgEn {
		t.Error("the download follows ?lang=")
	}
}
