package itunes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prayujt/wavelength/internal/discovery"
	"golang.org/x/time/rate"
)

func TestSearchAndReleaseUseProviderIDsAndAlbumArtist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("entity") == "" {
			t.Error("missing entity")
		}
		result := []entry{}
		switch {
		case r.URL.Path == "/search" && q.Get("entity") == "song":
			result = []entry{{Kind: "track", TrackID: 123, Artist: "Artist", Title: "Song", Millis: 180000}}
		case r.URL.Path == "/search" && q.Get("entity") == "album":
			result = []entry{{Kind: "collection", AlbumID: 456, Album: "Album", Artist: "Album artist"}}
		case r.URL.Path == "/search":
			result = []entry{{Kind: "artist", ArtistID: 789, Artist: "Artist"}}
		case q.Get("id") == "456":
			result = []entry{{Kind: "collection", AlbumID: 456, Album: "Album", Artist: "Various Artists"}, {Kind: "track", AlbumID: 456, TrackID: 123, Title: "Song", Artist: "Track artist", Millis: 180000, Number: 2, Disc: 1, Genre: "Country"}}
		case q.Get("id") == "789":
			result = []entry{{Kind: "artist", ArtistID: 789, Artist: "Artist"}, {Kind: "collection", AlbumID: 456, Album: "Album", Artist: "Artist"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": result})
	}))
	defer server.Close()
	c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	results, err := c.Search(context.Background(), "Artist", 5)
	if err != nil || len(results.Tracks) != 1 || len(results.Releases) != 1 || len(results.Artists) != 1 {
		t.Fatal(results, err)
	}
	release, err := c.Release(context.Background(), discovery.Ref{Provider: "itunes", ID: "456"})
	if err != nil || len(release.Tracks) != 1 || release.Tracks[0].AlbumArtist != "Various Artists" || release.Tracks[0].Artist != "Track artist" || release.Tracks[0].Duration != 180 {
		t.Fatal(release, err)
	}
	artist, err := c.Artist(context.Background(), discovery.Ref{Provider: "itunes", ID: "789"})
	if err != nil || artist.Name != "Artist" || len(artist.Releases) != 1 {
		t.Fatal(artist, err)
	}
	_, err = c.Track(context.Background(), discovery.Ref{Provider: "itunes", ID: "999"})
	if !errors.Is(err, discovery.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestMalformedProviderResponsesAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"results":`)) }))
	defer server.Close()
	c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	if _, err := c.Search(context.Background(), "Artist", 1); !errors.Is(err, discovery.ErrUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Track(ctx, discovery.Ref{Provider: "itunes", ID: "123"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
