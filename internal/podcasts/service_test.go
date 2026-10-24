package podcasts_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/podcasts"
	"github.com/prayujt/wavelength/internal/state"
)

func TestDeletionCannotResurrectAnInflightDownload(t *testing.T) {
	for _, channelDeletion := range []bool{false, true} {
		t.Run(fmt.Sprint(channelDeletion), func(t *testing.T) {
			ctx := context.Background()
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/feed" {
					fmt.Fprint(w, `<rss><channel><title>Feed</title><item><guid>one</guid><title>Episode</title><enclosure url="/media" type="audio/mpeg"/></item></channel></rss>`)
					return
				}
				requests.Add(1)
				once.Do(func() { close(started) })
				<-release
				fmt.Fprint(w, "episode bytes")
			}))
			defer server.Close()
			directory := t.TempDir()
			service, err := podcasts.New(ctx, &state.Memory{}, directory)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			var unblock sync.Once
			defer unblock.Do(func() { close(release) })
			if err := service.Subscribe(ctx, server.URL+"/feed"); err != nil {
				t.Fatal(err)
			}
			channels, err := service.Channels(ctx)
			if err != nil {
				t.Fatal(err)
			}
			episode := channels[0].Episodes[0]
			if err := service.Download(ctx, episode.ID); err != nil {
				t.Fatal(err)
			}
			if err := service.Download(ctx, episode.ID); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("download never started")
			}
			if channelDeletion {
				err = service.DeleteChannel(ctx, channels[0].ID)
			} else {
				err = service.DeleteEpisode(ctx, episode.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			unblock.Do(func() { close(release) })
			service.Close()
			_, current, err := service.Episode(ctx, episode.ID)
			if channelDeletion {
				if !errors.Is(err, state.ErrNotFound) {
					t.Fatalf("channel resurrected: %v", err)
				}
			} else if err != nil || current.Status != "deleted" {
				t.Fatalf("episode resurrected: %+v, %v", current, err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatalf("download file survived deletion: %v, %v", entries, err)
			}
			if requests.Load() != 1 {
				t.Fatalf("duplicate download: %d", requests.Load())
			}
		})
	}
}
