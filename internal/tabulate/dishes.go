package tabulate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	dishcmp "github.com/nicocesar/tabular_recipes/internal/dish"
	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
)

// dishPick is the Submission's choice of Dish (ADR 0017): an existing id,
// or a name for a new one.
type dishPick struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// submission is submit_recipe's input: the Recipe plus its Dish.
type submission struct {
	recipe.Recipe
	Dish dishPick `json:"dish"`
}

// checkPick answers whether a pick can be filed, phrased for the agent.
func (rn *Runner) checkPick(ctx context.Context, p dishPick) string {
	switch {
	case strings.TrimSpace(p.ID) != "":
		if _, err := rn.Store.GetDish(ctx, strings.TrimSpace(p.ID)); err != nil {
			return fmt.Sprintf("dish id %q is not in the list; use an id from the list, or give dish.name for a new dish", p.ID)
		}
	case strings.TrimSpace(p.Name) == "":
		return "dish is missing: give dish.id from the list, or dish.name for a new dish"
	case len([]rune(p.Name)) > 60:
		return "dish.name is too long; the everyday name of the dish"
	}
	return ""
}

// dishList is the block of the first message that lets the agent group
// a card (ADR 0017): every Dish with its names and its Variants' Labels.
func (rn *Runner) dishList(ctx context.Context) string {
	ds, err := rn.Store.Dishes(ctx)
	if err != nil || len(ds) == 0 {
		return "DISHES: the chest has none yet, so give dish.name for a new dish."
	}
	type line struct{ sort, text string }
	var lines []line
	for _, d := range ds {
		if len(d.Variants) == 0 {
			continue
		}
		var names []string
		for l, n := range d.Names {
			names = append(names, fmt.Sprintf("%s (%s)", n, l))
		}
		sort.Strings(names)
		var labels []string
		for _, v := range d.Variants {
			if r, err := rn.Store.Get(ctx, v.Record); err == nil && r.Recipe != nil && r.Recipe.Label != "" {
				labels = append(labels, fmt.Sprintf("%d. %s", v.N, r.Recipe.Label))
			}
		}
		t := fmt.Sprintf("- %s: %s", d.ID, strings.Join(names, " / "))
		if len(labels) > 0 {
			t += " — variants: " + strings.Join(labels, "; ")
		}
		lines = append(lines, line{strings.ToLower(names[0]), t})
	}
	if len(lines) == 0 {
		return "DISHES: the chest has none yet, so give dish.name for a new dish."
	}
	sort.Slice(lines, func(a, b int) bool { return lines[a].sort < lines[b].sort })
	var b strings.Builder
	b.WriteString("DISHES ALREADY IN THE CHEST. If this recipe is a version of one of them (in any language), give its id as dish.id; otherwise give dish.name for a new dish. Make the label tell this version apart from the variants listed.\n")
	for _, l := range lines {
		b.WriteString(l.text + "\n")
	}
	return b.String()
}

// file puts an accepted card into its Dish: it folds into a Variant it
// copies (ADR 0018), or becomes the Dish's next Variant. It returns a
// line for the Trace of the job that filed it.
func (rn *Runner) file(ctx context.Context, cardID string, pick dishPick, lang string) (string, error) {
	card, err := rn.Store.Get(ctx, cardID)
	if err != nil {
		return "", err
	}
	var d *store.Dish
	if id := strings.TrimSpace(pick.ID); id != "" {
		if d, err = rn.Store.GetDish(ctx, id); err != nil {
			return "", err
		}
		for _, v := range d.Variants {
			if v.Record == cardID {
				continue
			}
			vr, err := rn.Store.Get(ctx, v.Record)
			if err != nil || vr.Recipe == nil || vr.Status != store.Ready {
				continue
			}
			if same, report := dishcmp.Same(vr.Recipe, card.Recipe); same {
				return rn.fold(ctx, card, vr, d, v.N, report)
			}
		}
	} else {
		d = &store.Dish{ID: store.NewID(), Names: map[string]string{lang: strings.TrimSpace(pick.Name)}, CreatedAt: time.Now().UTC()}
		if err := rn.Store.CreateDish(ctx, d); err != nil {
			return "", err
		}
	}
	var n int
	if d, err = rn.Store.UpdateDish(ctx, d.ID, func(x *store.Dish) error { n = x.Add(cardID); return nil }); err != nil {
		return "", err
	}
	name, _ := d.Name(lang, lang)
	msg := fmt.Sprintf("filed as variant %d of %q (dish %s)", n, name, d.ID)
	if d.Reference == cardID {
		msg += ", the reference"
	}
	_, err = rn.Store.Update(ctx, cardID, func(r *store.Record) error {
		r.DishID, r.Variant = d.ID, n
		r.Log("dish", msg)
		return nil
	})
	return msg, err
}

// fold hides card as a copy of variant vr (ADR 0018).
func (rn *Runner) fold(ctx context.Context, card, vr *store.Record, d *store.Dish, n int, report string) (string, error) {
	msg := fmt.Sprintf("folded into variant %d (%s): %s", n, vr.ID, report)
	if _, err := rn.Store.Update(ctx, card.ID, func(r *store.Record) error {
		if r.DishID != "" && r.DishID != d.ID {
			rn.leaveDish(ctx, r.DishID, r.ID)
		}
		r.Status, r.FoldedInto, r.DishID, r.Variant = store.Folded, vr.ID, d.ID, 0
		r.Log("fold", msg)
		return nil
	}); err != nil {
		return "", err
	}
	_, err := rn.Store.Update(ctx, vr.ID, func(r *store.Record) error {
		r.AlsoAt = append(r.AlsoAt, store.Source{URL: card.SourceURL, Record: card.ID})
		for _, s := range card.AlsoAt { // its own copies come along
			r.AlsoAt = append(r.AlsoAt, s)
		}
		src := card.SourceURL
		if src == "" {
			src = "pasted text"
		}
		r.Log("fold", fmt.Sprintf("a copy (%s, %s) was folded in: %s", card.ID, src, report))
		return nil
	})
	return msg, err
}

func (rn *Runner) leaveDish(ctx context.Context, dishID, recordID string) {
	rn.Store.UpdateDish(ctx, dishID, func(x *store.Dish) error { x.Remove(recordID); return nil })
}

// backfillMessage starts a backfill session: the card as it is, and the
// one thing to add (ADR 0017).
func (rn *Runner) backfillMessage(ctx context.Context, job *store.Record) (string, error) {
	parent, err := rn.Store.Get(ctx, job.Parent)
	if err != nil || parent.Recipe == nil {
		return "", fmt.Errorf("the card %s has no recipe", job.Parent)
	}
	b, _ := json.MarshalIndent(parent.Recipe, "", " ")
	return "This card is already in the chest and must not change. Do not fetch anything. " +
		"Call submit_recipe with exactly this card — the same title, servings, time, notes, credit, and the same ingredients and steps with the same ids and words — " +
		"adding only: lang (the language the card is written in), label, dish, and a canon on every ingredient.\n\n<card>\n" +
		string(b) + "\n</card>\n\n" + rn.dishList(ctx), nil
}

// StartBackfill is the Owner's button: every card without a Dish gets a
// backfill Run, one at a time, so later cards see the Dishes earlier ones
// made.
func (rn *Runner) StartBackfill(ctx context.Context) (string, error) {
	cards, err := rn.Store.ByStatus(ctx, store.Ready)
	if err != nil {
		return "", err
	}
	for _, c := range cards {
		if c.Kind == store.KindCard && c.DishID == "" && c.BackfillTried {
			rn.Store.Update(ctx, c.ID, func(r *store.Record) error { r.BackfillTried = false; return nil })
		}
	}
	return rn.BackfillNext(ctx)
}

// BackfillNext starts the next backfill, unless one is in flight, none
// is left, or the day's cap is reached. It returns the job id, or "".
func (rn *Runner) BackfillNext(ctx context.Context) (string, error) {
	for _, st := range []store.Status{store.Dispatched, store.Running} {
		recs, err := rn.Store.ByStatus(ctx, st)
		if err != nil {
			return "", err
		}
		for _, r := range recs {
			if r.Kind == store.KindBackfill {
				return "", nil // one at a time
			}
		}
	}
	cards, err := rn.Store.ByStatus(ctx, store.Ready)
	if err != nil {
		return "", err
	}
	var next *store.Record
	for _, c := range cards {
		if c.Kind == store.KindCard && c.DishID == "" && !c.BackfillTried && c.Recipe != nil {
			if next == nil || c.CreatedAt.Before(next.CreatedAt) {
				next = c
			}
		}
	}
	if next == nil {
		return "", nil
	}
	now := time.Now().UTC()
	if _, ok, err := rn.Store.Incr(ctx, RunsKey(now), DailyRuns); err != nil || !ok {
		return "", fmt.Errorf("the daily run cap is reached; press backfill again tomorrow")
	}
	job := &store.Record{ID: store.NewID(), Kind: store.KindBackfill, Parent: next.ID, Status: store.Dispatched, CreatedAt: now, DispatchedAt: now}
	job.Log("filed", "backfill for card "+next.ID)
	if err := rn.Store.Create(ctx, job); err != nil {
		return "", err
	}
	rn.Store.Update(ctx, next.ID, func(r *store.Record) error {
		r.BackfillTried = true
		r.Log("backfill", "backfill run "+job.ID+" started")
		return nil
	})
	rn.Start(ctx, job.ID)
	return job.ID, nil
}

// afterRun continues a backfill chain once a job has ended either way.
func (rn *Runner) afterRun(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rec, err := rn.Store.Get(ctx, id)
	if err != nil || rec.Kind != store.KindBackfill || (rec.Status != store.Ready && rec.Status != store.Failed) {
		return
	}
	if _, err := rn.BackfillNext(ctx); err != nil {
		log.Printf("backfill chain: %v", err)
	}
}
