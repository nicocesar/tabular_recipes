// Package httpapi is the web layer: pages, the add form, the owner's
// workbench, and the service's own /work and /tick routes. Every route
// declares which credential admits it (rule 21, ADR 0009):
//
//	public      GET /, /recipes/{id}…, /dishes/{id}, /healthz, /version, /static/…; POST /recipes, /lang
//	owner       POST /recipes/{id}/retry|delete|move|label|unfold, /dishes/{id}/reference|name|merge,
//	            /admin/backfill; GET /admin…
//	TICK_TOKEN  POST /work/{id}, POST /tick
package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/card"
	"github.com/nicocesar/tabular_recipes/internal/i18n"
	"github.com/nicocesar/tabular_recipes/internal/store"
	"github.com/nicocesar/tabular_recipes/internal/tabulate"
)

// Config is everything the server needs; main fills it from env.
type Config struct {
	Store         store.Store
	Runner        *tabulate.Runner
	Assets        fs.FS // cmd/server's embedded templates/ and static/
	TickToken     string
	OwnerPassword string
	SessionSecret string
	Version       string // version.txt
	Commit        string
	BuiltAt       string
}

type Server struct {
	cfg    Config
	mux    *http.ServeMux
	assetv string
	// pages[lang][name] is each page parsed with that language's "t"
	// bound (ADR 0016): names are the template files without ".html",
	// plus "print" and "fragments".
	pages map[string]map[string]*template.Template
}

var pageNames = []string{"home", "recipe", "dish", "login", "admin", "admin_recipe", "notfound"}

// New parses every template up front, once per language: a broken
// template fails boot, never a request (web-layout.md).
func New(cfg Config) (*Server, error) {
	s := &Server{cfg: cfg, mux: http.NewServeMux(), pages: map[string]map[string]*template.Template{}}
	v, err := assetVersion(cfg.Assets)
	if err != nil {
		return nil, err
	}
	s.assetv = v
	for _, lang := range i18n.Langs() {
		funcs := template.FuncMap{
			"assetv": func() string { return s.assetv },
			"t":      func(key string, args ...any) string { return i18n.T(lang, key, args...) },
			"lang":   func() string { return lang },
		}
		set := map[string]*template.Template{}
		for _, name := range pageNames {
			t, err := template.New("").Funcs(funcs).ParseFS(cfg.Assets, "templates/layout.html", "templates/"+name+".html", "templates/fragments/*.html")
			if err != nil {
				return nil, err
			}
			set[name] = t
		}
		if set["print"], err = template.New("").Funcs(funcs).ParseFS(cfg.Assets, "templates/print.html"); err != nil {
			return nil, err
		}
		if set["fragments"], err = template.New("").Funcs(funcs).ParseFS(cfg.Assets, "templates/fragments/*.html"); err != nil {
			return nil, err
		}
		s.pages[lang] = set
	}
	s.routes()
	return s, nil
}

// assetVersion hashes the CSS and JS so links bust caches on deploy.
// A new static CSS/JS file must be added to this list.
func assetVersion(assets fs.FS) (string, error) {
	h := sha256.New()
	for _, f := range []string{"static/style.css", "static/wakelock.js", "static/filing.js", "static/htmx.min.js"} {
		b, err := fs.ReadFile(assets, f)
		if err != nil {
			return "", err
		}
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:10], nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow") // ADR 0012
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	m := s.mux
	static, _ := fs.Sub(s.cfg.Assets, "static")
	cached := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=86400")
			h.ServeHTTP(w, r)
		})
	}
	m.Handle("GET /static/fonts/", cached(http.StripPrefix("/static/fonts/", http.FileServerFS(card.Fonts()))))
	m.Handle("GET /static/", cached(http.StripPrefix("/static/", http.FileServerFS(static))))

	// /healthz is the stack's name (rule 19), but Cloud Run's front end
	// reserves paths ending in "z" and answers them itself with a 404, so
	// from outside the check is /health.
	health := func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) }
	m.HandleFunc("GET /healthz", health)
	m.HandleFunc("GET /health", health)
	m.HandleFunc("GET /version", s.handleVersion)

	m.HandleFunc("GET /{$}", s.handleHome)
	m.HandleFunc("POST /recipes", s.handleCreate)
	m.HandleFunc("GET /recipes/{id}", s.handleRecipe)
	m.HandleFunc("GET /recipes/{id}/status", s.handleStatus)
	m.HandleFunc("GET /recipes/{id}/card.svg", s.handleSVG)
	m.HandleFunc("GET /recipes/{id}/print", s.handlePrint)
	m.HandleFunc("POST /recipes/{id}/retry", s.owner(s.handleRetry))
	m.HandleFunc("POST /recipes/{id}/delete", s.owner(s.handleDelete))
	m.HandleFunc("POST /recipes/{id}/move", s.owner(s.handleMove))
	m.HandleFunc("POST /recipes/{id}/label", s.owner(s.handleLabel))
	m.HandleFunc("POST /recipes/{id}/unfold", s.owner(s.handleUnfold))
	m.HandleFunc("GET /dishes/{id}", s.handleDish)
	m.HandleFunc("POST /dishes/{id}/reference", s.owner(s.handlePromote))
	m.HandleFunc("POST /dishes/{id}/name", s.owner(s.handleDishName))
	m.HandleFunc("POST /dishes/{id}/merge", s.owner(s.handleMerge))

	m.HandleFunc("POST /lang", s.handleLang)

	m.HandleFunc("GET /login", s.handleLoginPage)
	m.HandleFunc("POST /login", s.handleLogin)
	m.HandleFunc("POST /logout", s.handleLogout)
	m.HandleFunc("GET /admin", s.ownerPage(s.handleAdmin))
	m.HandleFunc("GET /admin/recipes/{id}", s.ownerPage(s.handleAdminRecipe))
	m.HandleFunc("POST /admin/backfill", s.owner(s.handleBackfill))

	m.HandleFunc("POST /work/{id}", s.tick(s.handleWork))
	m.HandleFunc("POST /tick", s.tick(s.handleTick))

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.notFound(w, r) })
}

// pageView wraps a page's own data with the nav built from the request.
type pageView struct {
	Nav  navView
	Page any
}

type navView struct {
	Owner bool
	Lang  string
	Here  string // the path the language switch returns to
	Langs []langOption
}

type langOption struct {
	Code, Name string
	Current    bool
}

// render draws page name in the visitor's language.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, status int, data any) {
	lang := s.lang(r)
	nav := navView{Owner: s.isOwner(r), Lang: lang, Here: r.URL.RequestURI()}
	for _, l := range i18n.Langs() {
		nav.Langs = append(nav.Langs, langOption{l, i18n.Name(l), l == lang})
	}
	var buf bytes.Buffer
	if err := s.pages[lang][name].ExecuteTemplate(&buf, "layout", pageView{Nav: nav, Page: data}); err != nil {
		log.Printf("render: %v", err)
		http.Error(w, i18n.T(lang, "err.render"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "notfound", http.StatusNotFound, nil)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	commit := s.cfg.Commit
	if commit == "" {
		commit = "dev"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(commit + "\n"))
}

// ---- The chest ----

// cardRow is one tab in the chest: a Dish with its Labels, or a card on
// its own (a one-card Dish, a card from before Dishes, or, for the
// Owner, one in flight or failed).
type cardRow struct {
	Href, Title, Domain, Date, Badge, BadgeText, Count string
	Labels                                             []labelRow
	More                                               string
	at                                                 time.Time
}

type labelRow struct {
	N          int
	Text, Lang string // Lang only when not the visitor's
}

type homeView struct {
	Error, URL, Pasted, Source string
	Left, Cap                  int
	Cards                      []cardRow
}

// homeLabels is how many Labels a Dish shows on the home page (ADR 0017).
const homeLabels = 3

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.home(w, r, http.StatusOK, homeView{})
}

func (s *Server) home(w http.ResponseWriter, r *http.Request, status int, v homeView) {
	ctx := r.Context()
	lang := s.lang(r)
	statuses := []store.Status{store.Ready}
	if s.isOwner(r) {
		statuses = append(statuses, store.Dispatched, store.Running, store.Failed)
	}
	ready := map[string]*store.Record{}
	cardRowOf := func(rec *store.Record) cardRow {
		row := cardRow{Href: "/recipes/" + rec.ID, Domain: card.Domain(rec.SourceURL), at: rec.CreatedAt, Date: i18n.Date(lang, rec.CreatedAt)}
		if rec.Recipe != nil {
			row.Title = rec.Recipe.Title
		} else {
			row.Title = firstNonEmpty(rec.SourceURL, rec.PastedSource, "pasted recipe")
		}
		if rec.Status != store.Ready {
			row.Badge, row.BadgeText = string(rec.Status), s.t(r, "badge."+string(rec.Status))
		}
		return row
	}
	for _, st := range statuses {
		recs, err := s.cfg.Store.ByStatus(ctx, st)
		if err != nil {
			log.Printf("home: %v", err)
			continue
		}
		for _, rec := range recs {
			if rec.Kind != store.KindCard {
				continue // jobs live on the workbench
			}
			if st == store.Ready && rec.DishID != "" && rec.Recipe != nil {
				ready[rec.ID] = rec
				continue
			}
			v.Cards = append(v.Cards, cardRowOf(rec))
		}
	}
	filed := map[string]bool{}
	if ds, err := s.cfg.Store.Dishes(ctx); err != nil {
		log.Printf("home: dishes: %v", err)
	} else {
		for _, d := range ds {
			var vs []variant
			var ref *store.Record
			for _, dv := range d.Variants {
				if rec := ready[dv.Record]; rec != nil {
					vs = append(vs, variant{dv.N, rec})
					filed[rec.ID] = true
					if rec.ID == d.Reference {
						ref = rec
					}
				}
			}
			switch len(vs) {
			case 0:
				continue
			case 1:
				v.Cards = append(v.Cards, cardRowOf(vs[0].Rec))
				continue
			}
			row := cardRow{Href: "/dishes/" + d.ID, Count: i18n.T(lang, "home.variants", len(vs))}
			row.Title, _ = d.Name(lang, refLang(ref))
			for i, x := range vs {
				row.at = later(row.at, x.Rec.CreatedAt)
				if i < homeLabels {
					l := labelRow{N: x.N, Text: x.Rec.Recipe.Label}
					if x.Rec.Recipe.Lang != lang {
						l.Lang = x.Rec.Recipe.Lang
					}
					row.Labels = append(row.Labels, l)
				}
			}
			if n := len(vs) - homeLabels; n > 0 {
				row.More = i18n.T(lang, "home.more", n)
			}
			v.Cards = append(v.Cards, row)
		}
	}
	for id, rec := range ready { // a Dish that lost track of its card
		if !filed[id] {
			v.Cards = append(v.Cards, cardRowOf(rec))
		}
	}
	sort.Slice(v.Cards, func(a, b int) bool { return v.Cards[a].at.After(v.Cards[b].at) })
	used, _ := s.cfg.Store.Count(ctx, tabulate.RunsKey(time.Now()))
	v.Cap = tabulate.DailyRuns
	v.Left = max(v.Cap-used, 0)
	s.render(w, r, "home", status, v)
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (s *Server) record(w http.ResponseWriter, r *http.Request) *store.Record {
	rec, err := s.cfg.Store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return nil
	}
	return rec
}

// bg is a short context for work a handler does after deciding.
func bg() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
