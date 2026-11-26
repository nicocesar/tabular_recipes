package card

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"

	"github.com/nicocesar/tabular_recipes/internal/i18n"
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
	// Marks tint ingredient cells against the Dish's Reference (ADR
	// 0017), keyed by ingredient id. Screen only: Print ignores them.
	Marks map[string]Mark
	// Print furniture (ADR 0019), drawn only when Print is set: the
	// "Dish · variant N · Label" line above the title, the "Also found
	// at" line under the credit, the short link after the imprint, and
	// its QR code beside the title block.
	Header, Also, Link string
	QR                 [][]bool // [y][x], true is a dark module
}

// QR geometry in card points: a version 3 code (29 modules) at 1.6 pt
// a module with a 2-module quiet zone is about 18 mm at scale 1.
const (
	qrModule = 1.6
	qrQuiet  = 2
)

// furniture is a stand-in for the print furniture, so the Fit checked
// at submission leaves room for it on paper.
var furniture = Options{
	Header: "A dish name of usual length · variant 12 · a label of usual length",
	Also:   "Also found at: example.com, example.org, example.net",
	Link:   "recipes.nicocesar.com/r/abcdefghijklm",
	QR:     blankQR(29),
}

func blankQR(n int) [][]bool {
	q := make([][]bool, n)
	for i := range q {
		q[i] = make([]bool, n)
	}
	return q
}

func (o Options) qrSide() float64 {
	if !o.Print || len(o.QR) == 0 {
		return 0
	}
	return float64(len(o.QR)+2*qrQuiet) * qrModule
}

// Mark is one ingredient's difference: Kind "add" or "chg", and the
// words to show after it ("new", "was 1 cup cream").
type Mark struct{ Kind, Text string }

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
	mark string // "add", "chg" or ""
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
	ruleW float64 // the red rule's length: short of the QR code
	qrX, qrY float64
}

// widths caps how wide a column may grow before its text wraps. Deep
// trees have many step columns, so narrower caps trade width for height
// (ADR 0004); chooseWidths picks the caps.
type widths struct{ step, ing float64 }

var widthSteps = []widths{
	{140, 190}, {124, 175}, {110, 160}, {96, 145}, {84, 130}, {72, 118}, {62, 108},
}

// chooseWidths keeps the widest caps whose printed size is within 3% of
// the best achievable, so a card that already fits keeps its natural
// look and a deep one wraps just enough.
func chooseWidths(r *recipe.Recipe) widths {
	best := 0.0
	scales := make([]float64, len(widthSteps))
	for i, w := range widthSteps {
		sh := buildWith(r, Options{Print: true}, w)
		scales[i] = min(printScale(sh.W, sh.H), 1.0)
		best = max(best, scales[i])
	}
	for i, s := range scales {
		if s >= best*0.97 {
			return widthSteps[i]
		}
	}
	return widthSteps[0]
}

func build(r *recipe.Recipe, opt Options) *sheet {
	return buildWith(r, opt, chooseWidths(r))
}

func buildWith(r *recipe.Recipe, opt Options, wc widths) *sheet {
	sh := &sheet{opt: opt, title: r.Title, margin: 52}
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
			if m, ok := opt.Marks[in.ID]; ok && !opt.Print && m.Text != "" {
				ss = append(ss, prose("· "+m.Text, faceItalic, m.Kind)...)
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
		lo, hi := min(64.0, wc.step), wc.step
		if c.Ingredient != nil {
			lo, hi = min(100, wc.ing), wc.ing
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
	titleTop := m
	// The title block narrows to leave the QR code its corner.
	qs := opt.qrSide()
	bw := cw
	if qs > 0 {
		bw = cw - qs - 6
		sh.qrX, sh.qrY = x0+cw-qs, titleTop
	}
	sh.ruleW = bw

	if h := strings.TrimSpace(opt.Header); h != "" && opt.Print {
		hl := wrap(prose(h, faceMono, "pencil"), metaSize, bw)
		y += metaSize - 4
		sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: bw, lines: hl, size: metaSize, lead: metaSize * 1.3})
		y += float64(len(hl)-1)*metaSize*1.3 + 6
	}

	titleLines := wrap([]span{{faceTitle, r.Title, "ink"}}, titleSize, bw)
	y += faceTitle.asc * titleSize / faceTitle.upm
	sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: bw, lines: titleLines, size: titleSize, lead: titleSize * 1.2})
	y += float64(len(titleLines)-1) * titleSize * 1.2
	y += 7
	sh.ruleY = y
	y += 5

	var meta []string
	if s := strings.TrimSpace(r.Servings); s != "" {
		meta = append(meta, i18n.T(r.Lang, "card.serves", s))
	}
	if s := strings.TrimSpace(r.Time); s != "" {
		meta = append(meta, s)
	}
	if len(meta) > 0 {
		y += metaSize
		sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: bw, lines: wrap([]span{{faceMono, strings.Join(meta, " · "), "ink"}}, metaSize, bw), size: metaSize, lead: metaSize * 1.3})
		y += 3
	}
	lines := []string{CreditLine(r, opt.Pasted)}
	if opt.Print {
		lines = append(lines, opt.Also)
	}
	for _, t := range lines {
		if strings.TrimSpace(t) == "" {
			continue
		}
		cl := wrap(prose(t, faceItalic, "pencil"), noteSize, bw)
		y += noteSize + 1
		sh.texts = append(sh.texts, textBlock{x: x0, y: y, w: bw, lines: cl, size: noteSize, lead: noteLead})
		y += float64(len(cl)-1) * noteLead
	}
	if qs > 0 && y < titleTop+qs {
		y = titleTop + qs
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
		cb := cellBox{rect: box, step: c.Step != nil}
		if c.Ingredient != nil && !opt.Print {
			cb.mark = opt.Marks[c.Ingredient.ID].Kind
		}
		sh.cells = append(sh.cells, cb)
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
	stampSpans := []span{{faceTitle, i18n.T(r.Lang, "card.stamp"), "red"}}
	if l := strings.TrimSpace(opt.Link); l != "" && opt.Print {
		stampSpans = append(stampSpans, span{faceMono, " · " + l, "red"})
	}
	stamp := wrap(stampSpans, stampSize, cw)
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

// CreditLine is "Adapted from “X” — domain · by Author" (ADR 0012), in
// the card's language.
func CreditLine(r *recipe.Recipe, pasted bool) string {
	c := r.Credit
	var parts []string
	src := strings.TrimSpace(c.SourceTitle)
	dom := Domain(c.URL)
	switch {
	case src != "" && dom != "":
		parts = append(parts, i18n.T(r.Lang, "card.adapted_full", src, dom))
	case dom != "":
		parts = append(parts, i18n.T(r.Lang, "card.adapted", dom))
	case src != "":
		parts = append(parts, i18n.T(r.Lang, "card.adapted_title", src))
	}
	if a := strings.TrimSpace(c.Author); a != "" {
		parts = append(parts, i18n.T(r.Lang, "card.by", a))
	}
	if pasted && dom == "" {
		parts = append(parts, i18n.T(r.Lang, "card.pasted"))
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
// qrPath draws the dark modules as one path, a run of each row at a time.
func qrPath(q [][]bool, x0, y0 float64) string {
	var b strings.Builder
	b.WriteString(`<path class="ink" d="`)
	for y, row := range q {
		for x := 0; x < len(row); {
			if !row[x] {
				x++
				continue
			}
			start := x
			for x < len(row) && row[x] {
				x++
			}
			fmt.Fprintf(&b, "M%s %sh%sv%sh-%sz", trim(x0+float64(start)*qrModule), trim(y0+float64(y)*qrModule),
				trim(float64(x-start)*qrModule), trim(qrModule), trim(float64(x-start)*qrModule))
		}
	}
	b.WriteString(`"/>`)
	return b.String()
}

func SVG(r *recipe.Recipe, opt Options) []byte {
	return build(r, opt).write()
}

// Size is the Card's natural size in points.
func Size(r *recipe.Recipe, opt Options) (w, h float64) {
	sh := build(r, opt)
	return sh.W, sh.H
}

const screenStyle = `.paper{fill:var(--card,#F2E8D0)}.ink{fill:var(--ink,#3A2E26)}.red{fill:var(--red,#B0473A)}.pencil{fill:var(--pencil,#8A8278)}.stepcell{fill:var(--step,#E8DABB)}.ingcell{fill:none}.grid{fill:none;stroke:var(--ink,#3A2E26);stroke-width:.8}.rule{stroke:var(--red,#B0473A);stroke-width:1.2}.ruled{stroke:var(--blue,#6F8FB5);stroke-width:.5;opacity:.45}.grain{opacity:var(--grain,.35)}.add{fill:var(--add,#3F7040)}.chg{fill:var(--chg,#9A6516)}.addcell{fill:var(--add-bg,rgba(63,112,64,.14))}.chgcell{fill:var(--chg-bg,rgba(185,130,46,.18))}.stain{opacity:var(--stain,1)}`

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
	f(`<line class="rule" x1="%s" y1="%s" x2="%s" y2="%s"/>`, trim(sh.titleR.x), trim(sh.ruleY), trim(sh.titleR.x+sh.ruleW), trim(sh.ruleY))
	if qs := sh.opt.qrSide(); qs > 0 {
		body.WriteString(qrPath(sh.opt.QR, sh.qrX+qrQuiet*qrModule, sh.qrY+qrQuiet*qrModule))
	}

	for _, c := range sh.cells {
		cls := "ingcell"
		if c.step {
			cls = "stepcell"
		}
		if c.mark != "" {
			cls = c.mark + "cell"
		}
		f(`<rect class="%s" x="%s" y="%s" width="%s" height="%s"/>`, cls, trim(c.x), trim(c.y), trim(c.w), trim(c.h))
		if c.mark != "" {
			f(`<rect class="%s" x="%s" y="%s" width="3" height="%s"/>`, c.mark, trim(c.x), trim(c.y), trim(c.h))
		}
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
