package tabulate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/card"
	"github.com/nicocesar/tabular_recipes/internal/i18n"
	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
)

// A Translation is a Run of the translator for one card into one
// language; it writes an Overlay onto the card (ADRs 0014, 0015).

// TranslateKey is the Claim that dedupes Translations per (card, language).
func TranslateKey(cardID, to string) string { return "translate:" + cardID + ":" + to }

// NewTranslation is the job record for translating card into to. The
// caller claims, caps and stores it, then calls Started and Start.
func NewTranslation(cardID, to string) *store.Record {
	now := time.Now().UTC()
	job := &store.Record{ID: store.NewID(), Kind: store.KindTranslate, Parent: cardID, To: to,
		Status: store.Dispatched, CreatedAt: now, DispatchedAt: now}
	job.Log("filed", "translate card "+cardID+" into "+to)
	return job
}

// Started notes the job on its card, so the card page can show it.
func (rn *Runner) Started(ctx context.Context, job *store.Record) error {
	_, err := rn.Store.Update(ctx, job.Parent, func(r *store.Record) error {
		if r.Translating == nil {
			r.Translating = map[string]string{}
		}
		r.Translating[job.To] = job.ID
		r.Log("translate", fmt.Sprintf("translation into %s started: %s", job.To, job.ID))
		return nil
	})
	return err
}

func (rn *Runner) translateMessage(ctx context.Context, job *store.Record) (string, error) {
	parent, err := rn.Store.Get(ctx, job.Parent)
	if err != nil || parent.Recipe == nil {
		return "", fmt.Errorf("the card %s has no recipe", job.Parent)
	}
	b, _ := json.MarshalIndent(parent.Recipe, "", " ")
	msg := fmt.Sprintf("Translate this card from %s into %s (lang %q).\n\n<card>\n%s\n</card>\n",
		langName(parent.Recipe.Lang), langName(job.To), job.To, b)
	if parent.DishID != "" {
		if d, err := rn.Store.GetDish(ctx, parent.DishID); err == nil {
			if n := d.Names[job.To]; n != "" {
				msg += fmt.Sprintf("\nThe dish is already called %q in %s: use that as dish.\n", n, langName(job.To))
			} else if n, _ := d.Name("en", parent.Recipe.Lang); n != "" {
				msg += fmt.Sprintf("\nThe dish is %q; give its everyday name in %s as dish.\n", n, langName(job.To))
			}
		}
	}
	return msg, nil
}

// langName is a language's English name for the prompt.
func langName(code string) string {
	if n, ok := map[string]string{"en": "English", "es": "Spanish (Rioplatense)", "it": "Italian", "fr": "French", "de": "German", "pt": "Portuguese"}[code]; ok {
		return n
	}
	if code == "" {
		return "English"
	}
	return code
}

// submitTranslation validates one Overlay and answers it: every id once,
// and the card in these words must still fit one page (ADR 0004).
func (rn *Runner) submitTranslation(ctx context.Context, id, sid string, e Event) (bool, error) {
	job, err := rn.Store.Get(ctx, id)
	if err != nil {
		return true, err
	}
	parent, err := rn.Store.Get(ctx, job.Parent)
	if err != nil || parent.Recipe == nil {
		return true, rn.fail(id, "config", "the card to translate is gone")
	}
	reject := func(kind, msg string) (bool, error) {
		rn.reject(ctx, id, kind, msg)
		return false, rn.API.SendToolResult(ctx, sid, e.ID, "Rejected. Fix these and call submit_translation again:\n"+msg, true)
	}
	var o recipe.Overlay
	if err := json.NewDecoder(bytes.NewReader(e.Input)).Decode(&o); err != nil {
		return reject("schema", "- the input is not valid translation JSON: "+err.Error())
	}
	o.Lang = strings.TrimSpace(o.Lang)
	if ps := recipe.ValidateOverlay(parent.Recipe, &o, job.To); len(ps) > 0 {
		return reject("schema", problems(ps))
	}
	fit := card.Fit(parent.Recipe.Apply(&o))
	line := fmt.Sprintf("accepted; prints %s at %.1f pt", orientation(fit), fit.CellPt)
	if !fit.OK {
		if job.FitRejections < MaxFitTries {
			rn.reject(ctx, id, "fit", fit.Message)
			return false, rn.API.SendToolResult(ctx, sid, e.ID, "Rejected: "+fit.Message+" Then call submit_translation again.", true)
		}
		line = fmt.Sprintf("accepted after %d fit rejections; may not fit one page (%.1f pt)", MaxFitTries, fit.CellPt)
	}
	o.From, o.Model, o.Job, o.At = parent.Recipe.Lang, TranslatorModel, id, time.Now().UTC()
	if _, err := rn.Store.Update(ctx, parent.ID, func(r *store.Record) error {
		if r.Translations == nil {
			r.Translations = map[string]*recipe.Overlay{}
		}
		r.Translations[job.To] = &o
		delete(r.Translating, job.To)
		r.Log("translate", fmt.Sprintf("translated into %s by %s (%s)", job.To, id, line))
		return nil
	}); err != nil {
		return true, err
	}
	if parent.DishID != "" {
		rn.Store.UpdateDish(ctx, parent.DishID, func(d *store.Dish) error {
			if d.Names == nil {
				d.Names = map[string]string{}
			}
			if d.Names[job.To] == "" {
				d.Names[job.To] = strings.TrimSpace(o.Dish)
			}
			return nil
		})
	}
	rn.Store.Update(ctx, id, func(r *store.Record) error {
		r.Meters.Submissions++
		r.Status, r.LeaseUntil = store.Ready, time.Time{}
		r.Log("accepted", line)
		return nil
	})
	rn.API.SendToolResult(ctx, sid, e.ID, "Accepted and saved. You are done.", false)
	if info, err := rn.API.Session(ctx, sid); err == nil {
		rn.meter(ctx, id, info.Usage)
	}
	return true, nil
}

// Translatable says whether a card can be translated into to.
func Translatable(card *store.Record, to string) bool {
	if card.Kind != store.KindCard || card.Status != store.Ready || card.Recipe == nil {
		return false
	}
	from := card.Recipe.Lang
	if from == "" {
		from = "en"
	}
	return i18n.Has(to) && from != to && card.Translations[to] == nil
}
