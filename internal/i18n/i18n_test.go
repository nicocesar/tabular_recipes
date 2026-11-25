package i18n

import (
	"fmt"
	"regexp"
	"slices"
	"testing"
	"time"
)

var verb = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0-9.]*[a-zA-Z%]`)

// verbs lists a format's verbs in argument order: explicit indexes
// ("%[2]d") may reorder words, never change what is passed.
func verbs(f string) []string {
	idx := regexp.MustCompile(`^%\[(\d+)\]`)
	byArg := map[int]string{}
	n := 0
	for _, v := range verb.FindAllString(f, -1) {
		if v == "%%" {
			continue
		}
		if m := idx.FindStringSubmatch(v); m != nil {
			fmt.Sscan(m[1], &n)
			v = "%" + v[len(m[0]):]
		} else {
			n++
		}
		byArg[n] = v
	}
	out := make([]string, len(byArg))
	for i := range out {
		out[i] = byArg[i+1]
	}
	return out
}

// Every catalog has exactly English's keys, with the same verbs in the
// same order, so a translation can never break a page.
func TestCatalogsMatchEnglish(t *testing.T) {
	en := catalogs[Default]
	for lang, m := range catalogs {
		for k, v := range en {
			got, ok := m[k]
			if !ok {
				t.Errorf("%s: missing %q", lang, k)
				continue
			}
			if !slices.Equal(verbs(v), verbs(got)) {
				t.Errorf("%s: %q has verbs %v, English has %v", lang, k, verbs(got), verbs(v))
			}
		}
		for k := range m {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: %q is not in English", lang, k)
			}
		}
	}
}

func TestMatch(t *testing.T) {
	for in, want := range map[string]string{
		"es-AR,es;q=0.9,en;q=0.8": "es",
		"fr-FR,en;q=0.5":          "en",
		"fr":                      "",
		"en;q=0.2,es;q=0.9":       "es",
		"":                        "",
	} {
		if got := Match(in); got != want {
			t.Errorf("Match(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDate(t *testing.T) {
	d := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	if got := Date("en", d); got != "Nov 25, 2026" {
		t.Errorf("en: %q", got)
	}
	if got := Date("es", d); got != "25 nov 2026" {
		t.Errorf("es: %q", got)
	}
	if T("xx", "card.serves", "4") != "Serves 4" {
		t.Error("unknown language should fall back to English")
	}
}
