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
