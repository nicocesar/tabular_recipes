package tabulate

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed demo_recipe.json
var demoRecipe []byte

// Demo is a stand-in agent for `make demo` without an API key
// (DEMO_AGENT=1): after a short pause it submits one fixed recipe, so
// the whole flow — dispatch, polling, the card — can be seen for free.
// It is never used when ANTHROPIC_API_KEY is set.
type Demo struct {
	mu     sync.Mutex
	events map[string][]Event
}

func NewDemo() *Demo { return &Demo{events: map[string][]Event{}} }

func (d *Demo) EnsureAgent(context.Context, AgentSpec) (string, error)    { return "demo-agent", nil }
func (d *Demo) EnsureEnvironment(context.Context, string) (string, error) { return "demo-env", nil }

func (d *Demo) CreateSession(_ context.Context, _, _, _ string, _ int) (string, error) {
	return "demo-session-" + time.Now().Format("150405.000"), nil
}

func (d *Demo) SendMessage(_ context.Context, sid, text string) error {
	d.mu.Lock()
	d.events[sid] = append(d.events[sid], Event{Type: "user.message", Text: text})
	d.mu.Unlock()
	time.AfterFunc(4*time.Second, func() {
		var m map[string]any
		if card, ok := between(text, "<card>\n", "\n</card>"); ok {
			// A backfill: the card as given, plus the grouping fields.
			json.Unmarshal([]byte(card), &m)
			m["lang"], m["label"] = "en", "as first filed"
			for _, in := range m["ingredients"].([]any) {
				in := in.(map[string]any)
				in["canon"] = strings.ToLower(fmt.Sprint(in["item"]))
			}
			m["dish"] = map[string]any{"name": fmt.Sprint(m["title"])}
		} else {
			json.Unmarshal(demoRecipe, &m)
			m["credit"] = map[string]any{"source_title": "Demo pancakes (DEMO_AGENT)"}
		}
		// Join an existing dish of the same name, as the real agent would.
		if name, _ := m["dish"].(map[string]any)["name"].(string); name != "" {
			if id := demoDishID.FindStringSubmatch(text); id != nil && strings.Contains(id[2], name) {
				m["dish"] = map[string]any{"id": id[1]}
			}
		}
		in, _ := json.Marshal(m)
		d.mu.Lock()
		defer d.mu.Unlock()
		d.events[sid] = append(d.events[sid],
			Event{ID: "demo-tu", Type: "agent.custom_tool_use", Name: "submit_recipe", Input: in},
			Event{Type: "session.status_idle", StopReason: "requires_action"})
	})
	return nil
}

var demoDishID = regexp.MustCompile(`(?m)^- (\w+): (.*)$`)

func between(s, a, b string) (string, bool) {
	_, rest, ok := strings.Cut(s, a)
	if !ok {
		return "", false
	}
	in, _, ok := strings.Cut(rest, b)
	return in, ok
}

func (d *Demo) SendToolResult(_ context.Context, sid, id, _ string, _ bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events[sid] = append(d.events[sid], Event{Type: "user.custom_tool_result", ToolUseID: id})
	return nil
}

func (d *Demo) Session(context.Context, string) (SessionInfo, error) {
	return SessionInfo{Status: "idle", Usage: Usage{InputTokens: 4200, OutputTokens: 900, Fetches: 1, ListCostCents: 2}}, nil
}

func (d *Demo) Events(_ context.Context, sid string) ([]Event, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Event{}, d.events[sid]...), nil
}
