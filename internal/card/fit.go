package card

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
)

// The printable area of a US Letter page with 0.4 in margins, in points.
const (
	pageShort  = (8.5 - 0.8) * 72
	pageLong   = (11 - 0.8) * 72
	MinPrintPt = 8.0
	maxScale   = 1.3 // small recipes print larger, but not poster-sized
)

// FitResult says how the Card prints on one Letter page (ADR 0004).
type FitResult struct {
	OK        bool
	Landscape bool
	Scale     float64 // applied to the print Card's natural size
	CellPt    float64 // cell text size on paper
	Width     float64 // printed size in points
	Height    float64
	Message   string // for the agent when !OK
}

// Fit computes the best orientation and checks the 8 pt floor.
func Fit(r *recipe.Recipe) FitResult {
	w, h := Size(r, Options{Print: true})
	portrait := min(pageShort/w, pageLong/h)
	landscape := min(pageLong/w, pageShort/h)
	res := FitResult{Scale: portrait}
	if landscape > portrait {
		res = FitResult{Scale: landscape, Landscape: true}
	}
	if res.Scale > maxScale {
		res.Scale = maxScale
	}
	res.CellPt = cellSize * res.Scale
	res.Width, res.Height = w*res.Scale, h*res.Scale
	res.OK = res.CellPt >= MinPrintPt-1e-9
	if !res.OK {
		res.Message = fitMessage(r, w, h, res)
	}
	return res
}

func fitMessage(r *recipe.Recipe, w, h float64, res FitResult) string {
	factor := MinPrintPt / res.CellPt
	// Which dimension binds in the chosen orientation?
	pw, ph := pageShort, pageLong
	if res.Landscape {
		pw, ph = pageLong, pageShort
	}
	tooWide := w/pw >= h/ph

	var b strings.Builder
	fmt.Fprintf(&b, "The card does not fit one Letter page: cell text would print at %.1f pt (minimum %.0f pt), so it is %.1f× too %s. ",
		res.CellPt, MinPrintPt, factor, map[bool]string{true: "wide", false: "tall"}[tooWide])
	if tooWide {
		b.WriteString("Shorten step text to about 6 words each (keep times and temperatures; move explanations to notes), and merge consecutive steps that act on the same thing to reduce the number of columns.")
	} else {
		b.WriteString("Reduce rows and lines: shorten ingredient notes, combine minor ingredients (e.g. \"salt and pepper\"), and shorten or drop notes.")
	}
	type st struct {
		id    string
		words int
	}
	var ss []st
	for _, s := range r.Steps {
		ss = append(ss, st{s.ID, len(strings.Fields(s.Text))})
	}
	sort.SliceStable(ss, func(a, b int) bool { return ss[a].words > ss[b].words })
	var longest []string
	for i := 0; i < len(ss) && i < 3; i++ {
		if ss[i].words > 6 {
			longest = append(longest, fmt.Sprintf("%s (%d words)", ss[i].id, ss[i].words))
		}
	}
	if len(longest) > 0 {
		b.WriteString(" Longest steps: " + strings.Join(longest, ", ") + ".")
	}
	return b.String()
}
