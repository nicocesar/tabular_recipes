package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/nicocesar/tabular_recipes/internal/i18n"
)

const langCookie = "lang"

// lang is the visitor's page language (ADR 0016): ?lang= for one view,
// else the switch's cookie, else Accept-Language, else English.
func (s *Server) lang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); i18n.Has(l) {
		return l
	}
	if c, err := r.Cookie(langCookie); err == nil && i18n.Has(c.Value) {
		return c.Value
	}
	if l := i18n.Match(r.Header.Get("Accept-Language")); l != "" {
		return l
	}
	return i18n.Default
}

// t formats key in the visitor's language.
func (s *Server) t(r *http.Request, key string, args ...any) string {
	return i18n.T(s.lang(r), key, args...)
}

// handleLang is the footer switch: set the cookie, go back where we were.
func (s *Server) handleLang(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	if l := r.PostFormValue("to"); i18n.Has(l) {
		http.SetCookie(w, &http.Cookie{
			Name: langCookie, Value: l, Path: "/", MaxAge: 400 * 24 * 3600,
			SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		})
	}
	back := r.PostFormValue("back")
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.HasPrefix(back, "/\\") {
		back = "/"
	}
	// A ?lang= in the page we came from would outvote the new choice.
	if u, err := url.Parse(back); err == nil {
		q := u.Query()
		q.Del("lang")
		u.RawQuery = q.Encode()
		back = u.RequestURI()
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
