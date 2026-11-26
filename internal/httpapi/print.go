package httpapi

import (
	"log"
	"net/http"
	"strings"

	"rsc.io/qr"

	"github.com/nicocesar/tabular_recipes/internal/card"
	"github.com/nicocesar/tabular_recipes/internal/store"
)

// The print's way back to the card (ADR 0019): a short link that never
// changes, and its QR code.

// maxAlso is how many "Also found at" sites a print names.
const maxAlso = 4

// handleShort is /r/{id}, case-insensitive so the QR code can be
// uppercase (alphanumeric mode, a smaller code).
func (s *Server) handleShort(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/recipes/"+strings.ToLower(r.PathValue("id")), http.StatusMovedPermanently)
}

// shortLink is the printed link and the text its QR code encodes, from
// the request's own host (rule 17).
func shortLink(r *http.Request, id string) (shown, encoded string) {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	shown = r.Host + "/r/" + id
	return shown, strings.ToUpper(scheme + "://" + shown)
}

func qrModules(text string) [][]bool {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		log.Printf("qr %q: %v", text, err)
		return nil
	}
	out := make([][]bool, c.Size)
	for y := range out {
		out[y] = make([]bool, c.Size)
		for x := range out[y] {
			out[y][x] = c.Black(x, y)
		}
	}
	return out
}

// printOptions are the card's print furniture: its place in its Dish,
// where else it was found, and the way back.
func (s *Server) printOptions(r *http.Request, rec *store.Record) card.Options {
	opt := card.Options{Print: true, Pasted: rec.WasPasted}
	if cd := s.cardDish(r, rec, &card.Options{}); cd != nil {
		parts := []string{cd.Name}
		if cd.Count > 1 {
			parts = append(parts, s.t(r, "dish.variant", cd.N))
		}
		if cd.Label != "" {
			parts = append(parts, cd.Label)
		}
		opt.Header = strings.Join(parts, " · ")
	}
	var sites []string
	seen := map[string]bool{}
	for _, a := range rec.AlsoAt {
		d := card.Domain(a.URL)
		if d == "" {
			d = s.t(r, "card.pasted")
		}
		if !seen[d] {
			seen[d] = true
			sites = append(sites, d)
		}
	}
	if len(sites) > 0 {
		more := ""
		if len(sites) > maxAlso {
			more = " " + s.t(r, "home.more", len(sites)-maxAlso)
			sites = sites[:maxAlso]
		}
		opt.Also = s.t(r, "recipe.also") + " " + strings.Join(sites, ", ") + more
	}
	shown, encoded := shortLink(r, rec.ID)
	opt.Link = shown
	opt.QR = qrModules(encoded)
	return opt
}
