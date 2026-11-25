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

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(raw) < 2000
}

// handleCreate files a new recipe: honeypot, dedupe, per-IP cap, global
// cap, then a Run is dispatched and the visitor lands on the card page.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if err := r.ParseForm(); err != nil {
		s.home(w, r, http.StatusBadRequest, homeView{Error: "That form was too large."})
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
		v.Error = "Give me a recipe URL, or paste the recipe."
		s.home(w, r, http.StatusBadRequest, v)
		return
	}
	if v.URL != "" && !validURL(v.URL) {
		v.Error = "That doesn't look like a web address (it should start with https://)."
		s.home(w, r, http.StatusBadRequest, v)
		return
	}
	if len(v.Pasted) > maxPasted {
		v.Error = fmt.Sprintf("That's more than %d characters; paste just the recipe.", maxPasted)
		s.home(w, r, http.StatusBadRequest, v)
		return
	}
	ctx := r.Context()
	norm := NormURL(v.URL)
	if norm != "" && v.Pasted == "" {
		if existing, err := s.cfg.Store.ByNormURL(ctx, norm); err == nil {
			for _, e := range existing {
				if e.Status != store.Failed {
					http.Redirect(w, r, "/recipes/"+e.ID, http.StatusSeeOther)
					return
				}
			}
		}
	}
	now := time.Now().UTC()
	ipHash := s.ipHash(r)
	if !s.isOwner(r) {
		if _, ok, err := s.cfg.Store.Incr(ctx, tabulate.IPKey(now, ipHash), tabulate.DailyPerIP); err != nil {
			log.Printf("create: ip cap: %v", err)
		} else if !ok {
			v.Error = fmt.Sprintf("You've filed %d cards today — that's the limit per visitor. Come back tomorrow!", tabulate.DailyPerIP)
			s.home(w, r, http.StatusTooManyRequests, v)
			return
		}
	}
	if _, ok, err := s.cfg.Store.Incr(ctx, tabulate.RunsKey(now), tabulate.DailyRuns); err != nil {
		v.Error = "Couldn't reach the chest just now. Try again in a minute."
		s.home(w, r, http.StatusServiceUnavailable, v)
		return
	} else if !ok {
		v.Error = "Grampa has typed up all the cards he can for today. Come back tomorrow!"
		s.home(w, r, http.StatusTooManyRequests, v)
		return
	}
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
	if err := s.cfg.Store.Create(ctx, rec); err != nil {
		v.Error = "Couldn't file that card just now. Try again in a minute."
		s.home(w, r, http.StatusServiceUnavailable, v)
		return
	}
	s.cfg.Runner.Start(ctx, rec.ID)
	http.Redirect(w, r, "/recipes/"+rec.ID, http.StatusSeeOther)
}

type statusView struct {
	ID, State, Line, Hint string
	Polling               bool
}

func statusOf(rec *store.Record) statusView {
	v := statusView{ID: rec.ID, State: string(rec.Status)}
	switch rec.Status {
	case store.Dispatched, store.Running:
		v.Polling = true
		v.Line = "Reading the page…"
		for i := len(rec.Trace) - 1; i >= 0; i-- {
			switch rec.Trace[i].Kind {
			case "rejected":
				v.Line = "Re-typing a few lines so it fits on one card…"
			case "nudge":
				v.Line = "Nearly there…"
			case "tick":
				v.Line = "Picking up where we left off…"
			default:
				continue
			}
			break
		}
	case store.Failed:
		v.Line = rec.FailReason
		switch rec.FailKind {
		case "fetch_blocked":
			v.Hint = "The site wouldn't let me read it. Copy the recipe from the page and paste it into the form instead."
		case "not_a_recipe":
			v.Hint = "That page doesn't seem to hold a recipe."
		case "ambiguous":
			v.Hint = "If the page has several recipes, paste just the one you want."
		default:
			v.Hint = "Something went wrong on our side."
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
}

func (s *Server) handleRecipe(w http.ResponseWriter, r *http.Request) {
	rec := s.record(w, r)
	if rec == nil {
		return
	}
	v := recipeView{ID: rec.ID, Owner: s.isOwner(r), Failed: rec.Status == store.Failed, Status: statusOf(rec), Title: "A card in progress"}
	if rec.Status == store.Ready && rec.Recipe != nil {
		// A real visit wears the card a little (ADR 0011).
		ctx, cancel := bg()
		if upd, err := s.cfg.Store.Update(ctx, rec.ID, func(x *store.Record) error { x.Views++; return nil }); err == nil {
			rec = upd
		}
		cancel()
		rc := rec.Recipe
		v.Ready, v.MayNotFit = true, rec.MayNotFit
		v.Title, v.Slug, v.SourceURL = rc.Title, slug(rc.Title), rec.SourceURL
		v.SVG = template.HTML(card.SVG(rc, card.Options{ID: rec.ID, Views: rec.Views, Pasted: rec.WasPasted}))
		v.Credit = card.CreditLine(rc, rec.WasPasted)
		var meta []string
		if rc.Servings != "" {
			meta = append(meta, "Serves "+rc.Servings)
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
		v.Title = "Couldn't make this card"
	}
	s.render(w, r, s.tmplRecipe, http.StatusOK, v)
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
	if err := s.tmplFragments.ExecuteTemplate(w, "status", statusOf(rec)); err != nil {
		log.Printf("status fragment: %v", err)
	}
}

// cardETag changes when the drawing would: the recipe, the fit flag and
// whether the card has earned its well-used stain.
func cardETag(rec *store.Record, print bool) string {
	b, _ := json.Marshal(rec.Recipe)
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
	tag := cardETag(rec, false)
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Write(card.SVG(rec.Recipe, card.Options{ID: rec.ID, Views: rec.Views, Pasted: rec.WasPasted}))
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
	fit := card.Fit(rec.Recipe)
	v := printView{
		ID: rec.ID, Title: rec.Recipe.Title, Orientation: "portrait",
		WidthPt: fmt.Sprintf("%.1f", fit.Width), HeightPt: fmt.Sprintf("%.1f", fit.Height),
		Small: rec.MayNotFit || !fit.OK,
		SVG:   template.HTML(card.SVG(rec.Recipe, card.Options{Print: true, Pasted: rec.WasPasted})),
	}
	if fit.Landscape {
		v.Orientation = "landscape"
	}
	var buf strings.Builder
	if err := s.tmplPrint.ExecuteTemplate(&buf, "print", v); err != nil {
		log.Printf("print: %v", err)
		http.Error(w, "something went wrong drawing this page", http.StatusInternalServerError)
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
	if err := s.cfg.Store.Delete(r.Context(), r.PathValue("id")); err != nil && err != store.ErrNotFound {
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
