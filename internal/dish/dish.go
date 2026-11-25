// Package dish compares Variants of a Dish: whether a new card is a copy
// of an existing one (ADR 0018) and what differs from the Reference
// (ADR 0017). It works only on canonical names and amounts; steps are
// never compared, because the same text tabulated twice gives different
// trees.
package dish

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
)

// Slack is how much a copy may differ and still fold (ADR 0018).
const (
	SlackIngredients = 1
	SlackAmounts     = 1
	minShared        = 3 // too few ingredients to judge
)

// Same reports whether b is a copy of a, and the comparison in words for
// the Trace.
func Same(a, b *recipe.Recipe) (bool, string) {
	ma, okA := amounts(a)
	mb, okB := amounts(b)
	if !okA || !okB {
		return false, "not comparable: a card has ingredients without a canonical name"
	}
	var onlyA, onlyB, shared, differ []string
	for c := range ma {
		if _, ok := mb[c]; ok {
			shared = append(shared, c)
			if ma[c] != mb[c] {
				differ = append(differ, c)
			}
		} else {
			onlyA = append(onlyA, c)
		}
	}
	for c := range mb {
		if _, ok := ma[c]; !ok {
			onlyB = append(onlyB, c)
		}
	}
	sort.Strings(differ)
	total := len(shared) + len(onlyA) + len(onlyB)
	report := fmt.Sprintf("%d/%d ingredients shared, %d/%d amounts equal", len(shared), total, len(shared)-len(differ), len(shared))
	if len(differ) > 0 {
		report += " (differ: " + strings.Join(differ, ", ") + ")"
	}
	same := len(onlyA)+len(onlyB) <= SlackIngredients && len(differ) <= SlackAmounts && len(shared) >= minShared
	return same, report
}

// amounts maps each canonical name to its normalized amounts; an
// ingredient used twice (butter for the pan and the batter) keeps both.
func amounts(r *recipe.Recipe) (map[string]string, bool) {
	lists := map[string][]string{}
	for _, in := range r.Ingredients {
		c := strings.TrimSpace(in.Canon)
		if c == "" {
			return nil, false
		}
		lists[c] = append(lists[c], Amount(in.Qty, in.Unit))
	}
	out := map[string]string{}
	for c, l := range lists {
		sort.Strings(l)
		out[c] = strings.Join(l, "+")
	}
	return out, true
}

var fractions = map[rune]string{'½': "1/2", '⅓': "1/3", '⅔': "2/3", '¼': "1/4", '¾': "3/4", '⅕': "1/5", '⅛': "1/8", '⅜': "3/8", '⅝': "5/8", '⅞': "7/8"}

var units = map[string]string{
	"t": "tsp", "tsp": "tsp", "teaspoon": "tsp", "cdita": "tsp", "cucharadita": "tsp",
	"tbsp": "tbsp", "tbs": "tbsp", "tbl": "tbsp", "tablespoon": "tbsp", "cda": "tbsp", "cucharada": "tbsp",
	"c": "cup", "cup": "cup", "taza": "cup",
	"g": "g", "gr": "g", "gram": "g", "gramo": "g", "grm": "g",
	"kg": "kg", "kilo": "kg", "kilogram": "kg", "kilogramo": "kg",
	"ml": "ml", "milliliter": "ml", "millilitre": "ml", "mililitro": "ml", "cc": "ml",
	"l": "l", "liter": "l", "litre": "l", "litro": "l", "lt": "l",
	"oz": "oz", "ounce": "oz", "onza": "oz",
	"lb": "lb", "pound": "lb", "libra": "lb",
	"clove": "clove", "diente": "clove",
	"pinch": "pinch", "pizca": "pinch",
}

var number = regexp.MustCompile(`^\d+(?:[.,]\d+)?$|^\d+/\d+$`)

// Amount normalizes a quantity and unit so that "1 ½ cups", "1½ cup" and
// "1 1/2 Cups" compare equal. Words it does not understand are kept,
// lowercased, so "a pinch" still equals "a pinch".
func Amount(qty, unit string) string {
	q := strings.ToLower(strings.TrimSpace(qty))
	var b strings.Builder
	for _, r := range q {
		if f, ok := fractions[r]; ok {
			b.WriteString(" " + f)
		} else {
			b.WriteRune(r)
		}
	}
	q = strings.ReplaceAll(b.String(), "–", "-")
	q = strings.ReplaceAll(q, "—", "-")
	// Sum leading mixed numbers: "1 1/2" → 1.5; ranges keep both ends.
	var parts []string
	for _, side := range strings.Split(q, "-") {
		parts = append(parts, sumNumbers(side))
	}
	q = strings.Join(parts, "-")

	u := strings.ToLower(strings.TrimSpace(unit))
	u = strings.TrimRightFunc(u, func(r rune) bool { return r == '.' || unicode.IsSpace(r) })
	if n, ok := units[u]; ok {
		u = n
	} else if n, ok := units[strings.TrimSuffix(u, "s")]; ok {
		u = n
	} else if n, ok := units[strings.TrimSuffix(u, "es")]; ok {
		u = n
	}
	return strings.TrimSpace(q + " " + u)
}

func sumNumbers(s string) string {
	fields := strings.Fields(s)
	total, n := 0.0, 0
	for _, f := range fields {
		if !number.MatchString(f) {
			break
		}
		if num, den, ok := strings.Cut(f, "/"); ok {
			a, _ := strconv.ParseFloat(num, 64)
			d, _ := strconv.ParseFloat(den, 64)
			if d == 0 {
				break
			}
			total += a / d
		} else {
			v, _ := strconv.ParseFloat(strings.ReplaceAll(f, ",", "."), 64)
			total += v
		}
		n++
	}
	if n == 0 {
		return strings.Join(fields, " ")
	}
	rest := strings.Join(fields[n:], " ")
	num := strconv.FormatFloat(math.Round(total*1000)/1000, 'f', -1, 64)
	return strings.TrimSpace(num + " " + rest)
}
