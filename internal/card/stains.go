package card

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"strings"

	"golang.org/x/image/font/sfnt"
)

func sfntIndex(g int) sfnt.GlyphIndex { return sfnt.GlyphIndex(g) }

// filters: a faint paper grain for the whole card and a wobble that
// roughens stain edges so they read as liquid, not geometry.
const filters = `<filter id="grain" x="0" y="0" width="100%" height="100%"><feTurbulence type="fractalNoise" baseFrequency=".85" numOctaves="2" seed="3"/><feColorMatrix values="0 0 0 0 .36 0 0 0 0 .26 0 0 0 0 .16 0 0 0 .16 0"/></filter>` +
	`<filter id="wobble" x="-20%" y="-20%" width="140%" height="140%"><feTurbulence type="fractalNoise" baseFrequency=".07" numOctaves="2" seed="7"/><feDisplacementMap in="SourceGraphic" scale="5"/></filter>`

// WellUsedViews is when a Card earns its extra Stain (ADR 0011).
const WellUsedViews = 40

// stains draws one to three faint marks seeded from the recipe id, only
// where they cover no text and no table (ADR 0011). Screen only.
func stains(sh *sheet) string {
	if sh.opt.ID == "" {
		return ""
	}
	h := fnv.New64a()
	h.Write([]byte(sh.opt.ID))
	seed := h.Sum64()
	rng := rand.New(rand.NewPCG(seed, seed>>17|1))

	n := 1 + rng.IntN(2)
	if sh.opt.Views >= WellUsedViews {
		n++
	}
	if n > 3 {
		n = 3
	}

	// Everything a stain must not touch.
	var avoid []rect
	pad := 3.0
	grow := func(r rect) rect { return rect{r.x - pad, r.y - pad, r.w + 2*pad, r.h + 2*pad} }
	avoid = append(avoid, grow(sh.table))
	for _, tb := range sh.texts {
		w := maxWidth(tb.lines)
		x := tb.x
		if tb.center {
			x += (tb.w - w) / 2
		}
		top := tb.y - tb.size
		hgt := float64(len(tb.lines)-1)*tb.lead + tb.size*1.35
		avoid = append(avoid, grow(rect{x, top, w, hgt}))
	}
	clear := func(cx, cy, r float64) bool {
		b := rect{cx - r, cy - r, 2 * r, 2 * r}
		for _, a := range avoid {
			if a.overlaps(b) {
				return false
			}
		}
		return true
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<defs><clipPath id="cardclip"><rect x="0" y="0" width="%s" height="%s"/></clipPath></defs>`, trim(sh.W), trim(sh.H))
	b.WriteString(`<g class="stain" aria-hidden="true" clip-path="url(#cardclip)">`)

	kinds := []func(cx, cy, r float64){
		func(cx, cy, r float64) { // coffee ring, often doubled
			fmt.Fprintf(&b, `<g filter="url(#wobble)" fill="none" stroke="#6b3f1d"><circle cx="%s" cy="%s" r="%s" stroke-width="3.4" stroke-opacity=".12"/><circle cx="%s" cy="%s" r="%s" stroke-width=".9" stroke-opacity=".12"/>`,
				trim(cx), trim(cy), trim(r), trim(cx), trim(cy), trim(r-2.2))
			if rng.IntN(2) == 0 {
				a := rng.Float64() * 2 * math.Pi
				ox, oy := cx+math.Cos(a)*r*0.35, cy+math.Sin(a)*r*0.35
				fmt.Fprintf(&b, `<circle cx="%s" cy="%s" r="%s" stroke-width="2" stroke-opacity=".06" stroke-dasharray="%s %s"/>`,
					trim(ox), trim(oy), trim(r*0.95), trim(r*2.2), trim(r*1.4))
			}
			b.WriteString(`</g>`)
		},
		func(cx, cy, r float64) { // grease halo
			fmt.Fprintf(&b, `<g filter="url(#wobble)"><ellipse cx="%s" cy="%s" rx="%s" ry="%s" fill="#b07f2e" fill-opacity=".07"/><ellipse cx="%s" cy="%s" rx="%s" ry="%s" fill="none" stroke="#9a6a22" stroke-opacity=".08" stroke-width="1.2"/></g>`,
				trim(cx), trim(cy), trim(r*0.8), trim(r*0.62), trim(cx), trim(cy), trim(r*0.8), trim(r*0.62))
		},
		func(cx, cy, r float64) { // flour fingerprint
			rot := rng.Float64()*60 - 30
			fmt.Fprintf(&b, `<g transform="rotate(%s %s %s)" fill="none" stroke="#fffdf6" stroke-opacity=".55" stroke-width=".7">`, trim(rot), trim(cx), trim(cy))
			for k := 1; k <= 5; k++ {
				fr := float64(k) * r / 6
				fmt.Fprintf(&b, `<ellipse cx="%s" cy="%s" rx="%s" ry="%s"/>`, trim(cx), trim(cy), trim(fr*0.72), trim(fr))
			}
			b.WriteString(`</g>`)
		},
		func(cx, cy, r float64) { // tomato splash
			fmt.Fprintf(&b, `<g filter="url(#wobble)" fill="#b0473a"><circle cx="%s" cy="%s" r="%s" fill-opacity=".09"/>`, trim(cx), trim(cy), trim(r*0.55))
			for k := 0; k < 4; k++ {
				a := rng.Float64() * 2 * math.Pi
				d := r * (0.7 + rng.Float64()*0.3)
				fmt.Fprintf(&b, `<circle cx="%s" cy="%s" r="%s" fill-opacity=".1"/>`, trim(cx+math.Cos(a)*d), trim(cy+math.Sin(a)*d), trim(0.8+rng.Float64()*1.6))
			}
			b.WriteString(`</g>`)
		},
	}

	m := sh.margin
	for i := 0; i < n; i++ {
		if i == n-1 && rng.IntN(3) == 0 {
			// A pencil tick in the left margin, beside the table.
			x := m/2 - 4
			y := sh.table.y + rng.Float64()*math.Max(sh.table.h-12, 1)
			fmt.Fprintf(&b, `<path d="M%s %sl3.5 4.5 6-9" fill="none" stroke="#6e675e" stroke-opacity=".5" stroke-width=".9" stroke-linecap="round"/>`, trim(x), trim(y+5))
			continue
		}
		draw := kinds[rng.IntN(len(kinds))]
		for try := 0; try < 40; try++ {
			r := 12 + rng.Float64()*20
			var cx, cy float64
			switch rng.IntN(5) {
			case 0: // a corner, half off the card
				cx, cy = pick(rng, 0, sh.W), pick(rng, 0, sh.H)
				cx, cy = snap(cx, sh.W), snap(cy, sh.H)
			case 1: // beside the title
				cx = sh.titleR.x + sh.titleR.w - r - rng.Float64()*40
				cy = sh.titleR.y + rng.Float64()*sh.titleR.h
			case 2: // under the table, by the imprint
				cx = sh.content.x + rng.Float64()*sh.content.w
				cy = sh.H - m/2
			default: // the side margins
				if rng.IntN(2) == 0 {
					cx = m / 2
				} else {
					cx = sh.W - m/2
				}
				cy = m + rng.Float64()*(sh.H-2*m)
			}
			if clear(cx, cy, r) {
				draw(cx, cy, r)
				break
			}
		}
	}
	b.WriteString(`</g>`)
	return b.String()
}

func pick(rng *rand.Rand, lo, hi float64) float64 { return lo + rng.Float64()*(hi-lo) }

// snap pulls a coordinate to the nearer edge.
func snap(v, max float64) float64 {
	if v < max/2 {
		return 6
	}
	return max - 6
}
