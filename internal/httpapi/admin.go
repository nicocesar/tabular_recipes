package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/store"
	"github.com/nicocesar/tabular_recipes/internal/tabulate"
)

// tickStale is when the workbench warns that the scheduler seems gone:
// two missed passes of a 15-minute schedule.
const tickStale = 35 * time.Minute

type adminRow struct {
	ID, Date, Status, Title, Cost, Subs string
}

type adminView struct {
	LastTick, TickWarn, CostAll, CostNote, Counts string
	RunsToday, Cap                                int
	Total                                         store.Meters
	Rows                                          []adminRow
}

func dollars(cents int64) string { return fmt.Sprintf("%.2f", float64(cents)/100) }

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := adminView{Cap: tabulate.DailyRuns, CostNote: "the vendor's list price; the bill is in the Console"}
	var last tabulate.TickResult
	if err := s.cfg.Store.GetMeta(ctx, "tick", &last); err != nil || last.At.IsZero() {
		v.LastTick = "never"
		v.TickWarn = "No Tick has landed. Is the Cloud Scheduler job set up? (SETUP.md §7)"
	} else {
		ago := time.Since(last.At).Round(time.Minute)
		v.LastTick = last.At.Format("Jan 2 15:04 UTC") + fmt.Sprintf(" (%s ago)", ago)
		if ago > tickStale {
			v.TickWarn = "Stale: the scheduler may have stopped."
		}
		if len(last.Errors) > 0 {
			v.TickWarn += " Last pass had errors: " + last.Errors[0]
		}
	}
	v.RunsToday, _ = s.cfg.Store.Count(ctx, tabulate.RunsKey(time.Now()))

	var all []*store.Record
	counts := map[store.Status]int{}
	for _, st := range []store.Status{store.Ready, store.Dispatched, store.Running, store.Failed} {
		recs, err := s.cfg.Store.ByStatus(ctx, st)
		if err != nil {
			log.Printf("admin: %v", err)
		}
		counts[st] = len(recs)
		all = append(all, recs...)
	}
	sort.Slice(all, func(a, b int) bool { return all[a].CreatedAt.After(all[b].CreatedAt) })
	var cost int64
	for _, rec := range all {
		m := rec.Meters
		t := &v.Total
		t.InputTokens += m.InputTokens
		t.OutputTokens += m.OutputTokens
		t.CacheRead += m.CacheRead
		t.CacheWrite += m.CacheWrite
		t.Fetches += m.Fetches
		t.ActiveSeconds += m.ActiveSeconds
		t.Sessions += m.Sessions
		t.Submissions += m.Submissions
		t.RejectSchema += m.RejectSchema
		t.RejectTree += m.RejectTree
		t.RejectFit += m.RejectFit
		t.ResumesTotal += m.ResumesTotal
		cost += m.ListCostCents
		title := firstNonEmpty(rec.SourceURL, rec.PastedSource, "pasted recipe")
		if rec.Recipe != nil {
			title = rec.Recipe.Title
		}
		v.Rows = append(v.Rows, adminRow{
			ID: rec.ID, Date: rec.CreatedAt.Format("Jan 2 15:04"), Status: string(rec.Status), Title: title,
			Cost: dollars(m.ListCostCents),
			Subs: fmt.Sprintf("%d/%d", m.Submissions, m.RejectSchema+m.RejectTree+m.RejectFit),
		})
	}
	v.CostAll = dollars(cost)
	v.Counts = fmt.Sprintf("%d ready · %d in flight · %d failed", counts[store.Ready], counts[store.Dispatched]+counts[store.Running], counts[store.Failed])
	s.render(w, r, "admin", http.StatusOK, v)
}

type adminRecipeView struct {
	ID, Title, Status, Source, Session, Fail, Cost string
	Views                                          int
	M                                              store.Meters
	Trace                                          []store.TraceStep
}

func (s *Server) handleAdminRecipe(w http.ResponseWriter, r *http.Request) {
	rec := s.record(w, r)
	if rec == nil {
		return
	}
	v := adminRecipeView{
		ID: rec.ID, Status: string(rec.Status), Source: rec.SourceURL, Session: rec.SessionID,
		M: rec.Meters, Trace: rec.Trace, Views: rec.Views, Cost: "$" + dollars(rec.Meters.ListCostCents),
		Title: firstNonEmpty(rec.SourceURL, rec.PastedSource, "pasted recipe"),
	}
	if rec.Recipe != nil {
		v.Title = rec.Recipe.Title
	}
	if rec.Status == store.Failed {
		v.Fail = rec.FailKind + ": " + rec.FailReason
	}
	s.render(w, r, "admin_recipe", http.StatusOK, v)
}

// handleWork is the Run itself, held open for its whole duration (rule 7,
// ADR 0013). The Run outlives a dropped caller: if the dispatcher's
// connection goes away the work carries on, and if CPU is throttled
// after all, the lease lapses and the Tick resumes on the same session.
func (s *Server) handleWork(w http.ResponseWriter, r *http.Request) {
	err := s.cfg.Runner.Run(context.WithoutCancel(r.Context()), r.PathValue("id"))
	switch {
	case errors.Is(err, tabulate.ErrLeased):
		http.Error(w, "run in progress", http.StatusConflict)
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "no such recipe", http.StatusNotFound)
	case err != nil:
		// Not a 5xx: nothing retries /work, and the record carries the story.
		log.Printf("work %s: %v", r.PathValue("id"), err)
		w.Write([]byte("done: " + err.Error() + "\n"))
	default:
		w.Write([]byte("done\n"))
	}
}

// handleTick always answers 200: Scheduler retries non-2xx, and a retry
// could re-spend (rule 9). Errors ride in the JSON body.
func (s *Server) handleTick(w http.ResponseWriter, r *http.Request) {
	res := s.cfg.Runner.Tick(r.Context())
	if err := s.cfg.Store.PutMeta(r.Context(), "tick", res); err != nil {
		res.Errors = append(res.Errors, "recording the pass: "+err.Error())
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
