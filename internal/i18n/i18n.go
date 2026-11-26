// Package i18n is the chest's page text, one committed catalog per
// language (ADR 0016). Keys are stable ids; values are fmt format
// strings. A missing key falls back to English, then to the key itself.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

//go:embed *.json
var files embed.FS

// Default is the language of last resort.
const Default = "en"

var catalogs = map[string]map[string]string{}

func init() {
	ents, err := files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, e := range ents {
		b, err := files.ReadFile(e.Name())
		if err != nil {
			panic(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", e.Name(), err))
		}
		catalogs[strings.TrimSuffix(e.Name(), path.Ext(e.Name()))] = m
	}
	if catalogs[Default] == nil {
		panic("i18n: no " + Default + " catalog")
	}
}

// Langs lists the languages that have a catalog, English first.
func Langs() []string {
	var ls []string
	for l := range catalogs {
		if l != Default {
			ls = append(ls, l)
		}
	}
	sort.Strings(ls)
	return append([]string{Default}, ls...)
}

// Has reports whether lang has a catalog.
func Has(lang string) bool { return catalogs[lang] != nil }

// T formats key in lang.
func T(lang, key string, args ...any) string {
	f, ok := catalogs[lang][key]
	if !ok {
		if f, ok = catalogs[Default][key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return f
	}
	return fmt.Sprintf(f, args...)
}

// Date is "Nov 25, 2026" in lang's words and order.
func Date(lang string, t time.Time) string {
	return T(lang, "date.format", T(lang, fmt.Sprintf("month.%d", int(t.Month()))), t.Day(), t.Year())
}

// Name is a language's name in its own words ("Español"), for the switch.
func Name(lang string) string { return T(lang, "lang.name") }

// Match picks the first supported language from an Accept-Language
// header ("es-AR,es;q=0.9,en;q=0.8"), or "".
func Match(accept string) string {
	type cand struct {
		lang string
		q    float64
	}
	var cs []cand
	for i, part := range strings.Split(accept, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			fmt.Sscanf(v, "%g", &q)
		}
		base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		cs = append(cs, cand{base, q - float64(i)*1e-6}) // stable on ties
	}
	sort.SliceStable(cs, func(a, b int) bool { return cs[a].q > cs[b].q })
	for _, c := range cs {
		if c.q > 0 && Has(c.lang) {
			return c.lang
		}
	}
	return ""
}

// LangName is a language's name written in lang ("inglés"), or its code
// in capitals when the catalog has no name for it.
func LangName(lang, code string) string {
	if code == "" {
		code = "en"
	}
	if n := T(lang, "langname."+code); n != "langname."+code {
		return n
	}
	return strings.ToUpper(code)
}
