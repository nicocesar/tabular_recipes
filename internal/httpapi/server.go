// Package httpapi is the web layer: pages, the add form, the owner's
// workbench, and the service's own /work and /tick routes. Every route
// declares which credential admits it (rule 21, ADR 0009):
//
//	public      GET /, /recipes/{id}…, /healthz, /version, /static/…; POST /recipes
//	owner       POST /recipes/{id}/retry|delete, GET /admin…
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
	cfg                                                                       Config
	mux                                                                       *http.ServeMux
	assetv                                                                    string
	tmplHome, tmplRecipe, tmplLogin, tmplAdmin, tmplAdminRecipe, tmplNotFound *template.Template
	tmplPrint, tmplFragments                                                  *template.Template
}

// New parses every template up front: a broken template fails boot,
// never a request (web-layout.md).
func New(cfg Config) (*Server, error) {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	v, err := assetVersion(cfg.Assets)
	if err != nil {
		return nil, err
	}
	s.assetv = v
	funcs := template.FuncMap{"assetv": func() string { return s.assetv }}
	page := func(name string) (*template.Template, error) {
		return template.New("").Funcs(funcs).ParseFS(cfg.Assets, "templates/layout.html", "templates/"+name, "templates/fragments/*.html")
	}
	for _, p := range []struct {
		dst  **template.Template
		file string
	}{
		{&s.tmplHome, "home.html"}, {&s.tmplRecipe, "recipe.html"}, {&s.tmplLogin, "login.html"},
		{&s.tmplAdmin, "admin.html"}, {&s.tmplAdminRecipe, "admin_recipe.html"}, {&s.tmplNotFound, "notfound.html"},
	} {
		t, err := page(p.file)
		if err != nil {
			return nil, err
		}
		*p.dst = t
	}
	if s.tmplPrint, err = template.New("").Funcs(funcs).ParseFS(cfg.Assets, "templates/print.html"); err != nil {
		return nil, err
	}
	if s.tmplFragments, err = template.New("").Funcs(funcs).ParseFS(cfg.Assets, "templates/fragments/*.html"); err != nil {
		return nil, err
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

	m.HandleFunc("GET /login", s.handleLoginPage)
	m.HandleFunc("POST /login", s.handleLogin)
	m.HandleFunc("POST /logout", s.handleLogout)
	m.HandleFunc("GET /admin", s.ownerPage(s.handleAdmin))
	m.HandleFunc("GET /admin/recipes/{id}", s.ownerPage(s.handleAdminRecipe))

	m.HandleFunc("POST /work/{id}", s.tick(s.handleWork))
	m.HandleFunc("POST /tick", s.tick(s.handleTick))

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.notFound(w, r) })
}

// pageView wraps a page's own data with the nav built from the request.
type pageView struct {
	Nav  navView
	Page any
}

type navView struct{ Owner bool }

func (s *Server) render(w http.ResponseWriter, r *http.Request, t *template.Template, status int, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", pageView{Nav: navView{Owner: s.isOwner(r)}, Page: data}); err != nil {
		log.Printf("render: %v", err)
		http.Error(w, "something went wrong drawing this page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, s.tmplNotFound, http.StatusNotFound, nil)
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

type cardRow struct {
	ID, Title, Domain, Date, Badge string
	at                             time.Time
}

type homeView struct {
	Error, URL, Pasted, Source string
	Left, Cap                  int
	Cards                      []cardRow
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.home(w, r, http.StatusOK, homeView{})
}

func (s *Server) home(w http.ResponseWriter, r *http.Request, status int, v homeView) {
	ctx := r.Context()
	owner := s.isOwner(r)
	statuses := []store.Status{store.Ready}
	if owner {
		statuses = append(statuses, store.Dispatched, store.Running, store.Failed)
	}
	for _, st := range statuses {
		recs, err := s.cfg.Store.ByStatus(ctx, st)
		if err != nil {
			log.Printf("home: %v", err)
			continue
		}
		for _, rec := range recs {
			row := cardRow{ID: rec.ID, Domain: card.Domain(rec.SourceURL), at: rec.CreatedAt, Date: rec.CreatedAt.Format("Jan 2, 2006")}
			if rec.Recipe != nil {
				row.Title = rec.Recipe.Title
			} else {
				row.Title = firstNonEmpty(rec.SourceURL, rec.PastedSource, "pasted recipe")
			}
			if st != store.Ready {
				row.Badge = string(st)
			}
			v.Cards = append(v.Cards, row)
		}
	}
	sort.Slice(v.Cards, func(a, b int) bool { return v.Cards[a].at.After(v.Cards[b].at) })
	used, _ := s.cfg.Store.Count(ctx, tabulate.RunsKey(time.Now()))
	v.Cap = tabulate.DailyRuns
	v.Left = max(v.Cap-used, 0)
	s.render(w, r, s.tmplHome, status, v)
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
