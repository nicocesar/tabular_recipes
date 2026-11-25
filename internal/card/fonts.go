// Package card draws a Recipe as a Card: an SVG whose text is glyph
// outlines measured and drawn from the same embedded fonts, so wrapping
// computed here is exactly what any viewer shows (ADR 0003).
package card

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/*.ttf fonts/*.txt fonts/*.md
var fontFiles embed.FS

// Fonts is the embedded font directory, served to the HTML chrome at
// /static/fonts/ so the pages and the Card share one type system.
func Fonts() fs.FS {
	sub, _ := fs.Sub(fontFiles, "fonts")
	return sub
}

// face is one loaded font. All metrics are in font units; callers scale
// by size/upm.
type face struct {
	id   string
	f    *sfnt.Font
	upm  float64
	mu   sync.Mutex
	buf  sfnt.Buffer
	glyp map[rune]glyph
	asc  float64 // ascent, font units (positive)
	desc float64 // descent, font units (positive)
}

type glyph struct {
	idx sfnt.GlyphIndex
	adv float64
}

var (
	faceTitle  = mustFace("t", "SpecialElite-Regular.ttf")
	faceSerif  = mustFace("s", "SourceSerif4SmText-Regular.ttf")
	faceItalic = mustFace("i", "SourceSerif4SmText-It.ttf")
	faceBold   = mustFace("b", "SourceSerif4SmText-Semibold.ttf")
	faceMono   = mustFace("m", "CourierPrime-Regular.ttf")
)

func mustFace(id, name string) *face {
	b, err := fontFiles.ReadFile("fonts/" + name)
	if err != nil {
		panic(err)
	}
	f, err := sfnt.Parse(b)
	if err != nil {
		panic(fmt.Sprintf("card: parse %s: %v", name, err))
	}
	fc := &face{id: id, f: f, upm: float64(f.UnitsPerEm()), glyp: map[rune]glyph{}}
	ppem := fixed.Int26_6(f.UnitsPerEm()) << 6
	m, err := f.Metrics(&fc.buf, ppem, font.HintingNone)
	if err != nil {
		panic(err)
	}
	fc.asc = float64(m.Ascent) / 64
	fc.desc = float64(m.Descent) / 64
	return fc
}

func (fc *face) ppem() fixed.Int26_6 { return fixed.Int26_6(fc.f.UnitsPerEm()) << 6 }

// lookup returns the glyph for r, falling back to '?' for runes the font
// lacks (a missing glyph would otherwise draw as an empty box).
func (fc *face) lookup(r rune) glyph {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.lookupLocked(r)
}

func (fc *face) lookupLocked(r rune) glyph {
	if g, ok := fc.glyp[r]; ok {
		return g
	}
	idx, err := fc.f.GlyphIndex(&fc.buf, r)
	if (err != nil || idx == 0) && r != '?' {
		g := fc.lookupLocked('?')
		fc.glyp[r] = g
		return g
	}
	adv, err := fc.f.GlyphAdvance(&fc.buf, idx, fc.ppem(), font.HintingNone)
	if err != nil {
		adv = 0
	}
	g := glyph{idx: idx, adv: float64(adv) / 64}
	fc.glyp[r] = g
	return g
}

// kern is the pair adjustment in font units, 0 when the font has none.
func (fc *face) kern(a, b sfnt.GlyphIndex) float64 {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	k, err := fc.f.Kern(&fc.buf, a, b, fc.ppem(), font.HintingNone)
	if err != nil {
		return 0
	}
	return float64(k) / 64
}

// width of s at size points.
func (fc *face) width(s string, size float64) float64 {
	var w float64
	var prev sfnt.GlyphIndex
	for i, r := range s {
		g := fc.lookup(r)
		if i > 0 {
			w += fc.kern(prev, g.idx)
		}
		w += g.adv
		prev = g.idx
	}
	return w * size / fc.upm
}

// pathData is the outline of one glyph in font units, y pointing down,
// as SVG path data.
func (fc *face) pathData(idx sfnt.GlyphIndex) string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	segs, err := fc.f.LoadGlyph(&fc.buf, idx, fc.ppem(), nil)
	if err != nil {
		return ""
	}
	var b strings.Builder
	p := func(v fixed.Int26_6) string { return trim(float64(v) / 64) }
	for _, s := range segs {
		a := s.Args
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			if b.Len() > 0 {
				b.WriteString("Z")
			}
			fmt.Fprintf(&b, "M%s %s", p(a[0].X), p(a[0].Y))
		case sfnt.SegmentOpLineTo:
			fmt.Fprintf(&b, "L%s %s", p(a[0].X), p(a[0].Y))
		case sfnt.SegmentOpQuadTo:
			fmt.Fprintf(&b, "Q%s %s %s %s", p(a[0].X), p(a[0].Y), p(a[1].X), p(a[1].Y))
		case sfnt.SegmentOpCubeTo:
			fmt.Fprintf(&b, "C%s %s %s %s %s %s", p(a[0].X), p(a[0].Y), p(a[1].X), p(a[1].Y), p(a[2].X), p(a[2].Y))
		}
	}
	if b.Len() > 0 {
		b.WriteString("Z")
	}
	return b.String()
}

// trim formats a coordinate with at most one decimal.
func trim(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	s = strings.TrimSuffix(s, ".0")
	if s == "-0" {
		s = "0"
	}
	return s
}
