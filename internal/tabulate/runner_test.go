package tabulate

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
	"github.com/nicocesar/tabular_recipes/internal/store"
)

// fakeAPI plays an agent that answers each user event with the next
// scripted tool call (or nothing, which ends its turn).
type fakeAPI struct {
	mu      sync.Mutex
	script  []func() Event // nil entry: end turn without a tool
	events  []Event
	results []string
	n       int
}

func (f *fakeAPI) EnsureAgent(context.Context, AgentSpec) (string, error)    { return "agent", nil }
func (f *fakeAPI) EnsureEnvironment(context.Context, string) (string, error) { return "env", nil }
func (f *fakeAPI) CreateSession(context.Context, string, string, string, int) (string, error) {
	return "sess", nil
}

func (f *fakeAPI) step() {
	if len(f.script) == 0 {
		f.events = append(f.events, Event{Type: "session.status_idle", StopReason: "end_turn"})
		return
	}
	next := f.script[0]
	f.script = f.script[1:]
	if next == nil {
		f.events = append(f.events, Event{Type: "session.status_idle", StopReason: "end_turn"})
		return
	}
	f.n++
	ev := next()
	ev.ID = "tu" + string(rune('0'+f.n))
	f.events = append(f.events, Event{Type: "agent.tool_use", Name: "web_fetch"}, ev, Event{Type: "session.status_idle", StopReason: "requires_action"})
}

func (f *fakeAPI) SendMessage(_ context.Context, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, Event{Type: "user.message", Text: text})
	f.step()
	return nil
}

func (f *fakeAPI) SendToolResult(_ context.Context, _, id, text string, isErr bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, text)
	f.events = append(f.events, Event{Type: "user.custom_tool_result", ToolUseID: id})
	if isErr {
		f.step()
	}
	return nil
}

func (f *fakeAPI) Session(context.Context, string) (SessionInfo, error) {
	return SessionInfo{Status: "idle", Usage: Usage{InputTokens: 1000, OutputTokens: 200, ListCostCents: 3}}, nil
}

func (f *fakeAPI) Events(context.Context, string) ([]Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Event{}, f.events...), nil
}

func submit(t *testing.T, edit func(m map[string]any)) func() Event {
	b, err := os.ReadFile("../recipe/testdata/pancakes.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	m["dish"] = map[string]any{"name": "Pancakes"}
	if edit != nil {
		edit(m)
	}
	in, _ := json.Marshal(m)
	return func() Event { return Event{Type: "agent.custom_tool_use", Name: "submit_recipe", Input: in} }
}

func setup(t *testing.T, f *fakeAPI) (*Runner, string) {
	t.Helper()
	st, err := store.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := store.NewID()
	if err := st.Create(context.Background(), &store.Record{ID: id, Status: store.Dispatched, SourceURL: "https://example.com/pancakes", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return &Runner{Store: st, API: f, AgentID: "agent", EnvID: "env"}, id
}

func init() { pollEvery = time.Millisecond }

func TestRejectThenAccept(t *testing.T) {
	f := &fakeAPI{script: []func() Event{
		submit(t, func(m map[string]any) { // i8 left unused
			steps := m["steps"].([]any)
			steps[3].(map[string]any)["inputs"] = []any{"s3"}
		}),
		submit(t, nil),
	}}
	rn, id := setup(t, f)
	if err := rn.Run(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r, _ := rn.Store.Get(context.Background(), id)
	if r.Status != store.Ready || r.Recipe == nil {
		t.Fatalf("status %s", r.Status)
	}
	if r.Meters.RejectTree != 1 || r.Meters.Submissions != 2 {
		t.Fatalf("meters %+v", r.Meters)
	}
	if !strings.Contains(f.results[0], "never used") || !strings.Contains(f.results[1], "Accepted") {
		t.Fatalf("results %q", f.results)
	}
	if r.Meters.InputTokens != 1000 || r.Meters.ListCostCents != 3 || r.Meters.Fetches != 2 {
		t.Fatalf("usage not metered: %+v", r.Meters.Usage)
	}
}

func TestReportFailure(t *testing.T) {
	in, _ := json.Marshal(map[string]string{"kind": "fetch_blocked", "reason": "403 from the site"})
	f := &fakeAPI{script: []func() Event{func() Event {
		return Event{Type: "agent.custom_tool_use", Name: "report_failure", Input: in}
	}}}
	rn, id := setup(t, f)
	rn.Run(context.Background(), id)
	r, _ := rn.Store.Get(context.Background(), id)
	if r.Status != store.Failed || r.FailKind != "fetch_blocked" {
		t.Fatalf("%s %s", r.Status, r.FailKind)
	}
}

func TestSilentAgentIsNudgedThenFails(t *testing.T) {
	f := &fakeAPI{script: []func() Event{nil, nil}}
	rn, id := setup(t, f)
	rn.Run(context.Background(), id)
	r, _ := rn.Store.Get(context.Background(), id)
	if r.Status != store.Failed || r.FailKind != "no_submission" || !r.Nudged {
		t.Fatalf("%s %s nudged=%v", r.Status, r.FailKind, r.Nudged)
	}
}

func TestLeaseBlocksSecondRun(t *testing.T) {
	f := &fakeAPI{}
	rn, id := setup(t, f)
	rn.Store.Update(context.Background(), id, func(r *store.Record) error {
		r.LeaseUntil = time.Now().Add(time.Minute)
		return nil
	})
	if err := rn.Run(context.Background(), id); err != ErrLeased {
		t.Fatalf("got %v", err)
	}
}

func TestTickResumesExpiredLease(t *testing.T) {
	f := &fakeAPI{script: []func() Event{submit(t, nil)}}
	rn, id := setup(t, f)
	rn.Store.Update(context.Background(), id, func(r *store.Record) error {
		r.Status = store.Running
		r.LeaseUntil = time.Now().Add(-time.Minute)
		return nil
	})
	res := rn.Tick(context.Background())
	if len(res.Redispatched) != 1 {
		t.Fatalf("%+v", res)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := rn.Store.Get(context.Background(), id)
		if r.Status == store.Ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resumed run did not finish")
}

// fileCard runs one card through a fresh Run on rn's store.
func fileCard(t *testing.T, rn *Runner, f *fakeAPI, url string) *store.Record {
	t.Helper()
	ctx := context.Background()
	id := store.NewID()
	if err := rn.Store.Create(ctx, &store.Record{ID: id, Status: store.Dispatched, SourceURL: url, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	rn.API = f
	if err := rn.Run(ctx, id); err != nil {
		t.Fatal(err)
	}
	r, _ := rn.Store.Get(ctx, id)
	return r
}

// A first card makes its Dish; a copy folds into it; a different
// version becomes variant 2; an unknown dish id is rejected.
func TestDishesAndFolding(t *testing.T) {
	ctx := context.Background()
	rn, first := setup(t, &fakeAPI{script: []func() Event{submit(t, nil)}})
	if err := rn.Run(ctx, first); err != nil {
		t.Fatal(err)
	}
	a, _ := rn.Store.Get(ctx, first)
	if a.DishID == "" || a.Variant != 1 {
		t.Fatalf("first card: dish %q variant %d", a.DishID, a.Variant)
	}
	d, _ := rn.Store.GetDish(ctx, a.DishID)
	if d.Reference != a.ID || d.Names["en"] != "Pancakes" {
		t.Fatalf("dish %+v", d)
	}

	pick := func(m map[string]any) { m["dish"] = map[string]any{"id": a.DishID} }
	copyCard := fileCard(t, rn, &fakeAPI{script: []func() Event{submit(t, func(m map[string]any) {
		pick(m)
		m["steps"].([]any)[0].(map[string]any)["text"] = "whisk well" // trees differ; still a copy
	})}}, "https://copies.example/pancakes")
	if copyCard.Status != store.Folded || copyCard.FoldedInto != a.ID {
		t.Fatalf("copy: %s into %q", copyCard.Status, copyCard.FoldedInto)
	}
	a, _ = rn.Store.Get(ctx, first)
	if len(a.AlsoAt) != 1 || a.AlsoAt[0].URL != "https://copies.example/pancakes" {
		t.Fatalf("also at: %+v", a.AlsoAt)
	}

	f := &fakeAPI{script: []func() Event{
		submit(t, func(m map[string]any) { m["dish"] = map[string]any{"id": "nope"} }),
		submit(t, func(m map[string]any) {
			pick(m)
			ings := m["ingredients"].([]any)
			ings[0].(map[string]any)["qty"] = "3 cups"
			ings[1].(map[string]any)["qty"] = "a lot"
			m["label"] = "extra flour"
		}),
	}}
	other := fileCard(t, rn, f, "https://other.example/pancakes")
	if !strings.Contains(f.results[0], "not in the list") {
		t.Fatalf("unknown dish id should be rejected: %q", f.results[0])
	}
	if other.Status != store.Ready || other.DishID != a.DishID || other.Variant != 2 {
		t.Fatalf("variant: %s dish %q n %d", other.Status, other.DishID, other.Variant)
	}
}

// A backfill resubmits its card unchanged plus the grouping fields; a
// changed tree is rejected.
func TestBackfill(t *testing.T) {
	ctx := context.Background()
	rn, cardID := setup(t, &fakeAPI{})
	b, _ := os.ReadFile("../recipe/testdata/pancakes.json")
	var old recipe.Recipe
	json.Unmarshal(b, &old)
	old.Lang, old.Label = "", ""
	for i := range old.Ingredients {
		old.Ingredients[i].Canon = ""
	}
	rn.Store.Update(ctx, cardID, func(r *store.Record) error {
		r.Status, r.Recipe = store.Ready, &old
		return nil
	})
	rn.API = &fakeAPI{script: []func() Event{
		submit(t, func(m map[string]any) { m["title"] = "Better Pancakes" }),
		submit(t, nil),
	}}
	job, err := rn.StartBackfill(ctx)
	if err != nil || job == "" {
		t.Fatalf("start: %q %v", job, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if j, _ := rn.Store.Get(ctx, job); j.Status == store.Ready || j.Status == store.Failed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := rn.Store.Get(ctx, job)
	c, _ := rn.Store.Get(ctx, cardID)
	if j.Status != store.Ready || j.Meters.RejectTree != 1 {
		t.Fatalf("job %s, tree rejections %d", j.Status, j.Meters.RejectTree)
	}
	if c.DishID == "" || c.Recipe.Label == "" || c.Recipe.Ingredients[0].Canon == "" || c.Recipe.Title != old.Title {
		t.Fatalf("card not backfilled: %+v", c.Recipe)
	}
	if next, _ := rn.BackfillNext(ctx); next != "" {
		t.Fatal("nothing should be left to backfill")
	}
}
