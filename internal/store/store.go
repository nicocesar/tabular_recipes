// Package store keeps Recipes (as records with their Run state) and the
// daily counters, behind one interface the server owns (ADR 0001).
// Production is Datastore; development is a directory of JSON files.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/recipe"
)

// Status is where a Recipe is in its Run (CONTEXT.md).
type Status string

const (
	Dispatched Status = "dispatched"
	Running    Status = "running"
	Ready      Status = "ready"
	Failed     Status = "failed"
	// Folded: a copy of an existing Variant, hidden; FoldedInto names it
	// (ADR 0018).
	Folded Status = "folded"
)

// Kinds of record. A card is the default; the others are jobs that run
// on the same Run path for a Parent card (ADRs 0014, 0017).
const (
	KindCard      = ""
	KindBackfill  = "backfill"
	KindTranslate = "translate"
)

// MaxTrace bounds the Trace so a record stays far under Datastore's
// 1 MB entity limit; the oldest steps after the first few are dropped.
const MaxTrace = 200

// Record is one Recipe with everything about how it was made.
type Record struct {
	ID        string    `json:"id"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// The source: a URL, pasted text, or both.
	SourceURL    string `json:"source_url,omitempty"`
	NormURL      string `json:"norm_url,omitempty"`
	Pasted       string `json:"pasted,omitempty"` // dropped once Ready (ADR 0012)
	PastedSource string `json:"pasted_source,omitempty"`
	WasPasted    bool   `json:"was_pasted,omitempty"`
	IPHash       string `json:"ip_hash,omitempty"`

	Recipe    *recipe.Recipe `json:"recipe,omitempty"`
	MayNotFit bool           `json:"may_not_fit,omitempty"`

	// Kind is "" for a card; a job names its card in Parent.
	Kind   string `json:"kind,omitempty"`
	Parent string `json:"parent,omitempty"`

	// The Dish (ADR 0017). Variant is this card's number in it.
	DishID  string `json:"dish_id,omitempty"`
	Variant int    `json:"variant,omitempty"`
	// FoldedInto is the Variant this copy was folded into; AlsoAt is a
	// Variant's list of folded copies' sources (ADR 0018).
	FoldedInto string   `json:"folded_into,omitempty"`
	AlsoAt     []Source `json:"also_at,omitempty"`
	// BackfillTried marks a card the backfill chain has already visited.
	BackfillTried bool `json:"backfill_tried,omitempty"`

	// The Run.
	SessionID     string    `json:"session_id,omitempty"`
	LeaseUntil    time.Time `json:"lease_until,omitempty"`
	DispatchedAt  time.Time `json:"dispatched_at,omitempty"`
	Resumes       int       `json:"resumes,omitempty"`        // of the current Run
	FitRejections int       `json:"fit_rejections,omitempty"` // consecutive, current Run
	Nudged        bool      `json:"nudged,omitempty"`         // current Run was told to submit
	FailKind      string    `json:"fail_kind,omitempty"`
	FailReason    string    `json:"fail_reason,omitempty"`

	Views  int    `json:"views,omitempty"`
	Meters Meters `json:"meters"`
	// Snapshot of the current session's cumulative usage, so the next
	// reading adds only the difference to Meters.
	SessionSeen Usage       `json:"session_seen"`
	Trace       []TraceStep `json:"trace,omitempty"`
}

// Source is where a folded copy came from.
type Source struct {
	URL    string `json:"url,omitempty"` // empty for pasted text
	Record string `json:"record"`
}

// Dish groups Variants (ADR 0017). It has no language of its own.
type Dish struct {
	ID        string            `json:"id"`
	Names     map[string]string `json:"names"` // lang → name
	Variants  []DishVariant     `json:"variants"`
	Reference string            `json:"reference"` // a record id
	Next      int               `json:"next"`      // the next variant number
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// DishVariant is one card in a Dish, in filing order.
type DishVariant struct {
	Record string `json:"record"`
	N      int    `json:"n"`
}

// Name is the Dish's name in lang, else in the Reference's language,
// else any; and the language it is in.
func (d *Dish) Name(lang, refLang string) (string, string) {
	for _, l := range []string{lang, refLang, "en"} {
		if n := d.Names[l]; n != "" {
			return n, l
		}
	}
	for l, n := range d.Names {
		return n, l
	}
	return "", ""
}

// Add files a record as the next Variant; the first becomes Reference.
func (d *Dish) Add(record string) int {
	if d.Next == 0 {
		d.Next = 1
	}
	n := d.Next
	d.Next++
	d.Variants = append(d.Variants, DishVariant{record, n})
	if d.Reference == "" {
		d.Reference = record
	}
	return n
}

// Remove takes a record out; a removed Reference passes to the oldest
// remaining Variant. Numbers are never reused.
func (d *Dish) Remove(record string) {
	out := d.Variants[:0]
	for _, v := range d.Variants {
		if v.Record != record {
			out = append(out, v)
		}
	}
	d.Variants = out
	if d.Reference == record {
		d.Reference = ""
		if len(out) > 0 {
			d.Reference = out[0].Record
		}
	}
}

// Usage is what a session reports, cumulatively.
type Usage struct {
	InputTokens   int64   `json:"input_tokens,omitempty"`
	OutputTokens  int64   `json:"output_tokens,omitempty"`
	CacheRead     int64   `json:"cache_read,omitempty"`
	CacheWrite    int64   `json:"cache_write,omitempty"`
	Fetches       int64   `json:"fetches,omitempty"`
	ActiveSeconds float64 `json:"active_seconds,omitempty"`
	ListCostCents int64   `json:"list_cost_cents,omitempty"`
}

// Meters accumulate across every Run of a record (rule 12).
type Meters struct {
	Usage
	Sessions     int `json:"sessions,omitempty"`
	Submissions  int `json:"submissions,omitempty"`
	RejectSchema int `json:"reject_schema,omitempty"`
	RejectTree   int `json:"reject_tree,omitempty"`
	RejectFit    int `json:"reject_fit,omitempty"`
	ResumesTotal int `json:"resumes_total,omitempty"`
	Retries      int `json:"retries,omitempty"`
}

// AddSession folds a fresh cumulative session reading into Meters.
func (r *Record) AddSession(u Usage) {
	d := Usage{
		InputTokens:   u.InputTokens - r.SessionSeen.InputTokens,
		OutputTokens:  u.OutputTokens - r.SessionSeen.OutputTokens,
		CacheRead:     u.CacheRead - r.SessionSeen.CacheRead,
		CacheWrite:    u.CacheWrite - r.SessionSeen.CacheWrite,
		Fetches:       u.Fetches - r.SessionSeen.Fetches,
		ActiveSeconds: u.ActiveSeconds - r.SessionSeen.ActiveSeconds,
		ListCostCents: u.ListCostCents - r.SessionSeen.ListCostCents,
	}
	m := &r.Meters.Usage
	m.InputTokens += max(d.InputTokens, 0)
	m.OutputTokens += max(d.OutputTokens, 0)
	m.CacheRead += max(d.CacheRead, 0)
	m.CacheWrite += max(d.CacheWrite, 0)
	m.Fetches += max(d.Fetches, 0)
	m.ActiveSeconds += max(d.ActiveSeconds, 0)
	m.ListCostCents += max(d.ListCostCents, 0)
	u.Fetches = max(u.Fetches, r.SessionSeen.Fetches)
	r.SessionSeen = u
}

// TraceStep is one line of a record's operational history (rule 16).
type TraceStep struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Msg  string    `json:"msg"`
}

// Log appends to the Trace, keeping it bounded.
func (r *Record) Log(kind, msg string) {
	r.Trace = append(r.Trace, TraceStep{At: time.Now().UTC(), Kind: kind, Msg: msg})
	if len(r.Trace) > MaxTrace {
		keep := append([]TraceStep{}, r.Trace[:5]...)
		keep = append(keep, TraceStep{At: time.Now().UTC(), Kind: "trace", Msg: "older steps dropped"})
		r.Trace = append(keep, r.Trace[len(r.Trace)-(MaxTrace-6):]...)
	}
}

// Version identifies this state of the record, for ETags.
func (r *Record) Version() string {
	return r.UpdatedAt.UTC().Format("20060102T150405.000000000")
}

var ErrNotFound = errors.New("store: not found")

// Store is everything the server keeps.
type Store interface {
	Get(ctx context.Context, id string) (*Record, error)
	// Create stores a new record; its ID must be unused.
	Create(ctx context.Context, r *Record) error
	// Update runs fn on the current record inside a transaction and
	// stores the result. If fn returns an error nothing is written.
	Update(ctx context.Context, id string, fn func(*Record) error) (*Record, error)
	Delete(ctx context.Context, id string) error
	// Dishes (ADR 0017). UpdateDish runs fn inside a transaction.
	GetDish(ctx context.Context, id string) (*Dish, error)
	Dishes(ctx context.Context) ([]*Dish, error)
	CreateDish(ctx context.Context, d *Dish) error
	UpdateDish(ctx context.Context, id string, fn func(*Dish) error) (*Dish, error)
	DeleteDish(ctx context.Context, id string) error
	// CreateUnique stores r unless the dedupe key already names a record
	// that live accepts; then it returns that record and stores nothing.
	// Claim and record are written together, so of two simultaneous
	// submissions only one is created.
	CreateUnique(ctx context.Context, key string, r *Record, live func(*Record) bool) (*Record, error)
	ByStatus(ctx context.Context, s Status) ([]*Record, error)
	ByNormURL(ctx context.Context, norm string) ([]*Record, error)
	// Incr adds one to a named counter unless it is already at limit;
	// it returns the new value and whether it was incremented.
	Incr(ctx context.Context, key string, limit int) (int, bool, error)
	Count(ctx context.Context, key string) (int, error)
	// GetMeta/PutMeta keep small JSON values such as the last Tick.
	GetMeta(ctx context.Context, key string, v any) error
	PutMeta(ctx context.Context, key string, v any) error
}

// claimName turns a dedupe key (a normalised URL or a paste hash) into
// a short fixed-length name.
func claimName(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:16])
}

type claim struct {
	ID string `json:"id"`
}

// NewID is a short random id, safe in URLs.
func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}
