package main

// Favourites and history in Queen's KV, namespace `app`.
//
//	fav:<user>  -> [{id, name, lat, lon, kind, createdAt}]
//	hist:<user> -> [{id, at, request{...}, summary{...}}], newest first
//
// Both are ONE key each, so a list is one round trip. The broker caps a KV
// value at 64 KB: the arrays are capped by count (200) and then trimmed by
// size, oldest first, so a long night of routing cannot make the key
// unwritable. Every write carries the version it read as Expect, so two tabs
// of the same user cannot silently overwrite each other; a lost race is
// retried, not reported.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	queen "github.com/smartpricing/queen/clients/client-go"

	"ometto/internal/route"
)

const (
	maxFavourites = 200
	maxHistory    = 200
	maxValueBytes = 56 * 1024       // under the broker's 64 KB KV ceiling
	kvTTL         = 365 * 24 * 3600 // a year: long enough to be "kept", never immortal
)

type store struct {
	kv *queen.KV
	ns string
}

// Fav is one favourite place.
type Fav struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Kind      string  `json:"kind"`
	CreatedAt string  `json:"createdAt"`
}

// HistRequest is the question a history entry remembers.
type HistRequest struct {
	Points       []route.Point `json:"points"`
	Mode         string        `json:"mode"`
	Grade        string        `json:"grade"`
	Alternatives int           `json:"alternatives"`
	// Vias travel inside Points (each carries its own via flag); Avoid and
	// Lifts are the rest of what was asked.
	Avoid []route.Point `json:"avoid,omitempty"`
	Lifts bool          `json:"lifts,omitempty"`
}

// HistSummary is the answer, in six numbers.
type HistSummary struct {
	Seconds  float64 `json:"seconds"`
	Meters   float64 `json:"meters"`
	Ascent   float64 `json:"ascent"`
	Descent  float64 `json:"descent"`
	FromName string  `json:"fromName"`
	ToName   string  `json:"toName"`
}

// HistItem is one line of the history.
type HistItem struct {
	ID      string      `json:"id"`
	At      string      `json:"at"`
	Request HistRequest `json:"request"`
	Summary HistSummary `json:"summary"`
}

// read loads a key into out and reports the version to write back with.
func (s *store) read(ctx context.Context, key string, out interface{}) (int64, error) {
	e, err := s.kv.Get(ctx, s.ns, key)
	if err != nil {
		return 0, err
	}
	if !e.Found || len(e.Value) == 0 || string(e.Value) == "null" {
		return 0, nil
	}
	if err := json.Unmarshal(e.Value, out); err != nil {
		return e.Version, err
	}
	return e.Version, nil
}

// write stores a value, refusing to clobber a version somebody else wrote.
func (s *store) write(ctx context.Context, key string, version int64, value interface{}) (bool, error) {
	opts := queen.KVWriteOptions{Expect: queen.Expect(version)}
	w, err := s.kv.Put(ctx, s.ns, key, value, queen.TTLSeconds(kvTTL), opts)
	if err != nil {
		return false, err
	}
	return w.Applied, nil
}

// update is the read-modify-write loop both lists share.
func (s *store) update(ctx context.Context, key string, fn func(raw json.RawMessage) (interface{}, error)) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		e, err := s.kv.Get(ctx, s.ns, key)
		if err != nil {
			return err
		}
		var cur json.RawMessage
		if e.Found && len(e.Value) > 0 && string(e.Value) != "null" {
			cur = e.Value
		}
		next, err := fn(cur)
		if err != nil {
			return err
		}
		ok, err := s.write(ctx, key, e.Version, next)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		lastErr = fmt.Errorf("kv %s: lost the write race", key)
		time.Sleep(time.Duration(20*(attempt+1)) * time.Millisecond)
	}
	return lastErr
}

// trim caps a list by count and then by encoded size, dropping from the end
// (the oldest, since both lists are newest first).
func trim[T any](items []T, max int) ([]T, error) {
	if len(items) > max {
		items = items[:max]
	}
	for {
		raw, err := json.Marshal(items)
		if err != nil {
			return nil, err
		}
		if len(raw) <= maxValueBytes || len(items) <= 1 {
			return items, nil
		}
		items = items[:len(items)-1]
	}
}

// ------------------------------------------------------------- favourites

func (s *store) Favorites(ctx context.Context, user string) ([]Fav, error) {
	out := []Fav{}
	if _, err := s.read(ctx, "fav:"+user, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *store) AddFavorite(ctx context.Context, user string, f Fav) (Fav, error) {
	f.ID = newID()
	f.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	err := s.update(ctx, "fav:"+user, func(raw json.RawMessage) (interface{}, error) {
		list := []Fav{}
		if raw != nil {
			if err := json.Unmarshal(raw, &list); err != nil {
				return nil, err
			}
		}
		list = append([]Fav{f}, list...)
		return trim(list, maxFavourites)
	})
	return f, err
}

func (s *store) DeleteFavorite(ctx context.Context, user, id string) error {
	return s.update(ctx, "fav:"+user, func(raw json.RawMessage) (interface{}, error) {
		list := []Fav{}
		if raw != nil {
			if err := json.Unmarshal(raw, &list); err != nil {
				return nil, err
			}
		}
		out := list[:0]
		for _, f := range list {
			if f.ID != id {
				out = append(out, f)
			}
		}
		return out, nil
	})
}

// ---------------------------------------------------------------- history

func (s *store) History(ctx context.Context, user string, limit int) ([]HistItem, error) {
	out := []HistItem{}
	if _, err := s.read(ctx, "hist:"+user, &out); err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *store) AddHistory(ctx context.Context, user string, it HistItem) error {
	return s.update(ctx, "hist:"+user, func(raw json.RawMessage) (interface{}, error) {
		list := []HistItem{}
		if raw != nil {
			if err := json.Unmarshal(raw, &list); err != nil {
				return nil, err
			}
		}
		list = foldHistory(list, it, historyFold)
		return trim(list, maxHistory)
	})
}

// historyFold is how long the same question keeps its place in the list.
const historyFold = 10 * time.Minute

// foldHistory puts a request at the top of the list, unless it IS the top:
// the same points, mode and grade asked again within ten minutes updates the
// entry that is there instead of adding a second line. Dragging a marker,
// switching to alternatives and back, or a page that retries on focus should
// not fill a person's history with the same trip five times.
func foldHistory(list []HistItem, it HistItem, within time.Duration) []HistItem {
	if len(list) > 0 && sameQuestion(list[0].Request, it.Request) {
		if prev, err := time.Parse(time.RFC3339, list[0].At); err == nil {
			now, err2 := time.Parse(time.RFC3339, it.At)
			if err2 != nil {
				now = time.Now().UTC()
			}
			if d := now.Sub(prev); d >= 0 && d <= within {
				list[0].At = it.At
				list[0].Summary = it.Summary
				list[0].Request.Alternatives = it.Request.Alternatives
				return list
			}
		}
	}
	return append([]HistItem{it}, list...)
}

// sameQuestion compares what was asked, not what came back: the same points
// to five decimals (about a metre), the same mode, the same grade.
func sameQuestion(a, b HistRequest) bool {
	if a.Mode != b.Mode || a.Grade != b.Grade || a.Lifts != b.Lifts ||
		len(a.Points) != len(b.Points) || len(a.Avoid) != len(b.Avoid) {
		return false
	}
	for i := range a.Points {
		if a.Points[i].Via != b.Points[i].Via ||
			math.Abs(a.Points[i].Lat-b.Points[i].Lat) > 1e-5 ||
			math.Abs(a.Points[i].Lon-b.Points[i].Lon) > 1e-5 {
			return false
		}
	}
	for i := range a.Avoid {
		if math.Abs(a.Avoid[i].Lat-b.Avoid[i].Lat) > 1e-5 ||
			math.Abs(a.Avoid[i].Lon-b.Avoid[i].Lon) > 1e-5 {
			return false
		}
	}
	return true
}

func (s *store) DeleteHistory(ctx context.Context, user, id string) error {
	return s.update(ctx, "hist:"+user, func(raw json.RawMessage) (interface{}, error) {
		list := []HistItem{}
		if raw != nil {
			if err := json.Unmarshal(raw, &list); err != nil {
				return nil, err
			}
		}
		out := list[:0]
		for _, h := range list {
			if h.ID != id {
				out = append(out, h)
			}
		}
		return out, nil
	})
}

func (s *store) ClearHistory(ctx context.Context, user string) error {
	_, err := s.kv.Delete(ctx, s.ns, "hist:"+user)
	return err
}
