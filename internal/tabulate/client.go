package tabulate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// API is the slice of Claude Managed Agents a Run needs. Tests use a fake.
type API interface {
	EnsureAgent(ctx context.Context, a AgentSpec) (string, error)
	EnsureEnvironment(ctx context.Context, name string) (string, error)
	CreateSession(ctx context.Context, agentID, envID, title string, budgetCents int) (string, error)
	SendMessage(ctx context.Context, sessionID, text string) error
	SendToolResult(ctx context.Context, sessionID, toolUseID, text string, isError bool) error
	Session(ctx context.Context, sessionID string) (SessionInfo, error)
	Events(ctx context.Context, sessionID string) ([]Event, error)
}

// AgentSpec is one persona's desired configuration (rule 14).
type AgentSpec struct {
	Name   string
	Model  string
	System string
	Tools  []map[string]any
}

// SessionInfo is what a Run reads from the session object.
type SessionInfo struct {
	Status string // idle, running, rescheduling, terminated
	Usage  Usage
}

// Usage is the session's cumulative consumption.
type Usage struct {
	InputTokens   int64
	OutputTokens  int64
	CacheRead     int64
	CacheWrite    int64
	Fetches       int64
	ActiveSeconds float64
	ListCostCents int64
}

// Event is one entry of the session's event history, reduced to what a
// Run looks at.
type Event struct {
	ID         string
	Type       string
	Name       string          // agent.custom_tool_use
	Input      json.RawMessage // agent.custom_tool_use
	ToolUseID  string          // user.custom_tool_result
	StopReason string          // session.status_idle
	Text       string          // agent.message / session.error
}

// Client is a minimal hand-written REST client (rule 1): a handful of
// endpoints, no SDK.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey, baseURL: "https://api.anthropic.com", http: &http.Client{Timeout: 60 * time.Second}}
}

type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string { return fmt.Sprintf("anthropic: HTTP %d: %s", e.status, e.body) }

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	if in != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Event pages can carry fetched pages; allow generous bodies.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		s := strings.TrimSpace(string(raw))
		if len(s) > 400 {
			s = s[:400]
		}
		return &apiError{status: resp.StatusCode, body: s}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type listPage[T any] struct {
	Data     []T    `json:"data"`
	NextPage string `json:"next_page"`
}

func eachPage[T any](ctx context.Context, c *Client, path string, fn func(T) bool) error {
	page := ""
	for {
		q := "?limit=100"
		if page != "" {
			q += "&page=" + url.QueryEscape(page)
		}
		var pg listPage[T]
		if err := c.do(ctx, http.MethodGet, path+q, nil, &pg); err != nil {
			return err
		}
		for _, item := range pg.Data {
			if !fn(item) {
				return nil
			}
		}
		if pg.NextPage == "" || pg.NextPage == "null" {
			return nil
		}
		page = pg.NextPage
	}
}

type named struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

func (c *Client) findByName(ctx context.Context, path, name string) (*named, error) {
	var found *named
	err := eachPage(ctx, c, path, func(n named) bool {
		if n.Name == name {
			found = &n
			return false
		}
		return true
	})
	return found, err
}

// EnsureAgent creates the agent or pushes the desired config; an
// unchanged config creates no new version, so this is safe on every boot.
func (c *Client) EnsureAgent(ctx context.Context, a AgentSpec) (string, error) {
	found, err := c.findByName(ctx, "/v1/agents", a.Name)
	if err != nil {
		return "", err
	}
	cfg := map[string]any{"model": map[string]any{"id": a.Model}, "system": a.System, "tools": a.Tools}
	if found == nil {
		cfg["name"] = a.Name
		var created named
		if err := c.do(ctx, http.MethodPost, "/v1/agents", cfg, &created); err != nil {
			return "", err
		}
		return created.ID, nil
	}
	update := func(v int) error {
		cfg["version"] = v
		return c.do(ctx, http.MethodPost, "/v1/agents/"+found.ID, cfg, nil)
	}
	err = update(found.Version)
	var ae *apiError
	if errors.As(err, &ae) && ae.status == http.StatusConflict {
		var cur named
		if err := c.do(ctx, http.MethodGet, "/v1/agents/"+found.ID, nil, &cur); err != nil {
			return "", err
		}
		err = update(cur.Version)
	}
	if err != nil {
		return "", err
	}
	return found.ID, nil
}

// EnsureEnvironment creates the cloud environment if missing. Networking
// is unrestricted because limited networking would also restrict
// web_fetch to an allow-list; no sandbox tool is enabled (ADR 0006).
func (c *Client) EnsureEnvironment(ctx context.Context, name string) (string, error) {
	found, err := c.findByName(ctx, "/v1/environments", name)
	if err != nil {
		return "", err
	}
	if found != nil {
		return found.ID, nil
	}
	var created named
	err = c.do(ctx, http.MethodPost, "/v1/environments", map[string]any{
		"name":   name,
		"config": map[string]any{"type": "cloud", "networking": map[string]any{"type": "unrestricted"}},
	}, &created)
	return created.ID, err
}

func (c *Client) CreateSession(ctx context.Context, agentID, envID, title string, budgetCents int) (string, error) {
	body := map[string]any{"agent": agentID, "environment_id": envID, "title": title}
	if budgetCents > 0 {
		body["budget"] = map[string]any{
			"type":          "limit",
			"max_list_cost": map[string]any{"amount": strconv.Itoa(budgetCents), "currency": "USD"},
		}
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/sessions", body, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *Client) SendMessage(ctx context.Context, sessionID, text string) error {
	return c.do(ctx, http.MethodPost, "/v1/sessions/"+sessionID+"/events", map[string]any{
		"events": []map[string]any{{"type": "user.message", "content": []map[string]any{{"type": "text", "text": text}}}},
	}, nil)
}

func (c *Client) SendToolResult(ctx context.Context, sessionID, toolUseID, text string, isError bool) error {
	return c.do(ctx, http.MethodPost, "/v1/sessions/"+sessionID+"/events", map[string]any{
		"events": []map[string]any{{
			"type":               "user.custom_tool_result",
			"custom_tool_use_id": toolUseID,
			"content":            []map[string]any{{"type": "text", "text": text}},
			"is_error":           isError,
		}},
	}, nil)
}

func (c *Client) Session(ctx context.Context, sessionID string) (SessionInfo, error) {
	var s struct {
		Status string `json:"status"`
		Usage  struct {
			InputTokens   int64 `json:"input_tokens"`
			OutputTokens  int64 `json:"output_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			CacheCreation struct {
				Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
				Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
			ServerToolUse map[string]int64 `json:"server_tool_use"`
			ListCost      struct {
				Amount string `json:"amount"`
			} `json:"list_cost"`
			ActiveSeconds float64 `json:"active_seconds"`
		} `json:"usage"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/sessions/"+sessionID, nil, &s); err != nil {
		return SessionInfo{}, err
	}
	u := Usage{
		InputTokens:   s.Usage.InputTokens,
		OutputTokens:  s.Usage.OutputTokens,
		CacheRead:     s.Usage.CacheRead,
		CacheWrite:    s.Usage.CacheCreation.Ephemeral5m + s.Usage.CacheCreation.Ephemeral1h,
		ActiveSeconds: s.Usage.ActiveSeconds,
	}
	for k, v := range s.Usage.ServerToolUse {
		if strings.Contains(k, "fetch") {
			u.Fetches += v
		}
	}
	u.ListCostCents, _ = strconv.ParseInt(s.Usage.ListCost.Amount, 10, 64)
	return SessionInfo{Status: s.Status, Usage: u}, nil
}

type rawEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	ToolUseID  string          `json:"custom_tool_use_id"`
	StopReason json.RawMessage `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error json.RawMessage `json:"error"`
}

func (c *Client) Events(ctx context.Context, sessionID string) ([]Event, error) {
	var out []Event
	err := eachPage(ctx, c, "/v1/sessions/"+sessionID+"/events", func(e rawEvent) bool {
		ev := Event{ID: e.ID, Type: e.Type, Name: e.Name, Input: e.Input, ToolUseID: e.ToolUseID}
		if len(e.StopReason) > 0 {
			var sr struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(e.StopReason, &sr) == nil && sr.Type != "" {
				ev.StopReason = sr.Type
			} else {
				var s string
				json.Unmarshal(e.StopReason, &s)
				ev.StopReason = s
			}
		}
		for _, b := range e.Content {
			if b.Type == "text" {
				ev.Text += b.Text
			}
		}
		if ev.Text == "" && len(e.Error) > 0 {
			ev.Text = string(e.Error)
		}
		// Fetched pages are bulky and the Run never reads them.
		if e.Type != "agent.custom_tool_use" {
			ev.Input = nil
		}
		out = append(out, ev)
		return true
	})
	return out, err
}
