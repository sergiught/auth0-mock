package mgmtstore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

// Object is a JSON object as decoded from a request body.
type Object = map[string]any

// MaxObjects is the most objects one collection holds.
const MaxObjects = 10000

const timeFormat = "2006-01-02T15:04:05.000Z"

var (
	errNotFound = errors.New("mgmtstore: not found")
	errConflict = errors.New("mgmtstore: already exists")
	errLimit    = errors.New("mgmtstore: collection is full")
)

// stamp says which timestamp fields a write sets.
type stamp struct{ created, updated bool }

type collection struct {
	order []string
	objs  map[string]Object
}

// Store is an in-memory, concurrency-safe object store.
type Store struct {
	mu       sync.Mutex
	engine   *Engine
	now      func() time.Time
	max      int
	colls    map[string]*collection
	settings map[string]Object
}

// NewStore returns an empty store that stamps times from now.
func (e *Engine) NewStore(now func() time.Time) *Store {
	return &Store{
		engine:   e,
		now:      now,
		max:      MaxObjects,
		colls:    map[string]*collection{},
		settings: map[string]Object{},
	}
}

// Clear drops everything the store holds.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.colls = map[string]*collection{}
	s.settings = map[string]Object{}
}

func (s *Store) stamp() string { return s.now().UTC().Format(timeFormat) }

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, x := range t {
			m[k] = deepCopy(x)
		}
		return m
	case []any:
		l := make([]any, len(t))
		for i, x := range t {
			l[i] = deepCopy(x)
		}
		return l
	default:
		return v
	}
}

func copyObject(o Object) Object {
	if o == nil {
		return nil
	}
	return deepCopy(o).(Object)
}

func (s *Store) create(coll, idField string, obj Object, st stamp) (Object, error) {
	return s.insert(coll, idField, obj, st, false)
}

// insert adds obj to coll. With keepStamps, a created_at or updated_at the
// object already carries is kept instead of overwritten.
func (s *Store) insert(coll, idField string, obj Object, st stamp, keepStamps bool) (Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.colls[coll]
	if c == nil {
		c = &collection{objs: map[string]Object{}}
		s.colls[coll] = c
	}
	if len(c.order) >= s.max {
		return nil, errLimit
	}
	o := copyObject(obj)
	if o == nil {
		o = Object{}
	}
	id, _ := o[idField].(string)
	if id == "" {
		id = newID()
		o[idField] = id
	}
	if _, dup := c.objs[id]; dup {
		return nil, errConflict
	}
	if _, has := o["created_at"]; st.created && (!keepStamps || !has) {
		o["created_at"] = s.stamp()
	}
	if _, has := o["updated_at"]; st.updated && (!keepStamps || !has) {
		o["updated_at"] = s.stamp()
	}
	c.objs[id] = o
	c.order = append(c.order, id)
	return copyObject(o), nil
}

func (s *Store) get(coll, id string) (Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.colls[coll]
	if c == nil {
		return nil, false
	}
	o, ok := c.objs[id]
	if !ok {
		return nil, false
	}
	return copyObject(o), true
}

func (s *Store) list(coll string) []Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.colls[coll]
	if c == nil {
		return []Object{}
	}
	out := make([]Object, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, copyObject(c.objs[id]))
	}
	return out
}

func (s *Store) merge(coll, id, idField string, patch Object, st stamp) (Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.colls[coll]
	if c == nil || c.objs[id] == nil {
		return nil, errNotFound
	}
	o := c.objs[id]
	for k, v := range patch {
		if k == idField || k == "created_at" {
			continue
		}
		if v == nil {
			delete(o, k)
			continue
		}
		o[k] = deepCopy(v)
	}
	if st.updated {
		o["updated_at"] = s.stamp()
	}
	return copyObject(o), nil
}

func (s *Store) replace(coll, id, idField string, obj Object, st stamp) (Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.colls[coll]
	if c == nil || c.objs[id] == nil {
		return nil, errNotFound
	}
	old := c.objs[id]
	o := copyObject(obj)
	if o == nil {
		o = Object{}
	}
	o[idField] = old[idField]
	delete(o, "created_at")
	if v, ok := old["created_at"]; ok {
		o["created_at"] = v
	}
	if st.updated {
		o["updated_at"] = s.stamp()
	}
	c.objs[id] = o
	return copyObject(o), nil
}

func (s *Store) remove(coll, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.colls[coll]
	if c == nil {
		return false
	}
	if _, ok := c.objs[id]; !ok {
		return false
	}
	delete(c.objs, id)
	for i, x := range c.order {
		if x == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	prefix := coll + "/" + id + "/"
	for k := range s.colls {
		if strings.HasPrefix(k, prefix) {
			delete(s.colls, k)
		}
	}
	for k := range s.settings {
		if strings.HasPrefix(k, prefix) {
			delete(s.settings, k)
		}
	}
	return true
}

func (s *Store) getSettings(path string, initial Object) Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.settings[path]; ok {
		return copyObject(o)
	}
	return copyObject(initial)
}

func (s *Store) mergeSettings(path string, initial, patch Object) Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.settings[path]
	if !ok {
		cur = copyObject(initial)
		if cur == nil {
			cur = Object{}
		}
	}
	for k, v := range patch {
		if v == nil {
			delete(cur, k)
			continue
		}
		cur[k] = deepCopy(v)
	}
	s.settings[path] = cur
	return copyObject(cur)
}

func (s *Store) replaceSettings(path string, obj Object) Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := copyObject(obj)
	if o == nil {
		o = Object{}
	}
	s.settings[path] = o
	return copyObject(o)
}

func (s *Store) resetSettings(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.settings, path)
}
