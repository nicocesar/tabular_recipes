package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	cookieName  = "owner"
	sessionLife = 180 * 24 * time.Hour
)

// sign is an HMAC of v under SESSION_SECRET.
func (s *Server) sign(v string) string {
	m := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	m.Write([]byte(v))
	return hex.EncodeToString(m.Sum(nil))
}

// isOwner reports whether the request carries a valid owner cookie.
// With no SESSION_SECRET or OWNER_PASSWORD configured there is no owner.
func (s *Server) isOwner(r *http.Request) bool {
	if s.cfg.SessionSecret == "" || s.cfg.OwnerPassword == "" {
		return false
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	exp, mac, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	if !hmac.Equal([]byte(mac), []byte(s.sign("owner|"+exp))) {
		return false
	}
	n, err := strconv.ParseInt(exp, 10, 64)
	return err == nil && time.Now().Unix() < n
}

// sameOrigin is the CSRF check for cookie-authenticated writes: a
// browser sends Origin on POST; it must be this host.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		// Old browsers omit Origin on same-origin POSTs; fall back to Referer.
		o = r.Header.Get("Referer")
		if o == "" {
			return false
		}
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

// owner admits owner writes (cookie + same origin).
func (s *Server) owner(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isOwner(r) {
			http.Error(w, "owner only", http.StatusForbidden)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// ownerPage admits owner reads, sending others to the login page.
func (s *Server) ownerPage(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isOwner(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h(w, r)
	}
}

// tick admits the service's own unattended callers (rule 22). An unset
// TICK_TOKEN admits nobody: an empty Bearer is never valid.
func (s *Server) tick(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.cfg.TickToken == "" || !ok || got == "" ||
			subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.TickToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

type loginView struct{ Error string }

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.isOwner(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.render(w, r, s.tmplLogin, http.StatusOK, loginView{})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	pw := r.FormValue("password")
	if s.cfg.OwnerPassword == "" || s.cfg.SessionSecret == "" {
		s.render(w, r, s.tmplLogin, http.StatusServiceUnavailable, loginView{Error: "The owner login isn't configured on this server."})
		return
	}
	// Compare digests so the comparison is constant-time regardless of length.
	a, b := sha256.Sum256([]byte(pw)), sha256.Sum256([]byte(s.cfg.OwnerPassword))
	if subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		time.Sleep(400 * time.Millisecond)
		s.render(w, r, s.tmplLogin, http.StatusUnauthorized, loginView{Error: "That's not the passphrase."})
		return
	}
	exp := strconv.FormatInt(time.Now().Add(sessionLife).Unix(), 10)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: exp + "." + s.sign("owner|"+exp), Path: "/",
		MaxAge: int(sessionLife.Seconds()), HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// clientIP is the first X-Forwarded-For hop (Cloud Run's front end sets
// it) or the socket peer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(ip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipHash keys the per-IP cap without storing the address.
func (s *Server) ipHash(r *http.Request) string {
	return s.sign("ip|" + clientIP(r))[:16]
}
