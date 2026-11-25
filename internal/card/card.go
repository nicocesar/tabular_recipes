package card

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
)

// Design units are points at 100%: cell text is set at cellSize, and the
// Fit check scales the whole Card so cell text lands at ≥ MinPrintPt.
const (
	cellSize  = 10.0
	cellLead  = 1.28 * cellSize
	padX      = 5.0
	padY      = 4.0
	titleSize = 20.0
	metaSize  = 9.0
	noteSize  = 8.5
	noteLead  = 11.5
	stampSize = 7.0
	minWidth  = 400.0
)

// Options tune one rendering.
type Options struct {
	// Print drops the paper tint, ruled lines and Stains and uses a
	// narrow margin: the clean typed card (ADR 0011).
	Print bool
	// ID seeds the Stains; Views earns a well-used Card one more.
	ID    string
	Views int
	// Pasted credits the Card "pasted by a visitor" (ADR 0012).
	Pasted bool
}

type rect struct{ x, y, w, h float64 }

func (a rect) overlaps(b rect) bool {
	return a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h
}

// textBlock is wrapped text placed with its first baseline at (x, y).
type textBlock struct {
	x, y   float64
	w      float64 // box width, for centering
	lines  []line
	size   float64
	lead   float64
	center bool
}

type cellBox struct {
	rect
	step bool
}

// sheet is a fully laid-out Card, ready to write.
type sheet struct {
	opt     Options
	title   string
	W, H    float64
	margin  float64
	content rect // title block to imprint, inside the margins
	table   rect
	titleR  rect
	ruleY   float64
	cells   []cellBox
	texts   []textBlock
	// blue ruled lines behind the title block (screen only)
	ruled []float64
}

func build(r *recipe.Recipe, opt Options) *sheet {
	sh := &sheet{opt: opt, title: r.Title, margin: 40}
	if opt.Print {
		sh.margin = 10
	}
	L := recipe.Lay(r)

	// Cell contents.
	spans := make([][]span, len(L.Cells))
	for i, c := range L.Cells {
		if c.Ingredient != nil {
			in := c.Ingredient
			var ss []span
			if q := strings.TrimSpace(strings.TrimSpace(in.Qty) + " " + strings.TrimSpace(in.Unit)); q != "" {
				for _, tok := range strings.Fields(q) {
					f := faceMono
					if strings.ContainsAny(tok, "½¼¾⅓⅔⅛") {
						f = faceSerif
					}
					ss = append(ss, span{f, " " + tok, "ink"})
				}
			}
			ss = append(ss, prose(in.Item, faceSerif, "ink")...)
			if n := strings.TrimSpace(in.Note); n != "" {
				ss = append(ss, prose(", "+n, faceItalic, "pencil")...)
			}
			// prose drops the leading comma's attachment: glue it back.
			spans[i] = glueComma(ss)
		} else {
			spans[i] = prose(c.Step.Text, faceSerif, "ink")
		}
	}

	// Column widths: ingredients from their longest line, each step
	// column from the steps that start in it, within sane bounds.
	cols := make([]float64, L.Cols)
	for i, c := range L.Cells {
		nat := natural(spans[i], cellSize) + 2*padX
		lw := longestWord(spans[i], cellSize) + 2*padX
		lo, hi := 64.0, 140.0
		if c.Ingredient != nil {
			lo, hi = 100, 190
			if c.ColSpan > 1 {
				continue // a late ingredient has room to the right anyway
			}
		}
		w := clamp(nat, lo, hi)
		if w < lw {
			w = lw
		}
		if w > cols[c.Col] {
			cols[c.Col] = w
		}
	}
	for i := range cols {
		if cols[i] == 0 {
			cols[i] = 64
		}
	}
	tw := sum(cols)
	if tw < minWidth && L.Cols > 1 {
		extra := (minWidth - tw) / float64(L.Cols-1)
		for i := 1; i < L.Cols; i++ {
			cols[i] += extra
		}
		tw = minWidth
	} else if tw < minWidth {
		cols[0] = minWidth
		tw = minWidth
	}
	colX := make([]float64, L.Cols+1)
	for i, w := range cols {
		colX[i+1] = colX[i] + w
	}

	// Wrap each cell, then size rows: single-row cells first, then
	// spanning cells top up the rows they cover, smallest spans first.
	wrapped := make([][]line, len(L.Cells))
	need := make([]float64, len(L.Cells))
	for i, c := range L.Cells {
		avail := colX[c.Col+c.ColSpan] - colX[c.Col] - 2*padX
		wrapped[i] = wrap(spans[i], cellSize, avail)
		n := len(wrapped[i])
		if n == 0 {
			n = 1
		}
		need[i] = float64(n)*cellLead + 2*padY
	}
	rows := make([]float64, L.Rows)
	for i := range rows {
		rows[i] = cellLead + 2*padY
	}
	order := make([]int, len(L.Cells))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return L.Cells[order[a]].RowSpan < L.Cells[order[b]].RowSpan })
	for _, i := range order {
		c := L.Cells[i]
		have := sum(rows[c.Row : c.Row+c.RowSpan])
		if need[i] > have {
			add := (need[i] - have) / float64(c.RowSpan)
			for k := c.Row; k < c.Row+c.RowSpan; k++ {
				rows[k] += add
			}
		}
	}
	rowY := make([]float64, L.Rows+1)
	for i, h := range rows {
		rowY[i+1] = rowY[i] + h
	}

	// Vertical flow: title block, table, notes, imprint.
	m := sh.margin
	cw := tw
	x0 := m
	y := m + 6

	titleLines := wrap([]span{{faceTitle, r.Title, "ink"}}, titleSize, cw)
	y += faceTitle.asc * titleSize / faceTitle.upm
	sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: cw, lines: titleLines, size: titleSize, lead: titleSize * 1.2})
	titleTop := m
	y += float64(len(titleLines)-1) * titleSize * 1.2
	y += 7
	sh.ruleY = y
	y += 5

	var meta []string
	if s := strings.TrimSpace(r.Servings); s != "" {
		meta = append(meta, "Serves "+s)
	}
	if s := strings.TrimSpace(r.Time); s != "" {
		meta = append(meta, s)
	}
	if len(meta) > 0 {
		y += metaSize
		sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: cw, lines: wrap([]span{{faceMono, strings.Join(meta, " · "), "ink"}}, metaSize, cw), size: metaSize, lead: metaSize * 1.3})
		y += 3
	}
	if credit := CreditLine(r, opt.Pasted); credit != "" {
		cl := wrap(prose(credit, faceItalic, "pencil"), noteSize, cw)
		y += noteSize + 1
		sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: cw, lines: cl, size: noteSize, lead: noteLead})
		y += float64(len(cl)-1) * noteLead
	}
	sh.titleR = rect{x0, titleTop, cw, y - titleTop}
	for ly := sh.ruleY + 13; ly < y+2; ly += 13 {
		sh.ruled = append(sh.ruled, ly)
	}
	y += 10

	// Table.
	ty := y
	sh.table = rect{x0, ty, tw, rowY[L.Rows]}
	for i, c := range L.Cells {
		box := rect{x0 + colX[c.Col], ty + rowY[c.Row], colX[c.Col+c.ColSpan] - colX[c.Col], rowY[c.Row+c.RowSpan] - rowY[c.Row]}
		sh.cells = append(sh.cells, cellBox{rect: box, step: c.Step != nil})
		n := float64(len(wrapped[i]))
		textH := n * cellLead
		first := box.y + (box.h-textH)/2 + cellLead/2 + cellSize*0.35
		sh.texts = append(sh.texts, textBlock{
			x: box.x + padX, y: first, w: box.w - 2*padX, lines: wrapped[i],
			size: cellSize, lead: cellLead, center: c.Step != nil,
		})
	}
	y = ty + rowY[L.Rows]

	// Notes.
	if len(r.Notes) > 0 {
		y += 6
		for _, n := range r.Notes {
			nl := wrap(prose("— "+n, faceItalic, "pencil"), noteSize, cw)
			y += noteLead
			sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: cw, lines: nl, size: noteSize, lead: noteLead})
			y += float64(len(nl)-1) * noteLead
		}
	}

	// Imprint.
	y += 16
	stamp := wrap([]span{{faceTitle, "From the chest of Grampa the Great Engineer", "red"}}, stampSize, cw)
	sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: cw, lines: stamp, size: stampSize, lead: stampSize, center: true})
	y += 4

	sh.content = rect{x0, m, cw, y - m}
	sh.W = cw + 2*m
	sh.H = y + m
	return sh
}

// glueComma joins a ", note" span's comma to the previous word so it
// doesn't float as its own token.
func glueComma(ss []span) []span {
	for i := range ss {
		if strings.HasPrefix(strings.TrimSpace(ss[i].text), ",") && i > 0 {
			ss[i-1].text += ","
			ss[i].text = " " + strings.TrimPrefix(strings.TrimSpace(ss[i].text), ",")
			if strings.TrimSpace(ss[i].text) == "" {
				ss = append(ss[:i], ss[i+1:]...)
			}
			break
		}
	}
	return ss
}

// CreditLine is "Adapted from “X” — domain · by Author" (ADR 0012).
func CreditLine(r *recipe.Recipe, pasted bool) string {
	c := r.Credit
	var parts []string
	src := strings.TrimSpace(c.SourceTitle)
	dom := Domain(c.URL)
	switch {
	case src != "" && dom != "":
		parts = append(parts, fmt.Sprintf("Adapted from “%s” — %s", src, dom))
	case dom != "":
		parts = append(parts, "Adapted from "+dom)
	case src != "":
		parts = append(parts, fmt.Sprintf("Adapted from “%s”", src))
	}
	if a := strings.TrimSpace(c.Author); a != "" {
		parts = append(parts, "by "+a)
	}
	if pasted && dom == "" {
		parts = append(parts, "pasted by a visitor")
	}
	return strings.Join(parts, " · ")
}

// Domain is the host of a URL without "www.", or "".
func Domain(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func sum(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
}

// SVG renders the Card. r must have passed recipe.Validate.
func SVG(r *recipe.Recipe, opt Options) []byte {
	return build(r, opt).write()
}

// Size is the Card's natural size in points.
func Size(r *recipe.Recipe, opt Options) (w, h float64) {
	sh := build(r, opt)
	return sh.W, sh.H
}

const screenStyle = `.paper{fill:var(--card,#F2E8D0)}.ink{fill:var(--ink,#3A2E26)}.red{fill:var(--red,#B0473A)}.pencil{fill:var(--pencil,#8A8278)}.stepcell{fill:var(--step,#E8DABB)}.ingcell{fill:none}.grid{fill:none;stroke:var(--ink,#3A2E26);stroke-width:.8}.rule{stroke:var(--red,#B0473A);stroke-width:1.2}.ruled{stroke:var(--blue,#6F8FB5);stroke-width:.5;opacity:.45}.grain{opacity:var(--grain,.35)}.stain{opacity:var(--stain,1)}`

const printStyle = `.paper{fill:#fff}.ink{fill:#1d1a17}.red{fill:#1d1a17}.pencil{fill:#55504a}.stepcell{fill:#efefef}.ingcell{fill:none}.grid{fill:none;stroke:#1d1a17;stroke-width:.8}.rule{stroke:#1d1a17;stroke-width:1}`

func (sh *sheet) write() []byte {
	var body bytes.Buffer
	used := map[*face]map[uint16]bool{}
	f := func(format string, a ...any) { fmt.Fprintf(&body, format, a...) }

	f(`<rect class="paper" x="0" y="0" width="%s" height="%s" rx="%s"/>`, trim(sh.W), trim(sh.H), trim(sh.margin/5))
	if !sh.opt.Print {
		f(`<rect class="grain" x="0" y="0" width="%s" height="%s" filter="url(#grain)"/>`, trim(sh.W), trim(sh.H))
		for _, ly := range sh.ruled {
			f(`<line class="ruled" x1="%s" y1="%s" x2="%s" y2="%s"/>`, trim(sh.titleR.x), trim(ly), trim(sh.titleR.x+sh.titleR.w), trim(ly))
		}
		body.WriteString(stains(sh))
	}
	f(`<line class="rule" x1="%s" y1="%s" x2="%s" y2="%s"/>`, trim(sh.titleR.x), trim(sh.ruleY), trim(sh.titleR.x+sh.titleR.w), trim(sh.ruleY))

	for _, c := range sh.cells {
		cls := "ingcell"
		if c.step {
			cls = "stepcell"
		}
		f(`<rect class="%s" x="%s" y="%s" width="%s" height="%s"/>`, cls, trim(c.x), trim(c.y), trim(c.w), trim(c.h))
	}
	for _, c := range sh.cells {
		f(`<rect class="grid" x="%s" y="%s" width="%s" height="%s"/>`, trim(c.x), trim(c.y), trim(c.w), trim(c.h))
	}
	f(`<rect class="grid" style="stroke-width:1.4" x="%s" y="%s" width="%s" height="%s"/>`, trim(sh.table.x), trim(sh.table.y), trim(sh.table.w), trim(sh.table.h))

	for _, tb := range sh.texts {
		for li, ln := range tb.lines {
			x := tb.x
			if tb.center {
				x += (tb.w - ln.w) / 2
			}
			by := tb.y + float64(li)*tb.lead
			for wi, w := range ln.words {
				writeWord(&body, used, w, x, by, tb.size)
				x += w.w
				if wi < len(ln.words)-1 {
					x += w.space
				}
			}
		}
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s" role="img" aria-labelledby="card-title">`,
		trim(sh.W), trim(sh.H), trim(sh.W), trim(sh.H))
	fmt.Fprintf(&out, `<title id="card-title">%s — recipe card</title>`, html.EscapeString(sh.title))
	style := screenStyle
	if sh.opt.Print {
		style = printStyle
	}
	fmt.Fprintf(&out, `<style>%s</style><defs>`, style)
	if !sh.opt.Print {
		out.WriteString(filters)
	}
	faces := make([]*face, 0, len(used))
	for fc := range used {
		faces = append(faces, fc)
	}
	sort.Slice(faces, func(a, b int) bool { return faces[a].id < faces[b].id })
	for _, fc := range faces {
		ids := make([]int, 0, len(used[fc]))
		for g := range used[fc] {
			ids = append(ids, int(g))
		}
		sort.Ints(ids)
		for _, g := range ids {
			if d := fc.pathData(sfntIndex(g)); d != "" {
				fmt.Fprintf(&out, `<path id="%s%d" d="%s"/>`, fc.id, g, d)
			}
		}
	}
	out.WriteString(`</defs>`)
	out.Write(body.Bytes())
	out.WriteString(`</svg>`)
	return out.Bytes()
}

func writeWord(b *bytes.Buffer, used map[*face]map[uint16]bool, w word, x, y, size float64) {
	fc := w.face
	if used[fc] == nil {
		used[fc] = map[uint16]bool{}
	}
	k := size / fc.upm
	fmt.Fprintf(b, `<g class="%s" transform="translate(%s %s) scale(%.5f)">`, w.class, trim(x), trim(y), k)
	var gx float64
	var prev glyph
	for i, r := range w.text {
		g := fc.lookup(r)
		if i > 0 {
			gx += fc.kern(prev.idx, g.idx)
		}
		used[fc][uint16(g.idx)] = true
		if gx == 0 {
			fmt.Fprintf(b, `<use href="#%s%d"/>`, fc.id, g.idx)
		} else {
			fmt.Fprintf(b, `<use href="#%s%d" x="%s"/>`, fc.id, g.idx, trim(gx))
		}
		gx += g.adv
		prev = g
	}
	b.WriteString(`</g>`)
}
