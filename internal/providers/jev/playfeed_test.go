package jev

import (
	"context"
	"encoding/json"
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
	state := playfeed.Context{Seed: track, Upcoming: []library.Track{track}, Preferences: []playfeed.Preference{{Track: track, Completed: 3, EarlySkips: 1, OtherSkips: 2, ListenedMS: 600000, LastPlayedAt: time.Now().UTC()}}}
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
				Seed        feedTrack        `json:"seed"`
				Upcoming    []feedTrack      `json:"upcoming_manual_queue"`
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
