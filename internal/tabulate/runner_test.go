package tabulate

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
