package userdata_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/userdata"
)

type tracks struct{}

func (tracks) Get(_ context.Context, id string) (library.Track, error) {
	if id != "track" {
		return library.Track{}, library.ErrNotFound
	}
	return library.Track{ID: id}, nil
}

func TestPlaylistOwnershipAndTransactionalChanges(t *testing.T) {
	ctx := context.Background()
	service := userdata.New(&state.Memory{}, tracks{})
	p, err := service.CreatePlaylist(ctx, "alice", "Private", []string{"track", "track"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Playlist(ctx, "bob", p.ID); !errors.Is(err, userdata.ErrForbidden) {
		t.Fatalf("private playlist: %v", err)
	}
	if err := service.DeletePlaylist(ctx, "bob", p.ID); !errors.Is(err, userdata.ErrForbidden) {
		t.Fatalf("foreign deletion: %v", err)
	}
	name := "Should roll back"
	if _, err := service.UpdatePlaylist(ctx, "alice", p.ID, userdata.PlaylistChanges{Name: &name, Remove: []int{99}}); !errors.Is(err, userdata.ErrInvalid) {
		t.Fatalf("invalid mutation: %v", err)
	}
	stored, err := service.Playlist(ctx, "alice", p.ID)
	if err != nil || stored.Name != p.Name || len(stored.IDs) != 2 {
		t.Fatalf("partial update: %+v, %v", stored, err)
	}
	public := true
	if _, err := service.UpdatePlaylist(ctx, "alice", p.ID, userdata.PlaylistChanges{Public: &public, Remove: []int{0}}); err != nil {
		t.Fatal(err)
	}
	stored, err = service.Playlist(ctx, "bob", p.ID)
	if err != nil || len(stored.IDs) != 1 {
		t.Fatalf("public playlist: %+v, %v", stored, err)
	}
}

func TestListeningStateAndInvalidQueue(t *testing.T) {
	ctx := context.Background()
	service := userdata.New(&state.Memory{}, tracks{})
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := service.RecordPlay(ctx, "alice", "client", "track", time.Now(), true); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	annotations, err := service.Annotations(ctx, "alice")
	if err != nil || annotations["track"].Plays != 20 {
		t.Fatalf("lost plays: %+v, %v", annotations, err)
	}
	queue := userdata.Queue{IDs: []string{"track"}, Current: "track", Position: 100}
	if err := service.SaveQueue(ctx, "alice", queue); err != nil {
		t.Fatal(err)
	}
	queue.Current = "missing"
	if err := service.SaveQueue(ctx, "alice", queue); !errors.Is(err, userdata.ErrInvalid) {
		t.Fatalf("invalid queue: %v", err)
	}
	stored, err := service.Queue(ctx, "alice")
	if err != nil || stored.Current != "track" || stored.Position != 100 {
		t.Fatalf("queue overwritten: %+v, %v", stored, err)
	}
	if err := service.SaveBookmark(ctx, "alice", userdata.Bookmark{ID: "track", Position: 10}); err != nil {
		t.Fatal(err)
	}
	before, err := service.Bookmarks(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveBookmark(ctx, "alice", userdata.Bookmark{ID: "track", Position: 20}); err != nil {
		t.Fatal(err)
	}
	after, err := service.Bookmarks(ctx, "alice")
	if err != nil || after[0].Created != before[0].Created || after[0].Position != 20 {
		t.Fatalf("bookmark update: %+v, %v", after, err)
	}
}
