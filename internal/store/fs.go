package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FS is the development Store: one JSON file per record under dir. It
// serialises all access with one mutex — it is for one process.
type FS struct {
	dir string
	mu  sync.Mutex
}

func NewFS(dir string) (*FS, error) {
	for _, d := range []string{"recipes", "counters", "meta"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	return &FS{dir: dir}, nil
}

func (s *FS) path(kind, key string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '.' {
			return '_'
		}
		return r
	}, key)
	return filepath.Join(s.dir, kind, safe+".json")
}

func (s *FS) read(kind, key string, v any) error {
	b, err := os.ReadFile(s.path(kind, key))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (s *FS) write(kind, key string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(kind, key) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(kind, key))
}

func (s *FS) Get(_ context.Context, id string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r Record
	if err := s.read("recipes", id, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *FS) Create(_ context.Context, r *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path("recipes", r.ID)); err == nil {
		return fmt.Errorf("store: %s exists", r.ID)
	}
	r.UpdatedAt = time.Now().UTC()
	return s.write("recipes", r.ID, r)
}

func (s *FS) Update(_ context.Context, id string, fn func(*Record) error) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r Record
	if err := s.read("recipes", id, &r); err != nil {
		return nil, err
	}
	if err := fn(&r); err != nil {
		return nil, err
	}
	r.UpdatedAt = time.Now().UTC()
	if err := s.write("recipes", id, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *FS) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path("recipes", id))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

func (s *FS) all() ([]*Record, error) {
	ents, err := os.ReadDir(filepath.Join(s.dir, "recipes"))
	if err != nil {
		return nil, err
	}
	var out []*Record
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var r Record
		if err := s.read("recipes", strings.TrimSuffix(e.Name(), ".json"), &r); err != nil {
			continue
		}
		out = append(out, &r)
	}
	return out, nil
}

func (s *FS) ByStatus(_ context.Context, st Status) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.all()
	var out []*Record
	for _, r := range all {
		if r.Status == st {
			out = append(out, r)
		}
	}
	return out, err
}

func (s *FS) ByNormURL(_ context.Context, norm string) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.all()
	var out []*Record
	for _, r := range all {
		if r.NormURL == norm {
			out = append(out, r)
		}
	}
	return out, err
}

func (s *FS) Incr(_ context.Context, key string, limit int) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.read("counters", key, &n); err != nil && !errors.Is(err, ErrNotFound) {
		return 0, false, err
	}
	if n >= limit {
		return n, false, nil
	}
	n++
	return n, true, s.write("counters", key, n)
}

func (s *FS) Count(_ context.Context, key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.read("counters", key, &n); err != nil && !errors.Is(err, ErrNotFound) {
		return 0, err
	}
	return n, nil
}

func (s *FS) GetMeta(_ context.Context, key string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read("meta", key, v)
}

func (s *FS) PutMeta(_ context.Context, key string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write("meta", key, v)
}
