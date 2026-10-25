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

func TestIndividualTracksUseCollectionArtist(t *testing.T) {
	var collections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var results []map[string]any
		switch r.URL.Query().Get("id") {
		case "101", "102":
			artist, title := "Enya", "May It Be"
			id := int64(101)
			if r.URL.Query().Get("id") == "102" {
				artist, title, id = "Howard Shore", "The Prophecy", 102
			}
			results = []map[string]any{{"wrapperType": "track", "trackId": id, "collectionId": 500, "artistName": artist, "collectionName": "Fellowship soundtrack", "trackName": title, "trackTimeMillis": 240000}}
		case "500":
			collections.Add(1)
			if r.URL.Query().Get("entity") != "album" {
				t.Error("expected a collection lookup")
			}
			results = []map[string]any{{"wrapperType": "collection", "collectionId": 500, "artistName": "Howard Shore", "collectionName": "Fellowship soundtrack"}}
		default:
			t.Errorf("unexpected lookup: %s", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer server.Close()
	c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	for _, id := range []string{"101", "102", "101"} {
		got, err := c.Track(context.Background(), discovery.Ref{Provider: "itunes", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if got.AlbumArtist != "Howard Shore" {
			t.Fatalf("track %s would split the soundtrack: %+v", id, got)
		}
		if id == "101" && (got.Artist != "Enya" || got.Title != "May It Be") {
			t.Fatalf("guest performer or title changed: %+v", got)
		}
	}
	if got := collections.Load(); got != 1 {
		t.Fatalf("selected songs repeated the same collection lookup %d times", got)
	}
	// Do not retain provider credits indefinitely.
	c.mu.Lock()
	c.artists[500] = cachedAlbumArtist{name: "Old credit", expires: time.Now().Add(-time.Minute)}
	c.mu.Unlock()
	got, err := c.Track(context.Background(), discovery.Ref{Provider: "itunes", ID: "101"})
	if err != nil || got.AlbumArtist != "Howard Shore" || collections.Load() != 2 {
		t.Fatalf("expired collection credit was not refreshed: %+v, %v", got, err)
	}
}

func TestIndividualTrackRequiresExactCollectionCredit(t *testing.T) {
	for _, scenario := range []string{"no collection ID", "missing collection", "wrong collection", "empty credit"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				result := []entry{}
				if r.URL.Query().Get("id") == "101" {
					albumID := int64(500)
					if scenario == "no collection ID" {
						albumID = 0
					}
					result = []entry{{Kind: "track", TrackID: 101, AlbumID: albumID, Title: "May It Be", Artist: "Enya", Millis: 240000}}
				} else if scenario == "wrong collection" {
					result = []entry{{Kind: "collection", AlbumID: 999, Album: "Same title, different release", Artist: "Another artist"}}
				} else if scenario == "empty credit" {
					result = []entry{{Kind: "collection", AlbumID: 500, Artist: "  "}}
				}
				json.NewEncoder(w).Encode(map[string]any{"results": result})
			}))
			defer server.Close()
			c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
			if _, err := c.Track(context.Background(), discovery.Ref{Provider: "itunes", ID: "101"}); !errors.Is(err, discovery.ErrUnavailable) {
				t.Fatalf("unresolved credit should prevent incorrect tagging: %v", err)
			}
		})
	}
}

func TestCollectionLookupFailureCanBeRetried(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "101" {
			json.NewEncoder(w).Encode(map[string]any{"results": []entry{{Kind: "track", TrackID: 101, AlbumID: 500, Artist: "Enya", Title: "May It Be"}}})
			return
		}
		if attempts.Add(1) == 1 {
			w.Write([]byte(`{"results":`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []entry{{Kind: "collection", AlbumID: 500, Artist: "Howard Shore"}}})
	}))
	defer server.Close()
	c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	ref := discovery.Ref{Provider: "itunes", ID: "101"}
	if _, err := c.Track(context.Background(), ref); !errors.Is(err, discovery.ErrUnavailable) {
		t.Fatal(err)
	}
	got, err := c.Track(context.Background(), ref)
	if err != nil || got.AlbumArtist != "Howard Shore" {
		t.Fatalf("provider recovery did not resolve the album credit: %+v, %v", got, err)
	}
}

func TestConcurrentTrackLookupsPreservePerformer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := entry{Kind: "collection", AlbumID: 500, Artist: "Howard Shore"}
		if r.URL.Query().Get("id") == "101" {
			result = entry{Kind: "track", TrackID: 101, AlbumID: 500, Artist: "Enya", Title: "May It Be"}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []entry{result}})
	}))
	defer server.Close()
	c := &Client{http: server.Client(), base: server.URL, limiter: rate.NewLimiter(rate.Inf, 1)}
	results := make(chan error, 8)
	for range 8 {
		go func() {
			got, err := c.Track(context.Background(), discovery.Ref{Provider: "itunes", ID: "101"})
			if err == nil && (got.Artist != "Enya" || got.AlbumArtist != "Howard Shore") {
				err = errors.New("album or track artist changed during concurrent resolution")
			}
			results <- err
		}()
	}
	for range 8 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
