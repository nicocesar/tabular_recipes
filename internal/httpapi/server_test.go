package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/store"
	"github.com/nicocesar/tabular_recipes/internal/tabulate"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{
		Store: st, Runner: &tabulate.Runner{Store: st, API: tabulate.NewDemo(), WorkURL: "http://127.0.0.1:1"},
		Assets: os.DirFS("../../cmd/server"), TickToken: "tok", OwnerPassword: "pw", SessionSecret: "sec",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNormURL(t *testing.T) {
	a := NormURL("https://www.Example.com/pancakes/?utm_source=x&b=1#top")
	b := NormURL("http://example.com/pancakes?b=1")
	if a != b || a != "example.com/pancakes?b=1" {
		t.Fatalf("%q vs %q", a, b)
	}
}

func TestTickNeedsToken(t *testing.T) {
	s := newServer(t)
	for _, h := range []string{"", "Bearer ", "Bearer nope"} {
		req := httptest.NewRequest("POST", "/tick", nil)
		if h != "" {
			req.Header.Set("Authorization", h)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%q: %d", h, w.Code)
		}
	}
	req := httptest.NewRequest("POST", "/tick", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestPagesRender(t *testing.T) {
	s := newServer(t)
	for _, p := range []string{"/", "/login", "/nope", "/healthz", "/health", "/version", "/static/style.css", "/static/fonts/SpecialElite-Regular.ttf"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code >= 500 || (p != "/nope" && w.Code != 200) {
			t.Fatalf("%s: %d", p, w.Code)
		}
		if w.Header().Get("X-Robots-Tag") == "" {
			t.Fatalf("%s: no noindex", p)
		}
	}
}

func TestCreateCapsPerIP(t *testing.T) {
	s := newServer(t)
	post := func(u string) int {
		req := httptest.NewRequest("POST", "/recipes", strings.NewReader(url.Values{"url": {u}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < tabulate.DailyPerIP; i++ {
		if c := post("https://example.com/r" + string(rune('a'+i))); c != http.StatusSeeOther {
			t.Fatalf("post %d: %d", i, c)
		}
	}
	if c := post("https://example.com/one-too-many"); c != http.StatusTooManyRequests {
		t.Fatalf("cap not enforced: %d", c)
	}
}

// A double click on the paste form files one card: both submissions land
// on it, and only one counts against the caps.
func TestCreateDedupesPastes(t *testing.T) {
	s := newServer(t)
	post := func(pasted string) string {
		req := httptest.NewRequest("POST", "/recipes", strings.NewReader(url.Values{"pasted": {pasted}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("post: %d", w.Code)
		}
		return w.Header().Get("Location")
	}
	var wg sync.WaitGroup
	locs := make([]string, 3)
	texts := []string{"Chipá\n500 g almidón\nhornear", "Chipá 500 g almidón hornear", "  Chipá\t500 g  almidón\n\nhornear "}
	for i := range locs {
		wg.Add(1)
		go func() { defer wg.Done(); locs[i] = post(texts[i]) }()
	}
	wg.Wait()
	if locs[0] != locs[1] || locs[1] != locs[2] {
		t.Fatalf("filed more than one card: %v", locs)
	}
	if other := post("Brownies\ncacao"); other == locs[0] {
		t.Fatal("different text landed on the same card")
	}
	if n, _ := s.cfg.Store.Count(t.Context(), tabulate.RunsKey(time.Now())); n != 2 {
		t.Fatalf("runs counted %d, want 2", n)
	}
}

// The page language comes from ?lang=, else the cookie, else
// Accept-Language (ADR 0016); the switch sets the cookie.
func TestPageLanguage(t *testing.T) {
	s := newServer(t)
	get := func(path string, hdr map[string]string) string {
		req := httptest.NewRequest("GET", path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w.Body.String()
	}
	if b := get("/", nil); !strings.Contains(b, "File a new recipe") || !strings.Contains(b, `lang="en"`) {
		t.Error("default should be English")
	}
	if b := get("/", map[string]string{"Accept-Language": "es-AR,es;q=0.9"}); !strings.Contains(b, "Archivar una receta nueva") || !strings.Contains(b, `lang="es"`) {
		t.Error("Accept-Language es should give Spanish")
	}
	if b := get("/?lang=en", map[string]string{"Accept-Language": "es", "Cookie": "lang=es"}); !strings.Contains(b, "File a new recipe") {
		t.Error("?lang= should win")
	}
	if b := get("/", map[string]string{"Accept-Language": "en", "Cookie": "lang=es"}); !strings.Contains(b, "Archivar") {
		t.Error("cookie should beat Accept-Language")
	}

	req := httptest.NewRequest("POST", "/lang", strings.NewReader(url.Values{"to": {"es"}, "back": {"/recipes/x?lang=en"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/recipes/x" {
		t.Fatalf("switch: %d %q", w.Code, w.Header().Get("Location"))
	}
	if c := w.Result().Cookies(); len(c) != 1 || c[0].Value != "es" {
		t.Fatalf("switch cookie: %v", c)
	}
	req = httptest.NewRequest("POST", "/lang", strings.NewReader(url.Values{"to": {"es"}, "back": {"//evil.example"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Header().Get("Location") != "/" {
		t.Fatalf("open redirect: %q", w.Header().Get("Location"))
	}
}
