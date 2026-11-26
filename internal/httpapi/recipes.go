package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/card"
	dishcmp "github.com/nicocesar/tabular_recipes/internal/dish"
	"github.com/nicocesar/tabular_recipes/internal/i18n"
	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
	"github.com/nicocesar/tabular_recipes/internal/tabulate"
)

const maxPasted = 30000

// NormURL is the dedupe key (ADR 0008): lowercase host without www, no
// fragment, no tracking parameters, no trailing slash.
func NormURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "fbclid" || lk == "gclid" || lk == "ref" {
			q.Del(k)
		}
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	s := host + path
	if enc := q.Encode(); enc != "" {
		s += "?" + enc
	}
	return s
}

// DedupeKey is what makes two submissions the same card: the pasted text
// with whitespace collapsed when there is any, else the normalised URL.
func DedupeKey(norm, pasted string) string {
	if pasted != "" {
		h := sha256.Sum256([]byte(strings.Join(strings.Fields(pasted), " ")))
		return "paste:" + hex.EncodeToString(h[:])
	}
	return "url:" + norm
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(raw) < 2000
}

// handleCreate files a new recipe: honeypot, dedupe, per-IP cap, global
// cap, then a Run is dispatched and the visitor lands on the card page.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if err := r.ParseForm(); err != nil {
		s.home(w, r, http.StatusBadRequest, homeView{Error: s.t(r, "err.too_large")})
		return
	}
	if r.PostFormValue("website") != "" { // honeypot: only bots fill it
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	v := homeView{
		URL:    strings.TrimSpace(r.PostFormValue("url")),
		Pasted: strings.TrimSpace(r.PostFormValue("pasted")),
		Source: strings.TrimSpace(r.PostFormValue("source")),
	}
	if v.URL == "" && v.Pasted == "" {
		v.Error = s.t(r, "err.no_input")
		s.home(w, r, http.StatusBadRequest, v)
		return
	}
	if v.URL != "" && !validURL(v.URL) {
		v.Error = s.t(r, "err.bad_url")
		s.home(w, r, http.StatusBadRequest, v)
		return
	}
	if len(v.Pasted) > maxPasted {
		v.Error = s.t(r, "err.too_long", maxPasted)
		s.home(w, r, http.StatusBadRequest, v)
		return
	}
	ctx := r.Context()
	norm := NormURL(v.URL)
	now := time.Now().UTC()
	ipHash := s.ipHash(r)
	rec := &store.Record{
		ID: store.NewID(), Status: store.Dispatched, CreatedAt: now, DispatchedAt: now,
		SourceURL: v.URL, NormURL: norm, Pasted: v.Pasted, PastedSource: v.Source,
		WasPasted: v.Pasted != "", IPHash: ipHash,
	}
	what := v.URL
	if v.Pasted != "" {
		what = fmt.Sprintf("pasted text (%d chars)", len(v.Pasted))
	}
	rec.Log("filed", "filed: "+what)
	// Dedupe and create in one transaction, so a double click or a
	// resubmitted form lands on the card already being typed (ADR 0008).
	existing, err := s.cfg.Store.CreateUnique(ctx, DedupeKey(norm, v.Pasted), rec, func(e *store.Record) bool { return e.Status != store.Failed })
	if err != nil {
		v.Error = s.t(r, "err.not_filed")
		s.home(w, r, http.StatusServiceUnavailable, v)
		return
	}
	if existing != nil {
		http.Redirect(w, r, "/recipes/"+existing.ID, http.StatusSeeOther)
		return
	}
	// Caps come after the dedupe so a repeat costs nothing; a capped
	// submission takes its record back out.
	refuse := func(status int, msg string) {
		if err := s.cfg.Store.Delete(ctx, rec.ID); err != nil {
			log.Printf("create: undo %s: %v", rec.ID, err)
		}
		v.Error = msg
		s.home(w, r, status, v)
	}
	if !s.isOwner(r) {
		if _, ok, err := s.cfg.Store.Incr(ctx, tabulate.IPKey(now, ipHash), tabulate.DailyPerIP); err != nil {
			log.Printf("create: ip cap: %v", err)
		} else if !ok {
			refuse(http.StatusTooManyRequests, s.t(r, "err.ip_cap", tabulate.DailyPerIP))
			return
		}
	}
	if _, ok, err := s.cfg.Store.Incr(ctx, tabulate.RunsKey(now), tabulate.DailyRuns); err != nil {
		refuse(http.StatusServiceUnavailable, s.t(r, "err.unreachable"))
		return
	} else if !ok {
		refuse(http.StatusTooManyRequests, s.t(r, "err.day_cap"))
		return
	}
	s.cfg.Runner.Start(ctx, rec.ID)
	http.Redirect(w, r, "/recipes/"+rec.ID, http.StatusSeeOther)
}

type statusView struct {
	ID, State, Heading, Line, Hint string
	Polling                        bool
}

func statusOf(rec *store.Record, lang string) statusView {
	t := func(k string) string { return i18n.T(lang, k) }
	v := statusView{ID: rec.ID, State: string(rec.Status), Heading: t("status.typing")}
	switch rec.Status {
	case store.Dispatched, store.Running:
		v.Polling = true
		v.Line = t("status.reading")
		if rec.Kind == store.KindTranslate {
			v.Heading, v.Line = t("status.translating_h"), t("status.translating")
		}
		for i := len(rec.Trace) - 1; i >= 0; i-- {
			switch rec.Trace[i].Kind {
			case "rejected":
				v.Line = t("status.refit")
			case "nudge":
				v.Line = t("status.nearly")
			case "tick":
				v.Line = t("status.resume")
			default:
				continue
			}
			break
		}
	case store.Failed:
		v.Line = rec.FailReason
		switch rec.FailKind {
		case "fetch_blocked":
			v.Hint = t("hint.fetch_blocked")
		case "not_a_recipe":
			v.Hint = t("hint.not_a_recipe")
		case "ambiguous":
			v.Hint = t("hint.ambiguous")
		default:
			v.Hint = t("hint.other")
		}
	}
	return v
}

type recipeView struct {
	ID, Title, Slug, SourceURL, Credit, Meta string
	SVG                                      template.HTML
	Ingredients, Steps, Notes                []string
	Ready, Failed, Owner, MayNotFit          bool
	Status                                   statusView
	Dish                                     *cardDish
	AlsoAt                                   []alsoRow
	Others                                   []dishOption // owner: move targets
	Label                                    string
	LabelMax                                 int
	// Translation (ADRs 0014, 0015). Query is "?lang=xx" when the card
	// shows an Overlay, for its print and download links.
	Query, Translated, InLang, TranslateTo, TranslateErr string
	CanTranslate, HasTranslation                         bool
	Translating                                          *statusView
}

// cardDish is the card's place in its Dish, and its marks (ADR 0017).
type cardDish struct {
	ID, Name, NameLang, Label, LabelLang string
	N, Count, RefN                       int
	Reference                            bool
	// Marks: CanMark when there is a Reference to mark against.
	CanMark, Marked bool
	Missing         []string
}

type alsoRow struct{ URL, Domain, Record string }

func (s *Server) handleRecipe(w http.ResponseWriter, r *http.Request) {
	rec := s.record(w, r)
	if rec == nil {
		return
	}
	if to := redirectOf(rec); to != "" {
		http.Redirect(w, r, "/recipes/"+to+keepQuery(r), http.StatusSeeOther)
		return
	}
	v := recipeView{ID: rec.ID, Owner: s.isOwner(r), Failed: rec.Status == store.Failed, Status: statusOf(rec, s.lang(r)), Title: s.t(r, "recipe.pending_title")}
	if rec.Status == store.Ready && rec.Recipe != nil {
		// A real visit wears the card a little (ADR 0011).
		ctx, cancel := bg()
		if upd, err := s.cfg.Store.Update(ctx, rec.ID, func(x *store.Record) error { x.Views++; return nil }); err == nil {
			rec = upd
		}
		cancel()
		lang := s.lang(r)
		rc := rec.Recipe
		opt := card.Options{ID: rec.ID, Views: rec.Views, Pasted: rec.WasPasted}
		if ov := rec.Translations[lang]; ov != nil {
			if r.URL.Query().Get("orig") == "1" {
				v.HasTranslation = true
			} else {
				rc = rec.Recipe.Apply(ov)
				opt.From = ov.From
				v.Query = "?lang=" + lang
				v.Translated = i18n.LangName(lang, ov.From)
			}
		}
		s.translationState(r, rec, &v)
		v.Ready, v.MayNotFit = true, rec.MayNotFit
		v.Title, v.Slug, v.SourceURL = rc.Title, slug(rc.Title), rec.SourceURL
		v.Dish = s.cardDish(r, rec, &opt)
		v.SVG = template.HTML(card.SVG(rc, opt))
		v.Label, v.LabelMax = rec.Recipe.Label, recipe.MaxLabelLen
		for _, a := range rec.AlsoAt {
			v.AlsoAt = append(v.AlsoAt, alsoRow{URL: a.URL, Domain: card.Domain(a.URL), Record: a.Record})
		}
		if v.Owner {
			except := ""
			if v.Dish != nil {
				except = v.Dish.ID
			}
			v.Others = s.dishOptions(r.Context(), s.lang(r), except)
		}
		v.Credit = card.CreditLine(rc, rec.WasPasted)
		if opt.From != "" {
			v.Credit += " · " + i18n.T(rc.Lang, "card.translated", i18n.LangName(rc.Lang, opt.From))
		}
		var meta []string
		if rc.Servings != "" {
			meta = append(meta, i18n.T(rc.Lang, "card.serves", rc.Servings))
		}
		if rc.Time != "" {
			meta = append(meta, rc.Time)
		}
		v.Meta = strings.Join(meta, " · ")
		for _, in := range rc.Ingredients {
			v.Ingredients = append(v.Ingredients, in.Line())
		}
		for _, st := range recipe.StepsInOrder(rc) {
			v.Steps = append(v.Steps, st.Text)
		}
		v.Notes = rc.Notes
	} else if rec.Status == store.Failed {
		v.Title = s.t(r, "recipe.failed_title")
	}
	s.render(w, r, "recipe", http.StatusOK, v)
}

// translationState fills the Translate button, a translation in flight
// and a failed one, for the visitor's language.
func (s *Server) translationState(r *http.Request, rec *store.Record, v *recipeView) {
	lang := s.lang(r)
	v.TranslateTo = lang
	v.InLang = i18n.LangName(lang, rec.Recipe.Lang)
	v.CanTranslate = tabulate.Translatable(rec, lang)
	if r.URL.Query().Get("terr") == "cap" {
		v.TranslateErr = s.t(r, "recipe.translate_cap")
	}
	jobID := rec.Translating[lang]
	if !v.CanTranslate || jobID == "" {
		return
	}
	job, err := s.cfg.Store.Get(r.Context(), jobID)
	if err != nil {
		return
	}
	switch job.Status {
	case store.Dispatched, store.Running:
		st := statusOf(job, lang)
		v.Translating, v.CanTranslate = &st, false
	case store.Failed:
		v.TranslateErr = s.t(r, "recipe.translate_failed", job.FailReason)
	}
}

// cardDish places a card in its Dish and, when the visit asks for marks
// (?marks=1, the Dish page's links), sets them on opt.
func (s *Server) cardDish(r *http.Request, rec *store.Record, opt *card.Options) *cardDish {
	if rec.DishID == "" {
		return nil
	}
	d, vs, ref, err := s.loadDish(r.Context(), rec.DishID)
	if err != nil || ref == nil {
		return nil
	}
	lang := s.lang(r)
	rc, refRc := s.shown(rec, lang), s.shown(ref, lang)
	cd := &cardDish{ID: d.ID, N: rec.Variant, Count: len(vs), Label: rc.Label, Reference: ref.ID == rec.ID}
	cd.Name, cd.NameLang = d.Name(lang, refLang(ref))
	if cd.NameLang == lang {
		cd.NameLang = ""
	}
	if rc.Lang != lang {
		cd.LabelLang = rc.Lang
	}
	cd.RefN = ref.Variant
	cd.CanMark = !cd.Reference && len(vs) > 1
	if cd.CanMark && r.URL.Query().Get("marks") == "1" {
		cd.Marked = true
		diff := dishcmp.Compare(refRc, rc)
		opt.Marks, cd.Missing = marks(diff, lang)
	}
	return cd
}

// handleStatus is the htmx poll: the status fragment, or HX-Refresh once
// the card is ready so the full page loads with it.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rec := s.record(w, r)
	if rec == nil {
		return
	}
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/recipes/"+rec.ID, http.StatusSeeOther)
		return
	}
	if rec.Status == store.Ready || rec.Status == store.Failed {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.pages[s.lang(r)]["fragments"].ExecuteTemplate(w, "status", statusOf(rec, s.lang(r))); err != nil {
		log.Printf("status fragment: %v", err)
	}
}

// cardETag changes when the drawing would: the recipe, the fit flag and
// whether the card has earned its well-used stain.
func cardETag(rec *store.Record, rc *recipe.Recipe, print bool) string {
	b, _ := json.Marshal(rc)
	h := sha256.New()
	h.Write(b)
	fmt.Fprintf(h, "|%v|%v|%v|%v", rec.MayNotFit, rec.Views >= card.WellUsedViews, print, rec.WasPasted)
	return `"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
}

func (s *Server) readyRecord(w http.ResponseWriter, r *http.Request) *store.Record {
	rec := s.record(w, r)
	if rec == nil {
		return nil
	}
	if rec.Status != store.Ready || rec.Recipe == nil {
		http.Redirect(w, r, "/recipes/"+rec.ID, http.StatusSeeOther)
		return nil
	}
	return rec
}

func (s *Server) handleSVG(w http.ResponseWriter, r *http.Request) {
	rec := s.readyRecord(w, r)
	if rec == nil {
		return
	}
	rc, from := queryRecipe(r, rec)
	tag := cardETag(rec, rc, false)
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Write(card.SVG(rc, card.Options{ID: rec.ID, Views: rec.Views, Pasted: rec.WasPasted, From: from}))
}

type printView struct {
	ID, Title, Orientation string
	WidthPt, HeightPt      string
	Small                  bool
	SVG                    template.HTML
}

func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	rec := s.readyRecord(w, r)
	if rec == nil {
		return
	}
	rc, from := queryRecipe(r, rec)
	opt := s.printOptions(r, rec)
	opt.From = from
	fit := card.FitWith(rc, opt)
	v := printView{
		ID: rec.ID, Title: rc.Title, Orientation: "portrait",
		WidthPt: fmt.Sprintf("%.1f", fit.Width), HeightPt: fmt.Sprintf("%.1f", fit.Height),
		Small: rec.MayNotFit || !fit.OK,
		SVG:   template.HTML(card.SVG(rc, opt)),
	}
	if fit.Landscape {
		v.Orientation = "landscape"
	}
	var buf strings.Builder
	if err := s.pages[s.lang(r)]["print"].ExecuteTemplate(&buf, "print", v); err != nil {
		log.Printf("print: %v", err)
		http.Error(w, s.t(r, "err.render"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok, err := s.cfg.Store.Incr(r.Context(), tabulate.RunsKey(time.Now()), tabulate.DailyRuns); err != nil || !ok {
		http.Error(w, "today's run cap is reached; try tomorrow", http.StatusTooManyRequests)
		return
	}
	if err := s.cfg.Runner.Retry(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/recipes/"+id, http.StatusSeeOther)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if rec, err := s.cfg.Store.Get(ctx, r.PathValue("id")); err == nil && rec.DishID != "" {
		// Leave the Dish; a removed Reference passes on, an empty Dish goes.
		if d, err := s.cfg.Store.UpdateDish(ctx, rec.DishID, func(d *store.Dish) error { d.Remove(rec.ID); return nil }); err == nil && len(d.Variants) == 0 {
			s.cfg.Store.DeleteDish(ctx, d.ID)
		}
	}
	if err := s.cfg.Store.Delete(ctx, r.PathValue("id")); err != nil && err != store.ErrNotFound {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(t string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(t), "-"), "-")
	if s == "" {
		return "recipe"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// redirectOf is where a record's page really is: a folded copy shows its
// Variant (ADR 0018), a job shows its card.
func redirectOf(rec *store.Record) string {
	switch {
	case rec.Status == store.Folded && rec.FoldedInto != "":
		return rec.FoldedInto
	case rec.Kind != store.KindCard && rec.Parent != "":
		return rec.Parent
	}
	return ""
}

func keepQuery(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return ""
	}
	return "?" + r.URL.RawQuery
}
