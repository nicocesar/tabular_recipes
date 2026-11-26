package tabulate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/card"
	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
)

// Ceilings (ADR 0013). The service request timeout is 15 min.
const (
	RunCeiling    = 10 * time.Minute
	LeaseSlack    = 2 * time.Minute
	MaxFitTries   = 3 // fit-only rejections before accepting (ADR 0004)
	MaxResumes    = 3
	DailyRuns     = 100 // global cap, ADR 0008
	DailyPerIP    = 25
	StaleDispatch = 2 * time.Minute
)

// pollEvery paces a Run's session checks (a var so tests can shorten it).
var pollEvery = 2500 * time.Millisecond

// ErrLeased means another Run holds the record.
var ErrLeased = errors.New("tabulate: run in progress")

// Runner runs and dispatches Runs.
type Runner struct {
	Store store.Store
	API   API // nil when ANTHROPIC_API_KEY is unset: Runs fail fast
	// WorkURL is the service's own run.app URL; empty on a laptop, where
	// Runs are goroutines (rule 7).
	WorkURL   string
	TickToken string
	AgentID   string
	// TranslatorID is the translator agent (ADR 0014).
	TranslatorID string
	EnvID        string
	// Console builds the trace link to a session, when known.
	ConsoleWorkspace string
	HTTP             *http.Client
}

// Boot upserts the agent and environment (rule 14).
func (rn *Runner) Boot(ctx context.Context) error {
	if rn.API == nil {
		return nil
	}
	id, err := rn.API.EnsureAgent(ctx, Spec())
	if err != nil {
		return fmt.Errorf("ensure agent: %w", err)
	}
	tid, err := rn.API.EnsureAgent(ctx, TranslatorSpec())
	if err != nil {
		return fmt.Errorf("ensure translator: %w", err)
	}
	env, err := rn.API.EnsureEnvironment(ctx, EnvName)
	if err != nil {
		return fmt.Errorf("ensure environment: %w", err)
	}
	rn.AgentID, rn.TranslatorID, rn.EnvID = id, tid, env
	return nil
}

// Dispatch starts a Run for a Dispatched record: a POST to /work/{id}
// on the service itself, or a goroutine without WORK_URL. A failed
// dispatch is never run here as a fallback — the Tick retries it.
func (rn *Runner) Dispatch(ctx context.Context, id string) error {
	if rn.WorkURL == "" {
		go func() {
			if err := rn.Run(context.Background(), id); err != nil && !errors.Is(err, ErrLeased) {
				log.Printf("run %s: %v", id, err)
			}
		}()
		return nil
	}
	// Fire and forget: the /work request is held open for the whole Run,
	// so we only wait long enough to know it was accepted.
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodPost, strings.TrimRight(rn.WorkURL, "/")+"/work/"+id, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+rn.TickToken)
	errc := make(chan error, 1)
	go func() {
		c := rn.HTTP
		if c == nil {
			c = &http.Client{Timeout: RunCeiling + LeaseSlack + time.Minute}
		}
		resp, err := c.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 500 {
				err = fmt.Errorf("work answered %d", resp.StatusCode)
			}
		}
		errc <- err
	}()
	select {
	case err := <-errc:
		return err // finished (or failed) quickly
	case <-time.After(3 * time.Second):
		return nil // in flight: the Run is underway
	}
}

// Run is the whole Run for one record, inside the caller's request.
func (rn *Runner) Run(ctx context.Context, id string) error {
	now := time.Now().UTC()
	rec, err := rn.Store.Update(ctx, id, func(r *store.Record) error {
		if r.Status == store.Ready || r.Status == store.Failed {
			return fmt.Errorf("record is %s", r.Status)
		}
		if r.LeaseUntil.After(now) {
			return ErrLeased
		}
		r.LeaseUntil = now.Add(RunCeiling + LeaseSlack)
		r.Status = store.Running
		if r.SessionID != "" {
			r.Log("run", "resumed on the same session")
		} else {
			r.Log("run", "started")
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer rn.afterRun(id)
	ctx, cancel := context.WithTimeout(ctx, RunCeiling)
	defer cancel()

	if rn.API == nil {
		return rn.fail(id, "config", "ANTHROPIC_API_KEY is not set on the server")
	}
	if rec.SessionID == "" {
		agent, title := rn.AgentID, "recipe "+id
		if rec.Kind == store.KindTranslate {
			agent, title = rn.TranslatorID, "translate "+rec.Parent+" to "+rec.To
		}
		sid, err := rn.API.CreateSession(ctx, agent, rn.EnvID, title, SessionBudgetCents)
		if err != nil {
			return rn.fail(id, "vendor", "could not create session: "+err.Error())
		}
		// The checkpoint: a resume reattaches here instead of paying again.
		_, err = rn.Store.Update(ctx, id, func(r *store.Record) error {
			r.SessionID = sid
			r.SessionSeen = store.Usage{}
			r.Meters.Sessions++
			r.Log("session", "created "+sid+rn.consoleSuffix(sid))
			return nil
		})
		if err != nil {
			return err
		}
		first, err := rn.firstMessage(ctx, rec)
		if err != nil {
			return rn.fail(id, "config", err.Error())
		}
		if err := rn.API.SendMessage(ctx, sid, first); err != nil {
			return rn.fail(id, "vendor", "could not send the first message: "+err.Error())
		}
		rec.SessionID = sid
	}
	return rn.drive(ctx, id, rec.SessionID)
}

func (rn *Runner) consoleSuffix(sid string) string {
	if rn.ConsoleWorkspace == "" {
		return ""
	}
	return " — https://platform.claude.com/workspaces/" + rn.ConsoleWorkspace + "/sessions/" + sid
}

func (rn *Runner) firstMessage(ctx context.Context, r *store.Record) (string, error) {
	switch r.Kind {
	case store.KindBackfill:
		return rn.backfillMessage(ctx, r)
	case store.KindTranslate:
		return rn.translateMessage(ctx, r)
	}
	return sourceMessage(r) + "\n\n" + rn.dishList(ctx), nil
}

func sourceMessage(r *store.Record) string {
	if strings.TrimSpace(r.Pasted) != "" {
		src := strings.TrimSpace(r.PastedSource)
		if src == "" {
			src = r.SourceURL
		}
		msg := "A visitor pasted this recipe text. Use only this text; do not fetch anything."
		if src != "" {
			msg += " They gave its source as: " + src
		}
		return msg + "\n\n<pasted_recipe>\n" + r.Pasted + "\n</pasted_recipe>"
	}
	return "Tabulate the recipe at this URL: " + r.SourceURL
}

// drive watches the session until a Submission is accepted, the agent
// reports failure, or the Run gives up.
func (rn *Runner) drive(ctx context.Context, id, sid string) error {
	var lastMeter time.Time
	// The session's server_tool_use counter stays 0 for the agent
	// toolset's web_fetch (seen on the first real run), so fetches are
	// counted from the event history instead.
	var fetches int64
	for {
		select {
		case <-ctx.Done():
			return rn.fail(id, "timeout", fmt.Sprintf("the run passed its %s ceiling", RunCeiling))
		case <-time.After(pollEvery):
		}
		info, err := rn.API.Session(ctx, sid)
		if err != nil {
			continue // transient; the ceiling bounds us
		}
		if time.Since(lastMeter) > 10*time.Second || info.Status != "running" {
			info.Usage.Fetches = max(info.Usage.Fetches, fetches)
			rn.meter(ctx, id, info.Usage)
			lastMeter = time.Now()
		}
		switch info.Status {
		case "terminated":
			return rn.fail(id, "vendor", "the session terminated")
		case "idle":
		default:
			continue
		}
		evs, err := rn.API.Events(ctx, sid)
		if err != nil {
			continue
		}
		fetches = countFetches(evs)
		rn.meterFetches(ctx, id, fetches)
		done, err := rn.react(ctx, id, sid, evs)
		if err != nil || done {
			return err
		}
	}
}

// react handles one idle moment. It answers unanswered tool calls in
// order; otherwise it decides whether the idle is final.
func (rn *Runner) react(ctx context.Context, id, sid string, evs []Event) (bool, error) {
	answered := map[string]bool{}
	lastUser, lastIdle, stop := -1, -1, ""
	for i, e := range evs {
		switch e.Type {
		case "user.custom_tool_result":
			answered[e.ToolUseID] = true
			lastUser = i
		case "user.message":
			lastUser = i
		case "session.status_idle":
			lastIdle, stop = i, e.StopReason
		}
	}
	for _, e := range evs {
		if e.Type != "agent.custom_tool_use" || answered[e.ID] {
			continue
		}
		switch e.Name {
		case "submit_recipe":
			done, err := rn.submit(ctx, id, sid, e)
			if err != nil || done {
				return done, err
			}
		case "submit_translation":
			done, err := rn.submitTranslation(ctx, id, sid, e)
			if err != nil || done {
				return done, err
			}
		case "report_failure":
			var in struct{ Kind, Reason string }
			json.Unmarshal(e.Input, &in)
			if in.Kind == "" {
				in.Kind = "ambiguous"
			}
			rn.API.SendToolResult(ctx, sid, e.ID, "Recorded. Thank you.", false)
			_, err := rn.Store.Update(ctx, id, func(r *store.Record) error {
				r.Status, r.FailKind, r.FailReason = store.Failed, in.Kind, in.Reason
				r.LeaseUntil = time.Time{}
				r.Log("failure", in.Kind+": "+in.Reason)
				return nil
			})
			return true, err
		default:
			rn.API.SendToolResult(ctx, sid, e.ID, "Unknown tool.", true)
		}
		return false, nil // one answer per idle; the session runs again
	}
	if lastIdle < lastUser {
		return false, nil // the session hasn't reacted to our last event yet
	}
	switch stop {
	case "requires_action", "":
		return false, nil
	case "budget_reached":
		return true, rn.fail(id, "budget", fmt.Sprintf("the session reached its $%.2f budget", float64(SessionBudgetCents)/100))
	}
	rec, err := rn.Store.Get(ctx, id)
	if err != nil {
		return true, err
	}
	if !rec.Nudged {
		rn.Store.Update(ctx, id, func(r *store.Record) error {
			r.Nudged = true
			r.Log("nudge", "the agent ended its turn without submitting; asked it to call a tool")
			return nil
		})
		msg := "You ended your turn without calling a tool. Call submit_recipe with the tree, or report_failure if you cannot produce a faithful recipe."
		if rec.Kind == store.KindTranslate {
			msg = "You ended your turn without calling a tool. Call submit_translation with the translated card."
		}
		err := rn.API.SendMessage(ctx, sid, msg)
		return false, err
	}
	return true, rn.fail(id, "no_submission", "the agent ended without submitting or reporting a failure")
}

// submit validates one Submission and answers it. A card's Submission
// is a new Recipe; a backfill's must be its card unchanged plus the
// grouping fields (ADR 0017).
func (rn *Runner) submit(ctx context.Context, id, sid string, e Event) (bool, error) {
	rec, err := rn.Store.Get(ctx, id)
	if err != nil {
		return true, err
	}
	rejectWith := func(kind, msg string) (bool, error) {
		rn.reject(ctx, id, kind, msg)
		return false, rn.API.SendToolResult(ctx, sid, e.ID, "Rejected. Fix these and call submit_recipe again:\n"+msg, true)
	}
	var sub submission
	if err := json.NewDecoder(bytes.NewReader(e.Input)).Decode(&sub); err != nil {
		return rejectWith("schema", "- the input is not valid recipe JSON: "+err.Error())
	}
	rcp := sub.Recipe
	cardID := id
	if rec.Kind == store.KindBackfill {
		parent, err := rn.Store.Get(ctx, rec.Parent)
		if err != nil || parent.Recipe == nil {
			return true, rn.fail(id, "config", "the card to backfill is gone")
		}
		if ch := recipe.TreeChanges(parent.Recipe, &rcp); len(ch) > 0 {
			return rejectWith("tree", "- this card must not change; resubmit it exactly as given, adding only lang, label, dish and canon. Changed: "+strings.Join(ch, "; "))
		}
		rcp.Credit = parent.Recipe.Credit
		cardID = parent.ID
	} else {
		if rcp.Credit.URL == "" && rec.SourceURL != "" {
			rcp.Credit.URL = rec.SourceURL
		}
		if ps := recipe.Validate(&rcp); len(ps) > 0 {
			return rejectWith(ps[0].Kind, problems(ps))
		}
	}
	ps := recipe.ValidateGrouping(&rcp)
	if msg := rn.checkPick(ctx, sub.Dish); msg != "" {
		ps = append(ps, recipe.Problem{Kind: "schema", Message: msg})
	}
	if len(ps) > 0 {
		return rejectWith("schema", problems(ps))
	}

	mayNotFit := rec.MayNotFit
	var fitLine string
	if rec.Kind == store.KindCard {
		fit := card.Fit(&rcp)
		mayNotFit = false
		if !fit.OK {
			if rec.FitRejections < MaxFitTries {
				rn.reject(ctx, id, "fit", fit.Message)
				return false, rn.API.SendToolResult(ctx, sid, e.ID, "Rejected: "+fit.Message+" Then call submit_recipe again.", true)
			}
			mayNotFit = true
			fitLine = fmt.Sprintf("accepted after %d fit rejections; may not fit one page (%.1f pt)", MaxFitTries, fit.CellPt)
		} else {
			fitLine = fmt.Sprintf("accepted; prints %s at %.1f pt", orientation(fit), fit.CellPt)
		}
	}

	// The card takes the Recipe; a backfill job only records that it did.
	if _, err = rn.Store.Update(ctx, cardID, func(r *store.Record) error {
		r.Recipe = &rcp
		if r.Kind == store.KindCard && rec.Kind == store.KindCard {
			r.Meters.Submissions++
			r.MayNotFit = mayNotFit
			r.Status = store.Ready
			r.Pasted = "" // only the rewrite is kept (ADR 0012)
			r.LeaseUntil = time.Time{}
			r.FailKind, r.FailReason = "", ""
			r.Log("accepted", fitLine)
		} else {
			r.Log("backfill", "lang, label and canonical names added by "+id)
		}
		return nil
	}); err != nil {
		return true, err
	}
	filed, ferr := rn.file(ctx, cardID, sub.Dish, rcp.Lang)
	if ferr != nil {
		filed = "could not file into a dish: " + ferr.Error()
	}
	if rec.Kind != store.KindCard {
		rn.Store.Update(ctx, id, func(r *store.Record) error {
			r.Meters.Submissions++
			r.Status, r.LeaseUntil = store.Ready, time.Time{}
			r.Log("accepted", "card "+cardID+": "+filed)
			return nil
		})
	}
	rn.API.SendToolResult(ctx, sid, e.ID, "Accepted and saved. You are done.", false)
	if info, err := rn.API.Session(ctx, sid); err == nil {
		rn.meter(ctx, id, info.Usage)
	}
	return true, nil
}

func problems(ps []recipe.Problem) string {
	var lines []string
	for _, p := range ps {
		lines = append(lines, "- "+p.Message)
	}
	return strings.Join(lines, "\n")
}

func orientation(f card.FitResult) string {
	if f.Landscape {
		return "landscape"
	}
	return "portrait"
}

func (rn *Runner) reject(ctx context.Context, id, kind, msg string) {
	rn.Store.Update(ctx, id, func(r *store.Record) error {
		r.Meters.Submissions++
		switch kind {
		case "fit":
			r.Meters.RejectFit++
			r.FitRejections++
		case "tree":
			r.Meters.RejectTree++
			r.FitRejections = 0
		default:
			r.Meters.RejectSchema++
			r.FitRejections = 0
		}
		r.Log("rejected", kind+": "+msg)
		return nil
	})
}

func countFetches(evs []Event) int64 {
	var n int64
	for _, e := range evs {
		if e.Type == "agent.tool_use" && e.Name == "web_fetch" {
			n++
		}
	}
	return n
}

// meterFetches raises the current session's fetch reading to n.
func (rn *Runner) meterFetches(ctx context.Context, id string, n int64) {
	rn.Store.Update(ctx, id, func(r *store.Record) error {
		if n > r.SessionSeen.Fetches {
			r.Meters.Fetches += n - r.SessionSeen.Fetches
			r.SessionSeen.Fetches = n
		}
		return nil
	})
}

func (rn *Runner) meter(ctx context.Context, id string, u Usage) {
	rn.Store.Update(ctx, id, func(r *store.Record) error {
		r.AddSession(store.Usage(u))
		return nil
	})
}

// fail ends the Run as Failed; the record keeps its session id only for
// the Trace — a retry starts a new session.
func (rn *Runner) fail(id, kind, reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := rn.Store.Update(ctx, id, func(r *store.Record) error {
		r.Status, r.FailKind, r.FailReason = store.Failed, kind, reason
		r.LeaseUntil = time.Time{}
		r.Log("failure", kind+": "+reason)
		return nil
	})
	return err
}
