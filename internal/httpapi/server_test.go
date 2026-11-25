package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

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
