package youtube

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
)

func TestCandidateMatchingHandlesVersionsDurationAndUnicode(t *testing.T) {
	track := discovery.Track{Title: "Last Night", Artist: "Morgan Wallen", Duration: 180}
	for _, test := range []struct {
		title, uploader string
		duration        float64
		want            bool
	}{
		{"Morgan Wallen - Last Night (Official Audio)", "Morgan Wallen", 180, true},
		{"Last Night", "Someone Else", 180, false},
		{"Morgan Wallen Last Night Live", "Morgan Wallen", 180, false},
		{"Morgan Wallen Last Night", "Morgan Wallen", 420, false},
		{"Morgan Wallen Last Night", "Morgan Wallen", 0, false},
	} {
		_, suitable, _ := match(track, test.title, test.uploader, test.duration)
		if suitable != test.want {
			t.Errorf("%s: suitable=%v", test.title, suitable)
		}
	}
	score, suitable, _ := match(discovery.Track{Title: "तुम ही हो", Artist: "अरिजीत सिंह", Duration: 240}, "तुम ही हो", "अरिजीत सिंह", 240)
	if !suitable || score < .99 {
		t.Fatal("non-Latin matching failed", score, suitable)
	}
}
func fakeCommand(t *testing.T, body string) *Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	client := New()
	client.binary = path
	return client
}
func TestCandidatesAreRankedAndDownloadArgumentsAreBounded(t *testing.T) {
	args := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("WAVELENGTH_TEST_ARGUMENTS", args)
	c := fakeCommand(t, `printf '%s\n' "$@" > "$WAVELENGTH_TEST_ARGUMENTS"
printf '%s' '{"entries":[{"id":"abcdefghijk","title":"Other","uploader":"Other","duration":20},{"id":"BaW_jenozKc","title":"Song Artist","uploader":"Artist","duration":180}]}'
`)
	results, err := c.Candidates(context.Background(), discovery.Track{Title: "Song", Artist: "Artist", Duration: 180})
	if err != nil || len(results) != 2 || results[0].Ref.ID != "BaW_jenozKc" || !results[0].Suitable {
		t.Fatal(results, err)
	}
	raw, _ := os.ReadFile(args)
	if !strings.Contains(string(raw), "ytsearch15:Song Artist") {
		t.Fatal(string(raw))
	}
	if err = c.Download(context.Background(), results[0].Ref, "/tmp/audio.mp3"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(args)
	for _, want := range []string{"--ignore-config", "--no-playlist", "--max-filesize\n256M", "--\nhttps://www.youtube.com/watch?v=BaW_jenozKc"} {
		if !strings.Contains(string(raw), want) {
			t.Fatal(want, string(raw))
		}
	}
	if err = c.Download(context.Background(), discovery.Ref{Provider: "youtube", ID: "--exec bad"}, "/tmp/audio.mp3"); err == nil {
		t.Fatal("unsafe source reference accepted")
	}
}
func TestCancellationStopsTheDownloaderProcess(t *testing.T) {
	c := fakeCommand(t, "sleep 30 &\nwait\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.run(ctx, "ignored")
	if err == nil || time.Since(start) > time.Second {
		t.Fatal(err, time.Since(start))
	}
}

func TestCandidatesPreserveAllProviderMetadataForRanking(t *testing.T) {
	c := fakeCommand(t, `printf '%s' '{"entries":[{"id":"BaW_jenozKc","title":"Song Artist","uploader":"Artist","duration":180,"artist":"Artist","album":"Album","channel_id":"channel-id","channel_is_verified":true,"description":"Provided to YouTube","unknown_future_field":{"nested":true}}]}'`)
	results, err := c.Candidates(context.Background(), discovery.Track{Title: "Song", Artist: "Artist", Duration: 180})
	if err != nil || len(results) != 1 {
		t.Fatal(results, err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(results[0].Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["artist"] != "Artist" || evidence["album"] != "Album" || evidence["channel_id"] != "channel-id" || evidence["channel_is_verified"] != true || evidence["unknown_future_field"] == nil {
		t.Fatal("provider evidence dropped", evidence)
	}
	public, err := json.Marshal(results[0])
	if err != nil || strings.Contains(string(public), "unknown_future_field") || strings.Contains(string(public), "channel_id") {
		t.Fatal("internal evidence leaked into API", err)
	}
}
