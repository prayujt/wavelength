package sharing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/state"
)

type tracks struct{}

func (tracks) Get(_ context.Context, id string) (library.Track, error) {
	return library.Track{ID: id}, nil
}

func TestScopeExpirationOwnershipAndRevocation(t *testing.T) {
	ctx := context.Background()
	service := sharing.New(&state.Memory{}, tracks{})
	share, err := service.Create(ctx, "alice", "Selected track", 0, []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Authorize(ctx, share.ID, "two"); !errors.Is(err, sharing.ErrForbidden) {
		t.Fatalf("scope escape: %v", err)
	}
	if err := service.Update(ctx, "bob", share.ID, false, sharing.Changes{}); !errors.Is(err, sharing.ErrForbidden) {
		t.Fatalf("foreign mutation: %v", err)
	}
	if err := service.Visit(ctx, share.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := service.Resolve(ctx, share.ID)
	if err != nil || stored.VisitCount != 1 {
		t.Fatalf("visit not recorded: %+v, %v", stored, err)
	}
	past := time.Now().Add(-time.Second).UnixMilli()
	if err := service.Update(ctx, "alice", share.ID, false, sharing.Changes{Expires: &past}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(ctx, share.ID); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("expired link accepted: %v", err)
	}
	if err := service.Delete(ctx, "alice", share.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(ctx, share.ID); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("revoked link accepted: %v", err)
	}
}
