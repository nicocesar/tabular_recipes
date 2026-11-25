package card

import (
	"strings"
	"unicode"
)

// span is a run of text in one face and colour class.
type span struct {
	face  *face
	text  string
	class string // CSS class in the SVG: ink, red, pencil, blue
}

// word is an unbreakable token with the space that may follow it.
type word struct {
	face  *face
	text  string
	class string
	w     float64 // width at the wrap size
	space float64 // width of a following space in this face
}

// line is one wrapped line: words placed left to right.
type line struct {
	words []word
	w     float64
}

// numeric reports whether a token carries a number (a quantity, time or
// temperature): such tokens are set in the typewriter mono face.
func numeric(tok string) bool {
	// Courier Prime draws vulgar fractions tiny; leave those to the serif.
	if strings.ContainsAny(tok, "½¼¾⅓⅔⅛") {
		return false
	}
	for _, r := range tok {
		if unicode.IsDigit(r) || r == '°' {
			return true
		}
	}
	return false
}

// prose splits text into spans, putting numeric tokens in mono.
func prose(text string, base *face, class string) []span {
	var out []span
	for i, tok := range strings.Fields(text) {
		f := base
		if numeric(tok) {
			f = faceMono
		}
		if i > 0 {
			tok = " " + tok
		}
		out = append(out, span{face: f, text: tok, class: class})
	}
	return out
}

func words(spans []span, size float64) []word {
	var ws []word
	for _, s := range spans {
		for _, tok := range strings.Fields(s.text) {
			ws = append(ws, word{
				face: s.face, text: tok, class: s.class,
				w: s.face.width(tok, size),
				// Word gaps always use the serif's space: Courier's is a full
				// em-half and opens holes after every number.
				space: faceSerif.width(" ", size),
			})
		}
	}
	return ws
}

// wrap breaks spans into lines no wider than max (a single word wider
// than max gets a line of its own and overflows).
func wrap(spans []span, size, max float64) []line {
	var lines []line
	var cur line
	for _, w := range words(spans, size) {
		if len(cur.words) > 0 {
			prev := cur.words[len(cur.words)-1]
			if cur.w+prev.space+w.w > max {
				lines = append(lines, cur)
				cur = line{}
			} else {
				cur.w += prev.space
			}
		}
		cur.words = append(cur.words, w)
		cur.w += w.w
	}
	if len(cur.words) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// natural is the one-line width of spans.
func natural(spans []span, size float64) float64 {
	ls := wrap(spans, size, 1e9)
	if len(ls) == 0 {
		return 0
	}
	return ls[0].w
}

// longestWord is the width of the widest unbreakable token.
func longestWord(spans []span, size float64) float64 {
	var m float64
	for _, w := range words(spans, size) {
		if w.w > m {
			m = w.w
		}
	}
	return m
}

func maxWidth(ls []line) float64 {
	var m float64
	for _, l := range ls {
		if l.w > m {
			m = l.w
		}
	}
	return m
}
