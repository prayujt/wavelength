package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/playfeed"
)

func TestPlayFeedUsesMusicalMetadataAndListeningSignalsOnly(t *testing.T) {
	track := library.Track{ID: "catalog-id", Title: "Dandelion Wine", Artist: "Gregory Alan Isakov", Album: "This Empty Northern Hemisphere", AlbumArtist: "Gregory Alan Isakov", Genre: "Folk", Year: 2009, TrackNumber: 1, DiscNumber: 1, Duration: 184.9, Path: "/private/music/recording.flac"}
	now := time.Now().UTC()
	state := playfeed.Context{AsOf: now, Recent: []playfeed.RecentListen{{Track: track, StartedAt: now.Add(-time.Minute), UpdatedAt: now, ListenedMS: 45000, PositionMS: 60000, Playing: true}}, Seed: track, Upcoming: []library.Track{track}, Preferences: []playfeed.Preference{{Track: track, Completed: 3, EarlySkips: 1, OtherSkips: 2, ListenedMS: 600000, LastPlayedAt: now}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Error("missing authentication")
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "/private/") || strings.Contains(string(raw), "test-api-key") {
			t.Error("sensitive data in model input")
		}
		var input struct {
			Model string `json:"model"`
			State struct {
				AsOf        time.Time        `json:"as_of"`
				Recent      []feedListen     `json:"recent_listening_newest_first"`
				Seed        feedTrack        `json:"seed"`
				Upcoming    []feedTrack      `json:"upcoming_queue"`
				Preferences []feedPreference `json:"listening_preferences"`
				Candidates  []feedTrack      `json:"candidates"`
			} `json:"state"`
			Questions map[string]question `json:"questions"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			t.Error(err)
			return
		}
		want := feedMetadata(track)
		if !input.State.AsOf.Equal(now) || len(input.State.Recent) != 1 || input.State.Recent[0].ListenedMS != 45000 || input.State.Recent[0].PositionMS != 60000 || !input.State.Recent[0].Playing || input.State.Recent[0].Track != want || !input.State.Recent[0].StartedAt.Equal(now.Add(-time.Minute)) {
			t.Error("current playback and chronological timing context were lost")
		}
		if input.Model != "jev-1.14.0" || input.State.Seed != want || !reflect.DeepEqual(input.State.Upcoming, []feedTrack{want}) || !reflect.DeepEqual(input.State.Candidates, []feedTrack{want}) || len(input.Questions) != 1 {
			t.Error("musical context was lost")
		}
		if len(input.State.Preferences) != 1 || input.State.Preferences[0].Completed != 3 || input.State.Preferences[0].EarlySkips != 1 || input.State.Preferences[0].OtherSkips != 2 || input.State.Preferences[0].ListenedMS != 600000 || input.State.Preferences[0].Track != want {
			t.Error("listening signals were lost")
		}
		io.WriteString(w, `{"answers":{"recommend_0":{"type":"score","score":3}},"usage":{"input_tokens":40,"output_tokens":8}}`)
	}))
	defer server.Close()
	client := New("test-api-key", "")
	client.endpoint = server.URL
	got, err := client.Recommend(context.Background(), state, []library.Track{track})
	if err != nil || !reflect.DeepEqual(got, []playfeed.Score{{TrackID: track.ID, Value: .75}}) {
		t.Fatal(got, err)
	}
}
func TestPlayFeedRejectsInvalidProviderResponses(t *testing.T) {
	for _, body := range []string{`test-api-key upstream detail`, `{"answers":{}}`, `{"answers":{"unknown":{"type":"score","score":3}}}`, `{"answers":{"recommend_0":{"type":"score"}}}`, `{"answers":{"recommend_0":{"type":"noul","noul":0.9}}}`, `{"answers":{"recommend_0":{"type":"score","score":5}}}`, `{"answers":{"recommend_0":{"type":"score","score":-1}}}`, strings.Repeat(" ", maxResponseSize+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		client := New("test-api-key", "")
		client.endpoint = server.URL
		_, err := client.Recommend(context.Background(), playfeed.Context{}, []library.Track{{ID: "a"}})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "test-api-key") || strings.Contains(err.Error(), "upstream detail") {
			t.Fatal("invalid or unsafe failure", err)
		}
	}
	for _, status := range []int{401, 429, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, "test-api-key upstream detail")
		}))
		client := New("test-api-key", "")
		client.endpoint = server.URL
		_, err := client.Recommend(context.Background(), playfeed.Context{}, []library.Track{{ID: "a"}})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "test-api-key") {
			t.Fatal(err)
		}
	}
}

func TestLargeLifetimeHistoryKeepsCompleteTotalsAndCandidateEvidence(t *testing.T) {
	state := playfeed.Context{AsOf: time.Now()}
	for i := 0; i < 2000; i++ {
		state.Preferences = append(state.Preferences, playfeed.Preference{Track: library.Track{ID: fmt.Sprintf("track-%d", i), Title: "An observed song with musical context", Artist: fmt.Sprintf("Artist %d", i%10), Genre: "Folk", Duration: 180}, Sessions: 1, Completed: 1, ListenedMS: 180000, LastPlayedAt: time.Now()})
	}
	candidate := state.Preferences[1999].Track
	raw, err := recommendationPayload(state, []library.Track{candidate}, "test-model")
	if err != nil || len(raw) > maxRequestSize {
		t.Fatal("history context did not fit the provider budget", len(raw), err)
	}
	var input struct {
		State struct {
			History       feedHistoryTotals      `json:"lifetime_listening_totals"`
			Artists       []feedArtistPreference `json:"lifetime_artist_preferences"`
			Preferences   []feedPreference       `json:"listening_preferences"`
			DetailsCapped bool                   `json:"track_detail_budgeted"`
		} `json:"state"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if input.State.History.Sessions != 2000 || input.State.History.ListenedMS != 2000*180000 || len(input.State.Artists) != 10 || !input.State.DetailsCapped {
		t.Fatal("full lifetime history was lost from the budgeted request", input.State.History)
	}
	found := false
	for _, p := range input.State.Preferences {
		if p.Track.ID == candidate.ID && p.ListenedMS == 180000 {
			found = true
		}
	}
	if !found {
		t.Fatal("candidate-specific listening evidence was dropped")
	}
}
