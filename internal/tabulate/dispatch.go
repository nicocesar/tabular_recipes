package tabulate

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/store"
)

// RunsKey and IPKey name the daily counters (ADR 0008).
func RunsKey(now time.Time) string { return "runs:" + now.UTC().Format("2006-01-02") }
func IPKey(now time.Time, ipHash string) string {
	return "ip:" + now.UTC().Format("2006-01-02") + ":" + ipHash
}

// Retry starts a new Run (a new session) for a Failed record. The caller
// has already counted it against the daily cap.
func (rn *Runner) Retry(ctx context.Context, id string) error {
	_, err := rn.Store.Update(ctx, id, func(r *store.Record) error {
		if r.Status != store.Failed {
			return fmt.Errorf("only a failed recipe can be retried (this one is %s)", r.Status)
		}
		r.Status = store.Dispatched
		r.SessionID, r.SessionSeen = "", store.Usage{}
		r.Resumes, r.FitRejections, r.Nudged = 0, 0, false
		r.FailKind, r.FailReason = "", ""
		r.DispatchedAt = time.Now().UTC()
		r.Meters.Retries++
		r.Log("retry", "the owner retried; a new run starts")
		return nil
	})
	if err != nil {
		return err
	}
	return rn.dispatchLogged(ctx, id)
}

// Start dispatches a Run and notes a failed dispatch in the Trace; the
// Tick picks it up. It never runs the work here as a fallback (rule 7).
func (rn *Runner) Start(ctx context.Context, id string) { rn.dispatchLogged(ctx, id) }

func (rn *Runner) dispatchLogged(ctx context.Context, id string) error {
	if err := rn.Dispatch(ctx, id); err != nil {
		rn.Store.Update(ctx, id, func(r *store.Record) error {
			r.Log("dispatch", "dispatch failed, the Tick will retry: "+err.Error())
			return nil
		})
		return nil
	}
	return nil
}

// TickResult is the Tick's JSON body.
type TickResult struct {
	At           time.Time `json:"at"`
	Redispatched []string  `json:"redispatched"`
	GaveUp       []string  `json:"gave_up,omitempty"`
	Deferred     int       `json:"deferred_by_cap,omitempty"`
	Errors       []string  `json:"errors,omitempty"`
}

const tickBudget = 3

// Tick re-dispatches lost and stalled Runs (ADR 0013). It never runs
// work inline; it only dispatches, within a count budget.
func (rn *Runner) Tick(ctx context.Context) TickResult {
	now := time.Now().UTC()
	res := TickResult{At: now, Redispatched: []string{}}
	var due []*store.Record
	if ds, err := rn.Store.ByStatus(ctx, store.Dispatched); err != nil {
		res.Errors = append(res.Errors, err.Error())
	} else {
		for _, r := range ds {
			if now.Sub(r.DispatchedAt) > StaleDispatch {
				due = append(due, r)
			}
		}
	}
	if rs, err := rn.Store.ByStatus(ctx, store.Running); err != nil {
		res.Errors = append(res.Errors, err.Error())
	} else {
		for _, r := range rs {
			if r.LeaseUntil.Before(now) {
				due = append(due, r)
			}
		}
	}
	sort.Slice(due, func(a, b int) bool { return due[a].CreatedAt.Before(due[b].CreatedAt) })

	for _, r := range due {
		if len(res.Redispatched)+len(res.GaveUp) >= tickBudget {
			break
		}
		if r.Resumes >= MaxResumes {
			if err := rn.fail(r.ID, "stalled", fmt.Sprintf("gave up after %d resumes", MaxResumes)); err != nil {
				res.Errors = append(res.Errors, err.Error())
			}
			res.GaveUp = append(res.GaveUp, r.ID)
			continue
		}
		if _, ok, err := rn.Store.Incr(ctx, RunsKey(now), DailyRuns); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		} else if !ok {
			res.Deferred++
			continue
		}
		_, err := rn.Store.Update(ctx, r.ID, func(x *store.Record) error {
			if x.Status != store.Dispatched && x.Status != store.Running {
				return fmt.Errorf("%s is now %s", x.ID, x.Status)
			}
			x.Resumes++
			x.Meters.ResumesTotal++
			x.Status = store.Dispatched
			x.DispatchedAt = now
			x.LeaseUntil = time.Time{}
			x.Log("tick", fmt.Sprintf("re-dispatched (resume %d of %d)", x.Resumes, MaxResumes))
			return nil
		})
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		rn.dispatchLogged(ctx, r.ID)
		res.Redispatched = append(res.Redispatched, r.ID)
	}
	return res
}
