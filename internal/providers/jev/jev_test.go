package jev

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
)

func fixtures() (discovery.Track, []discovery.Candidate) {
	track := discovery.Track{Ref: discovery.Ref{Provider: "itunes", ID: "123"}, Title: "Dandelion Wine", Artist: "Gregory Alan Isakov", Album: "This Empty Northern Hemisphere", AlbumArtist: "Gregory Alan Isakov", Duration: 184.907, Genre: "Singer/Songwriter", ReleaseDate: "2009-05-18", TrackNumber: 1, DiscNumber: 1, ArtworkURL: "https://example.com/art.jpg"}
	candidates := []discovery.Candidate{
		{Ref: discovery.Ref{Provider: "youtube", ID: "we25uACwRkQ"}, Title: "Gregory Alan Isakov - Dandelion Wine - Lyrics", Uploader: "sayhellotonever", Duration: 185, Score: .999, Suitable: true, Reason: "title, artist and duration match", Evidence: json.RawMessage(`{"id":"we25uACwRkQ","title":"Gregory Alan Isakov - Dandelion Wine - Lyrics","channel_id":"fan-channel","description":"Fan lyrics","view_count":1234,"thumbnails":[{"url":"https://example.com/thumb.jpg"}]}`)},
		{Ref: discovery.Ref{Provider: "youtube", ID: "QbR4aNW0nCk"}, Title: "Dandelion Wine", Uploader: "Gregory Alan Isakov", Duration: 185, Score: .999, Suitable: true, Reason: "title, artist and duration match", Evidence: json.RawMessage(`{"id":"QbR4aNW0nCk","channel":"Gregory Alan Isakov","channel_id":"artist-channel","channel_is_verified":true,"artist":"Gregory Alan Isakov","album":"This Empty Northern Hemisphere","description":"Provided to YouTube by a label","extra_provider_field":{"preserve":true}}`)},
	}
	return track, candidates
}

func rankedResponse() string {
	return `{"model":"jev-1.14.0","answers":{"match_0":{"type":"noul","noul":0.99},"source_0":{"type":"score","score":2},"match_1":{"type":"noul","noul":0.99},"source_1":{"type":"score","score":4}}}`
}

func TestRankingUsesCompleteMetadataAndPrefersArtistSource(t *testing.T) {
	track, candidates := fixtures()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer test-api-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect authenticated API request")
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "test-api-key") {
			t.Error("key included in model input")
		}
		var request struct {
			Model string `json:"model"`
			State struct {
				Track   discovery.Track `json:"requested_track"`
				Options []option        `json:"candidates"`
			} `json:"state"`
			Questions map[string]question `json:"questions"`
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Error(err)
			return
		}
		if request.Model != "jev-1.14.0" || request.State.Track != track || len(request.State.Options) != 2 || len(request.Questions) != 4 {
			t.Error("incomplete track or question metadata")
		}
		for i, candidate := range candidates {
			got := request.State.Options[i]
			if got.Ref != candidate.Ref || got.Title != candidate.Title || got.Uploader != candidate.Uploader || got.Duration != candidate.Duration || got.URL != "https://www.youtube.com/watch?v="+candidate.Ref.ID || got.Index != i {
				t.Error("missing candidate details or URL")
			}
			var actual, expected any
			_ = json.Unmarshal(got.Metadata, &actual)
			_ = json.Unmarshal(candidate.Evidence, &expected)
			a, _ := json.Marshal(actual)
			e, _ := json.Marshal(expected)
			if string(a) != string(e) {
				t.Error("provider metadata was lost")
			}
		}
		_, _ = io.WriteString(w, rankedResponse())
	}))
	defer server.Close()
	c := New("test-api-key", "jev-1.14.0")
	c.endpoint = server.URL + "/v1/systemone"
	ranked, err := c.Rank(context.Background(), track, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if ranked[0].Ref != candidates[1].Ref || ranked[0].Score < .99 || ranked[1].Score > .90 || !ranked[0].Suitable {
		t.Fatal("artist source was not preferred", ranked)
	}
	service := discovery.New(nil, nil, discovery.WithRanking(c, Policy()))
	if ref, ok := service.Select(ranked); !ok || ref != candidates[1].Ref {
		t.Fatal("strong artist result should be selected", ref, ok)
	}
	if candidates[0].Score != .999 || candidates[0].Ref.ID != "we25uACwRkQ" {
		t.Fatal("input was mutated")
	}
	ranked[0].Title = "caller mutation"
	cached, err := c.Rank(context.Background(), track, candidates)
	if err != nil || cached[0].Title == "caller mutation" || calls.Load() != 1 {
		t.Fatal("cache did not isolate results", err, calls.Load())
	}
	// Every supplied metadata field contributes to the cache identity.
	candidates[0].Evidence = json.RawMessage(`{"changed":"provider metadata"}`)
	if _, err := c.Rank(context.Background(), track, candidates); err != nil || calls.Load() != 2 {
		t.Fatal("changed evidence reused stale rank", err)
	}
}

func TestModelCannotOverrideExistingMismatchChecks(t *testing.T) {
	track, candidates := fixtures()
	candidates[1].Suitable = false
	candidates[1].Reason = "different version: live"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, rankedResponse()) }))
	defer server.Close()
	c := New("test", "")
	c.endpoint = server.URL
	ranked, err := c.Rank(context.Background(), track, candidates)
	if err != nil || ranked[0].Suitable || !strings.Contains(ranked[0].Reason, "different version: live") {
		t.Fatal("model bypassed mismatch gate", ranked, err)
	}
	if _, ok := discovery.New(nil, nil, discovery.WithRanking(c, Policy())).Select(ranked); ok {
		t.Fatal("mismatched recording selected")
	}
}

func TestUncertainRecordingDoesNotBecomeSuitableFromSourcePreference(t *testing.T) {
	track, candidates := fixtures()
	response := strings.Replace(rankedResponse(), `"match_1":{"type":"noul","noul":0.99}`, `"match_1":{"type":"noul","noul":0.94}`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, response) }))
	defer server.Close()
	c := New("test", "")
	c.endpoint = server.URL
	ranked, err := c.Rank(context.Background(), track, candidates)
	if err != nil || ranked[0].Ref != candidates[1].Ref || ranked[0].Score < Policy().MinimumScore || ranked[0].Suitable {
		t.Fatal("source preference bypassed recording certainty", ranked, err)
	}
	if _, ok := discovery.New(nil, nil, discovery.WithRanking(c, Policy())).Select(ranked); ok {
		t.Fatal("uncertain recording was selected automatically")
	}
}

func TestInvalidResponsesAreRejectedWithoutEchoingSecrets(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		response string
	}{
		{"unauthorized", 401, "test-api-key upstream detail"},
		{"rate limited", 429, "test-api-key upstream detail"},
		{"provider error", 500, "test-api-key upstream detail"},
		{"bad json", 200, "test-api-key upstream detail"},
		{"missing answer", 200, `{"answers":{}}`},
		{"wrong type", 200, strings.Replace(rankedResponse(), `"type":"noul"`, `"type":"score"`, 1)},
		{"missing probability", 200, strings.Replace(rankedResponse(), `,"noul":0.99`, ``, 1)},
		{"negative probability", 200, strings.Replace(rankedResponse(), `"noul":0.99`, `"noul":-0.1`, 1)},
		{"excess probability", 200, strings.Replace(rankedResponse(), `"noul":0.99`, `"noul":1.1`, 1)},
		{"invalid rubric score", 200, strings.Replace(rankedResponse(), `"score":4`, `"score":5`, 1)},
		{"missing rubric score", 200, strings.Replace(rankedResponse(), `,"score":4`, ``, 1)},
		{"unknown question", 200, strings.Replace(rankedResponse(), `"match_0"`, `"unknown"`, 1)},
		{"oversized response", 200, strings.Repeat(" ", maxResponseSize+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()
			c := New("test-api-key", "")
			c.endpoint = server.URL
			track, candidates := fixtures()
			for range 2 {
				result, err := c.Rank(context.Background(), track, candidates)
				if err == nil || result != nil || strings.Contains(err.Error(), "test-api-key") || strings.Contains(err.Error(), "upstream detail") {
					t.Fatal("invalid or unsafe provider failure", err)
				}
			}
			if calls.Load() != 2 {
				t.Fatal("failure was cached")
			}
		})
	}
}

func TestCancellationTimeoutAndRedirects(t *testing.T) {
	track, candidates := fixtures()
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := New("test", "").Rank(ctx, track, candidates); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer server.Close()
		defer close(release)
		c := New("test", "")
		c.endpoint = server.URL
		c.http.Timeout = 30 * time.Millisecond
		start := time.Now()
		if _, err := c.Rank(context.Background(), track, candidates); err == nil || time.Since(start) > time.Second {
			t.Fatal("timeout was not bounded", err)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var redirected atomic.Bool
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Store(true) }))
		defer target.Close()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		c := New("test", "")
		c.endpoint = server.URL
		if _, err := c.Rank(context.Background(), track, candidates); err == nil || redirected.Load() {
			t.Fatal("followed redirect with credentials", err)
		}
	})
}

func TestCacheExpiresAndStaysBounded(t *testing.T) {
	c := New("test", "")
	var key [32]byte
	c.remember(key, []discovery.Candidate{{Title: "expired"}})
	entry := c.cache[key]
	entry.created = time.Now().Add(-cacheTTL)
	c.cache[key] = entry
	if _, ok := c.cached(key); ok {
		t.Fatal("expired result reused")
	}
	for i := range cacheSize + 10 {
		var k [32]byte
		k[0], k[1] = byte(i), byte(i>>8)
		c.remember(k, nil)
	}
	if len(c.cache) != cacheSize {
		t.Fatal("unbounded cache", len(c.cache))
	}
}

func TestOversizedInputFailsBeforeCallingProvider(t *testing.T) {
	track, candidates := fixtures()
	candidates[0].Evidence = json.RawMessage(`{"description":"` + strings.Repeat("x", maxRequestSize) + `"}`)
	if _, err := New("test", "").Rank(context.Background(), track, candidates); err == nil {
		t.Fatal("oversized request accepted")
	}
}
