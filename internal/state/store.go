// Package state provides transactional persistence for user-owned application records.
package state

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

var ErrNotFound = errors.New("record not found")

type Store interface {
	Get(context.Context, string, string) (json.RawMessage, error)
	List(context.Context, string) (map[string]json.RawMessage, error)
	Update(context.Context, string, string, func(json.RawMessage) (json.RawMessage, error)) error
}

// Memory is intended for embedded/test use. The executable uses SQLite.
type Memory struct {
	mu     sync.Mutex
	values map[string]map[string]json.RawMessage
}

func (m *Memory) Get(ctx context.Context, scope, key string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[scope][key]
	if !ok {
		return nil, ErrNotFound
	}
	return append(json.RawMessage(nil), v...), nil
}
func (m *Memory) List(ctx context.Context, scope string) (map[string]json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]json.RawMessage{}
	for k, v := range m.values[scope] {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out, nil
}
func (m *Memory) Update(ctx context.Context, scope, key string, update func(json.RawMessage) (json.RawMessage, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string]map[string]json.RawMessage{}
	}
	if m.values[scope] == nil {
		m.values[scope] = map[string]json.RawMessage{}
	}
	v, err := update(append(json.RawMessage(nil), m.values[scope][key]...))
	if err != nil {
		return err
	}
	if v == nil {
		delete(m.values[scope], key)
	} else {
		m.values[scope][key] = append(json.RawMessage(nil), v...)
	}
	return nil
}
