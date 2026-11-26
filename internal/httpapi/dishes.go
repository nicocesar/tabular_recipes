package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/card"
	dishcmp "github.com/nicocesar/tabular_recipes/internal/dish"
	"github.com/nicocesar/tabular_recipes/internal/i18n"
	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
)

// ---- The Dish page (ADR 0017): every Variant with its Label and chips ----

type chip struct{ Kind, Text string } // add, chg, del, more

type variantRow struct {
	ID, Label, LabelLang, From string
	N                          int
	Reference                  bool
	Facts, Same, Copies        string
	Chips                      []chip
}

type dishOption struct{ ID, Name string }

type nameRow struct{ Lang, LangName, Name string }

type dishView struct {
	ID, Name, NameLang, Sub string
	Variants                []variantRow
	Owner                   bool
	Others                  []dishOption
	Names                   []nameRow
}

// variant is a Dish's Variant with its record, in filing order.
type variant struct {
	N   int
	Rec *store.Record
}

// loadDish reads a Dish and its Variants' records; records that are no
// longer ready cards are skipped.
func (s *Server) loadDish(ctx context.Context, id string) (*store.Dish, []variant, *store.Record, error) {
	d, err := s.cfg.Store.GetDish(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	var vs []variant
	var ref *store.Record
	for _, v := range d.Variants {
		rec, err := s.cfg.Store.Get(ctx, v.Record)
		if err != nil || rec.Status != store.Ready || rec.Recipe == nil {
			continue
		}
		vs = append(vs, variant{v.N, rec})
		if v.Record == d.Reference {
			ref = rec
		}
	}
	if ref == nil && len(vs) > 0 {
		ref = vs[0].Rec
	}
	return d, vs, ref, nil
}

func refLang(ref *store.Record) string {
	if ref == nil || ref.Recipe == nil {
		return "en"
	}
	return ref.Recipe.Lang
}

func (s *Server) handleDish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, vs, ref, err := s.loadDish(ctx, r.PathValue("id"))
	if err != nil || len(vs) == 0 {
		s.notFound(w, r)
		return
	}
	if len(vs) == 1 { // a one-card Dish is just its card
		http.Redirect(w, r, "/recipes/"+vs[0].Rec.ID+keepQuery(r), http.StatusSeeOther)
		return
	}
	lang := s.lang(r)
	v := dishView{ID: d.ID, Owner: s.isOwner(r)}
	v.Name, v.NameLang = d.Name(lang, refLang(ref))
	if v.NameLang == lang {
		v.NameLang = ""
	}
	refN := 0
	for _, x := range vs {
		if x.Rec == ref {
			refN = x.N
		}
	}
	v.Sub = i18n.T(lang, "dish.sub", len(vs), refN)
	for _, x := range vs {
		rc := x.Rec.Recipe
		row := variantRow{ID: x.Rec.ID, N: x.N, Label: rc.Label, Reference: x.Rec == ref,
			From:  source(x.Rec, lang),
			Facts: i18n.T(lang, "dish.facts", len(rc.Ingredients), len(rc.Steps)),
		}
		if rc.Lang != lang {
			row.LabelLang = rc.Lang
		}
		if n := len(x.Rec.AlsoAt); n > 0 {
			row.Copies = i18n.T(lang, "dish.copies", n)
		}
		if !row.Reference {
			diff := dishcmp.Compare(ref.Recipe, rc)
			row.Chips = chips(diff, lang)
			row.Same = i18n.T(lang, "dish.same", diff.Same)
		}
		v.Variants = append(v.Variants, row)
	}
	if v.Owner {
		v.Others = s.dishOptions(ctx, lang, d.ID)
		for _, l := range i18n.Langs() {
			v.Names = append(v.Names, nameRow{l, i18n.Name(l), d.Names[l]})
		}
	}
	s.render(w, r, "dish", http.StatusOK, v)
}

// source is where a card came from, for a line under its Label.
func source(rec *store.Record, lang string) string {
	if dom := card.Domain(rec.SourceURL); dom != "" {
		return i18n.T(lang, "home.from", dom, i18n.Date(lang, rec.CreatedAt))
	}
	return i18n.T(lang, "home.pasted", i18n.Date(lang, rec.CreatedAt))
}

// chipsPerKind is how many chips of one kind show before the rest
// collapse into "N more" (ADR 0017).
const chipsPerKind = 3

// chips are a Variant's differences from the Reference, in its own
// words: added, changed, then missing (red, not struck through).
func chips(d dishcmp.Diff, lang string) []chip {
	var out []chip
	add := func(kind string, texts []string) {
		for i, t := range texts {
			if i == chipsPerKind && len(texts) > chipsPerKind+1 {
				out = append(out, chip{kind, i18n.T(lang, "chip.more", len(texts)-chipsPerKind)})
				return
			}
			out = append(out, chip{kind, t})
		}
	}
	var a, c, m []string
	for _, in := range d.Added {
		a = append(a, "+ "+in.Item)
	}
	for _, ch := range d.Changed {
		c = append(c, changeText(ch, lang))
	}
	for _, in := range d.Missing {
		m = append(m, "− "+in.Item)
	}
	add("add", a)
	add("chg", c)
	add("del", m)
	return out
}

func changeText(ch dishcmp.Change, lang string) string {
	now, was := dishcmp.AmountText(ch.Now), dishcmp.AmountText(ch.Was)
	if ch.ItemChanged {
		if dishcmp.Amount(ch.Now.Qty, ch.Now.Unit) != dishcmp.Amount(ch.Was.Qty, ch.Was.Unit) {
			return i18n.T(lang, "chip.instead", strings.TrimSpace(now+" "+ch.Now.Item), strings.TrimSpace(was+" "+ch.Was.Item))
		}
		return i18n.T(lang, "chip.instead", ch.Now.Item, ch.Was.Item)
	}
	if now == "" {
		now = i18n.T(lang, "mark.no_amount")
	}
	if was == "" {
		was = i18n.T(lang, "mark.no_amount")
	}
	return i18n.T(lang, "chip.amount", ch.Now.Item, now, was)
}

// marks are the same differences drawn on the card itself, and the
// Reference's ingredients this Variant leaves out.
func marks(d dishcmp.Diff, lang string) (map[string]card.Mark, []string) {
	m := map[string]card.Mark{}
	for id, ch := range d.Marks {
		if ch.Added() {
			m[id] = card.Mark{Kind: "add", Text: i18n.T(lang, "mark.new")}
			continue
		}
		was := dishcmp.AmountText(ch.Was)
		if ch.ItemChanged {
			was = strings.TrimSpace(was + " " + ch.Was.Item)
		}
		if was == "" {
			was = i18n.T(lang, "mark.no_amount")
		}
		m[id] = card.Mark{Kind: "chg", Text: i18n.T(lang, "mark.was", was)}
	}
	var gone []string
	for _, in := range d.Missing {
		gone = append(gone, in.Line())
	}
	return m, gone
}

// dishOptions lists the other Dishes by name, for move and merge.
func (s *Server) dishOptions(ctx context.Context, lang, except string) []dishOption {
	ds, err := s.cfg.Store.Dishes(ctx)
	if err != nil {
		return nil
	}
	var out []dishOption
	for _, d := range ds {
		if d.ID == except || len(d.Variants) == 0 {
			continue
		}
		name, _ := d.Name(lang, "en")
		out = append(out, dishOption{d.ID, fmt.Sprintf("%s (%d)", name, len(d.Variants))})
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	return out
}

// ---- Owner tools (ADR 0017): promote, rename, merge, move, label, unfold ----

func (s *Server) handlePromote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, rec := r.PathValue("id"), r.PostFormValue("record")
	_, err := s.cfg.Store.UpdateDish(ctx, id, func(d *store.Dish) error {
		for _, v := range d.Variants {
			if v.Record == rec {
				d.Reference = rec
				return nil
			}
		}
		return errors.New("that card is not in this dish")
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cfg.Store.Update(ctx, rec, func(x *store.Record) error { x.Log("dish", "promoted to the reference of dish "+id); return nil })
	http.Redirect(w, r, "/dishes/"+id, http.StatusSeeOther)
}

func (s *Server) handleDishName(w http.ResponseWriter, r *http.Request) {
	id, lang := r.PathValue("id"), r.PostFormValue("lang")
	name := strings.TrimSpace(r.PostFormValue("name"))
	if !i18n.Has(lang) || len([]rune(name)) > 60 {
		http.Error(w, "a language from the list and a name of at most 60 characters", http.StatusBadRequest)
		return
	}
	_, err := s.cfg.Store.UpdateDish(r.Context(), id, func(d *store.Dish) error {
		if name == "" {
			delete(d.Names, lang)
			if len(d.Names) == 0 {
				return errors.New("a dish needs a name in at least one language")
			}
			return nil
		}
		if d.Names == nil {
			d.Names = map[string]string{}
		}
		d.Names[lang] = name
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, backTo(r, "/dishes/"+id), http.StatusSeeOther)
}

// handleMerge files every Variant of this Dish into another, each
// taking the next number there; the Trace keeps the old one.
func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, into := r.PathValue("id"), r.PostFormValue("into")
	src, err := s.cfg.Store.GetDish(ctx, from)
	if err != nil || from == into {
		http.Error(w, "pick another dish to merge into", http.StatusBadRequest)
		return
	}
	if _, err := s.cfg.Store.GetDish(ctx, into); err != nil {
		http.Error(w, "no such dish", http.StatusBadRequest)
		return
	}
	for _, v := range src.Variants {
		if err := s.refile(ctx, v.Record, into, fmt.Sprintf("merged from dish %s", from)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.cfg.Store.UpdateDish(ctx, into, func(d *store.Dish) error {
		for l, n := range src.Names {
			if d.Names[l] == "" {
				d.Names[l] = n
			}
		}
		return nil
	})
	s.cfg.Store.DeleteDish(ctx, from)
	http.Redirect(w, r, "/dishes/"+into, http.StatusSeeOther)
}

// handleMove files one card into another Dish, or a new one.
func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec := s.readyRecord(w, r)
	if rec == nil {
		return
	}
	to := r.PostFormValue("dish")
	if name := strings.TrimSpace(r.PostFormValue("new_name")); to == "" && name != "" {
		if len([]rune(name)) > 60 {
			http.Error(w, "a dish name is at most 60 characters", http.StatusBadRequest)
			return
		}
		d := &store.Dish{ID: store.NewID(), Names: map[string]string{rec.Recipe.Lang: name}, CreatedAt: time.Now().UTC()}
		if err := s.cfg.Store.CreateDish(ctx, d); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		to = d.ID
	} else if _, err := s.cfg.Store.GetDish(ctx, to); err != nil || to == rec.DishID {
		http.Error(w, "pick another dish, or name a new one", http.StatusBadRequest)
		return
	}
	if err := s.refile(ctx, rec.ID, to, "moved by the owner"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/recipes/"+rec.ID, http.StatusSeeOther)
}

// refile takes a card out of its Dish (an emptied Dish goes) and files it
// as the next Variant of another. Numbers are never reused (ADR 0017).
func (s *Server) refile(ctx context.Context, recID, to, why string) error {
	rec, err := s.cfg.Store.Get(ctx, recID)
	if err != nil {
		return err
	}
	if rec.DishID != "" && rec.DishID != to {
		if d, err := s.cfg.Store.UpdateDish(ctx, rec.DishID, func(d *store.Dish) error { d.Remove(recID); return nil }); err == nil && len(d.Variants) == 0 {
			s.cfg.Store.DeleteDish(ctx, d.ID)
		}
	}
	var n int
	if _, err := s.cfg.Store.UpdateDish(ctx, to, func(d *store.Dish) error { n = d.Add(recID); return nil }); err != nil {
		return err
	}
	_, err = s.cfg.Store.Update(ctx, recID, func(x *store.Record) error {
		was := "no dish"
		if x.DishID != "" {
			was = fmt.Sprintf("dish %s variant %d", x.DishID, x.Variant)
		}
		x.Log("dish", fmt.Sprintf("%s: was %s, now dish %s variant %d", why, was, to, n))
		x.DishID, x.Variant = to, n
		return nil
	})
	return err
}

func (s *Server) handleLabel(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(r.PostFormValue("label"))
	if label == "" || len([]rune(label)) > recipe.MaxLabelLen {
		http.Error(w, fmt.Sprintf("a label of 1 to %d characters", recipe.MaxLabelLen), http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	_, err := s.cfg.Store.Update(r.Context(), id, func(x *store.Record) error {
		if x.Recipe == nil {
			return errors.New("no card yet")
		}
		x.Log("label", fmt.Sprintf("owner relabelled %q → %q", x.Recipe.Label, label))
		x.Recipe.Label = label
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/recipes/"+id, http.StatusSeeOther)
}

// handleUnfold makes a folded copy a Variant of its own (ADR 0018); id
// is the copy, and it leaves its Variant's "Also found at".
func (s *Server) handleUnfold(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cp := s.record(w, r)
	if cp == nil {
		return
	}
	if cp.Status != store.Folded || cp.Recipe == nil {
		http.Error(w, "that card is not a folded copy", http.StatusBadRequest)
		return
	}
	variantID := cp.FoldedInto
	vr, err := s.cfg.Store.Update(ctx, variantID, func(x *store.Record) error {
		kept := x.AlsoAt[:0]
		for _, a := range x.AlsoAt {
			if a.Record != cp.ID {
				kept = append(kept, a)
			}
		}
		x.AlsoAt = kept
		x.Log("fold", "owner unfolded the copy "+cp.ID)
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.cfg.Store.Update(ctx, cp.ID, func(x *store.Record) error {
		x.Status, x.FoldedInto, x.DishID = store.Ready, "", ""
		x.Log("fold", "unfolded from "+variantID+" by the owner")
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if vr.DishID != "" {
		if err := s.refile(ctx, cp.ID, vr.DishID, "unfolded"); err != nil {
			log.Printf("unfold %s: %v", cp.ID, err)
		}
	}
	http.Redirect(w, r, "/recipes/"+cp.ID, http.StatusSeeOther)
}

// backTo is a same-site path from the form's "back", else def.
func backTo(r *http.Request, def string) string {
	b := r.PostFormValue("back")
	if strings.HasPrefix(b, "/") && !strings.HasPrefix(b, "//") && !strings.Contains(b, `\`) {
		return b
	}
	return def
}
