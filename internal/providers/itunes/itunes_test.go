package itunes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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
			result = []entry{{Kind: "track", TrackID: 123, ArtistID: 789, AlbumID: 456, Artist: "Artist", Title: "Song", Millis: 180000, Artwork: "https://example.test/100x100bb.jpg"}}
		case r.URL.Path == "/search" && q.Get("entity") == "album":
			result = []entry{{Kind: "collection", AlbumID: 456, Album: "Album", Artist: "Album artist"}}
		case r.URL.Path == "/search":
			result = []entry{{Kind: "artist", ArtistID: 789, Artist: "Artist"}, {Kind: "artist", ArtistID: 790, Artist: "Artist"}}
		case q.Get("id") == "456":
			result = []entry{{Kind: "collection", AlbumID: 456, ArtistID: 789, Album: "Album", Artist: "Various Artists"}, {Kind: "track", AlbumID: 456, TrackID: 123, Title: "Song", Artist: "Track artist", Millis: 180000, Number: 2, Disc: 1, Genre: "Country"}}
		case q.Get("id") == "789":
			result = []entry{{Kind: "artist", ArtistID: 789, Artist: "Artist"}, {Kind: "collection", AlbumID: 456, Album: "Album", Artist: "Artist"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": result})
	}))
	defer server.Close()
	c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	results, err := c.Search(context.Background(), "Artist", 5)
	if err != nil || len(results.Tracks) != 1 || len(results.Releases) != 1 || len(results.Artists) != 2 {
		t.Fatal(results, err)
	}
	if results.Tracks[0].ArtistRef != ref(789) || results.Tracks[0].ReleaseRef != ref(456) {
		t.Fatal("song navigation lost provider identities", results.Tracks[0])
	}
	if results.Artists[0].ArtworkURL != "https://example.test/600x600bb.jpg" || results.Artists[1].ArtworkURL != "" {
		t.Fatal("artist artwork must match provider identity", results.Artists)
	}
	release, err := c.Release(context.Background(), discovery.Ref{Provider: "itunes", ID: "456"})
	if err != nil || len(release.Tracks) != 1 || release.Tracks[0].AlbumArtist != "Various Artists" || release.Tracks[0].Artist != "Track artist" || release.Tracks[0].Duration != 180 || release.ArtistRef != ref(789) {
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

func TestBatchReleasesKeepExactMembershipAndRequestOrder(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("id") != "456,789,999,1000" || r.URL.Query().Get("entity") != "song" || r.URL.Query().Get("limit") != "200" {
			t.Error(r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []entry{
			{Kind: "collection", AlbumID: 789, Album: "Same title", Artist: "Second", Count: 2},
			{Kind: "collection", AlbumID: 456, Album: "Same title", Artist: "First", Count: 2},
			{Kind: "collection", AlbumID: 1000, Album: "Too large", Count: 201},
			{Kind: "track", AlbumID: 456, TrackID: 2, Title: "B", Artist: "Guest", Number: 2, Millis: 180000},
			{Kind: "track", AlbumID: 789, TrackID: 3, Title: "C", Artist: "Second", Number: 1, Millis: 180000},
			{Kind: "track", AlbumID: 456, TrackID: 1, Title: "A", Artist: "First", Number: 1, Millis: 180000},
			{Kind: "track", AlbumID: 1000, TrackID: 4, Title: "D", Artist: "Third", Number: 1, Millis: 180000},
		}})
	}))
	defer server.Close()
	c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	albums, err := c.Releases(context.Background(), []discovery.Ref{ref(456), ref(789), ref(999), ref(1000)})
	if err != nil || calls != 1 || len(albums) != 2 {
		t.Fatal(albums, calls, err)
	}
	if albums[0].Ref != ref(456) || len(albums[0].Tracks) != 2 || albums[0].Tracks[0].Ref != ref(1) || albums[0].Tracks[1].AlbumArtist != "First" || albums[1].Ref != ref(789) || albums[1].Tracks[0].Ref != ref(3) {
		t.Fatal(albums)
	}
}

func TestSearchRequestsIndependentEntitiesTogether(t *testing.T) {
	var started atomic.Int32
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if started.Add(1) == 3 {
			close(ready)
		}
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []entry{}})
	}))
	defer server.Close()
	client := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Search(ctx, "Artist", 5); err != nil || started.Load() != 3 {
		t.Fatal("search entities did not run together", err, started.Load())
	}
}
