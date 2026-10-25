package ingest

import (
	"context"
	"errors"

	"github.com/prayujt/wavelength/internal/discovery"
)

// Lookup identifies imports by provenance. Similar titles or other editions are
// deliberately left unmatched, and no external provider or source file is read.
func (s *Service) Lookup(ctx context.Context, refs []discovery.Ref) (map[discovery.Ref]string, error) {
	if len(refs) > MaxItems {
		return nil, ErrInvalid
	}
	for _, ref := range refs {
		if !ref.ValidMetadata() {
			return nil, ErrInvalid
		}
	}
	return s.deps.Store.LookupMany(ctx, s.root.ID, refs)
}

func (s *Service) Origins(ctx context.Context, ids []string) (map[discovery.Ref]string, error) {
	if len(ids) > MaxItems {
		return nil, ErrInvalid
	}
	found := make(map[discovery.Ref]string)
	for _, id := range ids {
		if id == "" || len(id) > 128 {
			return nil, ErrInvalid
		}
		ref, err := s.deps.Store.Origin(ctx, s.root.ID, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		found[ref] = id
	}
	return found, nil
}
