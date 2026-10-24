package state

import (
	"context"
	"encoding/json"
)

// Read and Write keep serialization in the persistence boundary, not in transports.
func Read[T any](ctx context.Context, store Store, scope, key string) (T, error) {
	var value T
	raw, err := store.Get(ctx, scope, key)
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(raw, &value)
	return value, err
}

func ReadAll[T any](ctx context.Context, store Store, scope string) (map[string]T, error) {
	records, err := store.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	values := make(map[string]T, len(records))
	for key, raw := range records {
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func Write(ctx context.Context, store Store, scope, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return store.Update(ctx, scope, key, func(json.RawMessage) (json.RawMessage, error) { return raw, nil })
}

func Delete(ctx context.Context, store Store, scope, key string) error {
	return store.Update(ctx, scope, key, func(json.RawMessage) (json.RawMessage, error) { return nil, nil })
}

// Modify runs the read, mutation and write under the store's transaction.
func Modify[T any](ctx context.Context, store Store, scope, key string, change func(*T, bool) error) error {
	return store.Update(ctx, scope, key, func(raw json.RawMessage) (json.RawMessage, error) {
		var value T
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
		}
		if err := change(&value, len(raw) > 0); err != nil {
			return nil, err
		}
		return json.Marshal(value)
	})
}
