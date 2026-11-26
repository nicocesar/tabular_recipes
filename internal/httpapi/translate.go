package httpapi

import (
	"log"
	"net/http"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
	"github.com/nicocesar/tabular_recipes/internal/tabulate"
)

// shown is a card in lang's words: its Overlay when it has one, else the
// original (ADR 0015). Chips, Labels and marks all read through it.
func (s *Server) shown(rec *store.Record, lang string) *recipe.Recipe {
	if ov := rec.Translations[lang]; ov != nil {
		return rec.Recipe.Apply(ov)
	}
	return rec.Recipe
}

// queryRecipe is the card a download or print URL names: ?lang= picks an
// Overlay, nothing picks the original. The URL alone decides, so caches
// never mix languages.
func queryRecipe(r *http.Request, rec *store.Record) (*recipe.Recipe, string) {
	if ov := rec.Translations[r.URL.Query().Get("lang")]; ov != nil {
		return rec.Recipe.Apply(ov), ov.From
	}
	return rec.Recipe, ""
}

// handleTranslate is any visitor's Translate button (ADR 0014): one
// translate job per (card, language), counted against the same caps as
// filing a card.
func (s *Server) handleTranslate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec := s.readyRecord(w, r)
	if rec == nil {
		return
	}
	back := "/recipes/" + rec.ID
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	to := r.PostFormValue("to")
	if !tabulate.Translatable(rec, to) {
		http.Redirect(w, r, back+"?lang="+to, http.StatusSeeOther)
		return
	}
	job := tabulate.NewTranslation(rec.ID, to)
	job.IPHash = s.ipHash(r)
	existing, err := s.cfg.Store.CreateUnique(ctx, tabulate.TranslateKey(rec.ID, to), job, func(e *store.Record) bool { return e.Status != store.Failed })
	if err != nil {
		http.Error(w, s.t(r, "err.unreachable"), http.StatusServiceUnavailable)
		return
	}
	if existing != nil {
		http.Redirect(w, r, back+"?lang="+to, http.StatusSeeOther)
		return
	}
	refuse := func() {
		if err := s.cfg.Store.Delete(ctx, job.ID); err != nil {
			log.Printf("translate: undo %s: %v", job.ID, err)
		}
		http.Redirect(w, r, back+"?lang="+to+"&terr=cap", http.StatusSeeOther)
	}
	now := job.CreatedAt
	if !s.isOwner(r) {
		if _, ok, err := s.cfg.Store.Incr(ctx, tabulate.IPKey(now, job.IPHash), tabulate.DailyPerIP); err == nil && !ok {
			refuse()
			return
		}
	}
	if _, ok, err := s.cfg.Store.Incr(ctx, tabulate.RunsKey(now), tabulate.DailyRuns); err != nil || !ok {
		refuse()
		return
	}
	if err := s.cfg.Runner.Started(ctx, job); err != nil {
		log.Printf("translate %s: %v", job.ID, err)
	}
	s.cfg.Runner.Start(ctx, job.ID)
	http.Redirect(w, r, back+"?lang="+to, http.StatusSeeOther)
}
