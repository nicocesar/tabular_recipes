package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/datastore"
)

// Datastore is the production Store (Firestore in Datastore mode). Only
// the properties queried by equality are indexed; the record itself is
// an unindexed JSON body (ADR 0010).
type Datastore struct {
	c *datastore.Client
}

func NewDatastore(ctx context.Context, project string) (*Datastore, error) {
	c, err := datastore.NewClient(ctx, project)
	if err != nil {
		return nil, err
	}
	return &Datastore{c: c}, nil
}

type recipeEntity struct {
	Status    string
	NormURL   string
	CreatedAt time.Time
	Body      string `datastore:",noindex"`
}

type counterEntity struct {
	N int `datastore:",noindex"`
}

type metaEntity struct {
	Body string `datastore:",noindex"`
}

func rkey(id string) *datastore.Key { return datastore.NameKey("Recipe", id, nil) }

func toEntity(r *Record) (*recipeEntity, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return &recipeEntity{Status: string(r.Status), NormURL: r.NormURL, CreatedAt: r.CreatedAt, Body: string(b)}, nil
}

func fromEntity(e *recipeEntity) (*Record, error) {
	var r Record
	if err := json.Unmarshal([]byte(e.Body), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Datastore) Get(ctx context.Context, id string) (*Record, error) {
	var e recipeEntity
	if err := s.c.Get(ctx, rkey(id), &e); err != nil {
		if errors.Is(err, datastore.ErrNoSuchEntity) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return fromEntity(&e)
}

func (s *Datastore) Create(ctx context.Context, r *Record) error {
	r.UpdatedAt = time.Now().UTC()
	e, err := toEntity(r)
	if err != nil {
		return err
	}
	_, err = s.c.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var cur recipeEntity
		if err := tx.Get(rkey(r.ID), &cur); err == nil {
			return fmt.Errorf("store: %s exists", r.ID)
		} else if !errors.Is(err, datastore.ErrNoSuchEntity) {
			return err
		}
		_, err := tx.Put(rkey(r.ID), e)
		return err
	})
	return err
}

func (s *Datastore) Update(ctx context.Context, id string, fn func(*Record) error) (*Record, error) {
	var out *Record
	_, err := s.c.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var e recipeEntity
		if err := tx.Get(rkey(id), &e); err != nil {
			if errors.Is(err, datastore.ErrNoSuchEntity) {
				return ErrNotFound
			}
			return err
		}
		r, err := fromEntity(&e)
		if err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
		r.UpdatedAt = time.Now().UTC()
		ne, err := toEntity(r)
		if err != nil {
			return err
		}
		if _, err := tx.Put(rkey(id), ne); err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (s *Datastore) Delete(ctx context.Context, id string) error {
	return s.c.Delete(ctx, rkey(id))
}

func (s *Datastore) query(ctx context.Context, field, value string) ([]*Record, error) {
	var es []*recipeEntity
	if _, err := s.c.GetAll(ctx, datastore.NewQuery("Recipe").FilterField(field, "=", value), &es); err != nil {
		return nil, err
	}
	out := make([]*Record, 0, len(es))
	for _, e := range es {
		r, err := fromEntity(e)
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Datastore) ByStatus(ctx context.Context, st Status) ([]*Record, error) {
	return s.query(ctx, "Status", string(st))
}

func (s *Datastore) ByNormURL(ctx context.Context, norm string) ([]*Record, error) {
	return s.query(ctx, "NormURL", norm)
}

func ckey(key string) *datastore.Key { return datastore.NameKey("Counter", key, nil) }

func (s *Datastore) Incr(ctx context.Context, key string, limit int) (int, bool, error) {
	var n int
	var ok bool
	_, err := s.c.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var c counterEntity
		if err := tx.Get(ckey(key), &c); err != nil && !errors.Is(err, datastore.ErrNoSuchEntity) {
			return err
		}
		n, ok = c.N, false
		if c.N >= limit {
			return nil
		}
		c.N++
		n, ok = c.N, true
		_, err := tx.Put(ckey(key), &c)
		return err
	})
	return n, ok, err
}

func (s *Datastore) Count(ctx context.Context, key string) (int, error) {
	var c counterEntity
	if err := s.c.Get(ctx, ckey(key), &c); err != nil && !errors.Is(err, datastore.ErrNoSuchEntity) {
		return 0, err
	}
	return c.N, nil
}

func (s *Datastore) GetMeta(ctx context.Context, key string, v any) error {
	var m metaEntity
	if err := s.c.Get(ctx, datastore.NameKey("Meta", key, nil), &m); err != nil {
		if errors.Is(err, datastore.ErrNoSuchEntity) {
			return ErrNotFound
		}
		return err
	}
	return json.Unmarshal([]byte(m.Body), v)
}

func (s *Datastore) PutMeta(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.c.Put(ctx, datastore.NameKey("Meta", key, nil), &metaEntity{Body: string(b)})
	return err
}
