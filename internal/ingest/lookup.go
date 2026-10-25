package ingest

import (
	"context"
	"errors"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/library"
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
	found := make(map[discovery.Ref]string)
	seen := make(map[discovery.Ref]bool)
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		track, err := s.deps.Store.Lookup(ctx, s.root.ID, ref)
		if errors.Is(err, ErrNotFound) || errors.Is(err, library.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		found[ref] = track.ID
	}
	return found, nil
}
