package ingest_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
)

func TestLookupUsesOnlyPublishedProvenance(t *testing.T) {
	f := setup(t, writeAudio, metadata{}, nil)
	ctx := context.Background()
	if found, err := f.s.Lookup(ctx, []discovery.Ref{metadataRef}); err != nil || len(found) != 0 {
		t.Fatal(found, err)
	}
	job := create(t, f.s, "alice", request("lookup"))
	done := wait(t, f.s, "alice", job.ID, ingest.Completed)
	// A lookup reads catalog provenance, even if the source is temporarily unavailable.
	track, err := f.lib.Get(ctx, done.Items[0].TrackID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.root.Path + "/" + track.Path); err != nil {
		t.Fatal(err)
	}
	found, err := f.s.Lookup(ctx, []discovery.Ref{metadataRef, metadataRef, {Provider: "itunes", ID: "456"}})
	if err != nil || len(found) != 1 || found[metadataRef] != done.Items[0].TrackID {
		t.Fatal(found, err)
	}
	for _, refs := range [][]discovery.Ref{{{Provider: "youtube", ID: "BaW_jenozKc"}}, make([]discovery.Ref, 201)} {
		if _, err := f.s.Lookup(ctx, refs); !errors.Is(err, ingest.ErrInvalid) {
			t.Fatal("invalid lookup accepted", err)
		}
	}
}
