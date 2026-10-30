package playfeed

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/prayujt/wavelength/internal/library"
)

func TestExplorationConfidenceRequiresBroadMeaningfulHistory(t *testing.T) {
	history := func(tracks, artists int) Context {
		c := Context{}
		for i := 0; i < tracks; i++ {
			c.Preferences = append(c.Preferences, Preference{Track: library.Track{ID: fmt.Sprint(i), Artist: fmt.Sprintf("artist-%d", i%artists)}, Sessions: 1000, Completed: 1000, ListenedMS: 180000000})
		}
		return c
	}
	if p := ExplorationFor(Context{}); p.Fraction != 1 || p.Confidence != 0 {
		t.Fatal(p)
	}
	// Hundreds of repeat sessions on one artist still do not establish broad taste.
	for _, c := range []Context{history(5, 2), history(100, 1), history(30, 10)} {
		if p := ExplorationFor(c); p.Fraction < .94 {
			t.Fatalf("small/narrow history overpersonalized: %+v", p)
		}
	}
	previous := 1.0
	for _, n := range []int{5, 25, 50, 100, 200} {
		p := ExplorationFor(history(n, 40))
		if p.Fraction > previous || p.Fraction < .2-1e-9 {
			t.Fatal(p)
		}
		previous = p.Fraction
	}
	if p := ExplorationFor(history(200, 40)); math.Abs(p.Fraction-.2) > 1e-9 {
		t.Fatal(p)
	}
	c := history(200, 40)
	for i := range c.Preferences {
		c.Preferences[i].ListenedMS = 1000
	}
	if p := ExplorationFor(c); p.Confidence != 0 {
		t.Fatal("tiny skips counted as taste evidence", p)
	}
}

func TestSparseHistoryEscapesOverrankedSoundtrackAndFolkThemes(t *testing.T) {
	state := Context{Preferences: []Preference{
		{Track: library.Track{ID: "old-folk", Artist: "Gregory Alan Isakov", Genre: "Folk"}, Completed: 2, ListenedMS: 360000},
		{Track: library.Track{ID: "old-score", Artist: "Howard Shore", Genre: "Soundtrack"}, Completed: 2, ListenedMS: 360000},
	}}
	tracks := []library.Track{}
	scores := map[string]float64{}
	for i := 0; i < 90; i++ {
		artist, genre := "Gregory Alan Isakov", "Folk"
		if i%2 == 0 {
			artist, genre = "Howard Shore", "Soundtrack"
		}
		id := fmt.Sprint(i)
		tracks = append(tracks, library.Track{ID: id, Artist: artist, Genre: genre, Album: "Familiar"})
		scores[id] = 1
	}
	for i := 90; i < 120; i++ {
		id := fmt.Sprint(i)
		tracks = append(tracks, library.Track{ID: id, Artist: "New Artist " + id, Genre: fmt.Sprintf("Genre %d", i%10), Album: "New " + id})
		scores[id] = 0
	}
	rng := rand.New(rand.NewPCG(12, 34))
	novel := 0
	different := map[string]bool{}
	for range 500 {
		selected := exploratorySelection(state, tracks, scores, 3, "Gregory Alan Isakov", rng.Float64)
		seen := map[string]bool{}
		for _, track := range selected {
			if seen[track.ID] {
				t.Fatal("duplicate", track.ID)
			}
			seen[track.ID] = true
			if track.Artist != "Gregory Alan Isakov" && track.Artist != "Howard Shore" {
				novel++
				different[track.Artist] = true
			}
		}
	}
	if novel < 1300 || len(different) < 25 {
		t.Fatalf("stuck in familiar themes despite broad alternatives: novel=%d/1500 distinct=%d", novel, len(different))
	}
	// Random selection cannot manufacture alternatives in a tiny catalog.
	if got := exploratorySelection(state, tracks[:1], scores, 3, "", rng.Float64); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestFocusedHistoryCanMatureWithoutManyArtists(t *testing.T) {
	state := Context{}
	for i := range 400 {
		state.Preferences = append(state.Preferences, Preference{Track: library.Track{ID: fmt.Sprint(i), Artist: "Focused Artist"}, Completed: 3, Sessions: 3, ListenedMS: 900000})
	}
	if p := ExplorationFor(state); p.Confidence != 1 || math.Abs(p.Fraction-.2) > 1e-9 {
		t.Fatal("focused evidence was permanently capped", p)
	}
	for i := range state.Preferences {
		state.Preferences[i].Completed = 0
		state.Preferences[i].Sessions = 60
		state.Preferences[i].EarlySkips = 60
		state.Preferences[i].ListenedMS = 300000
	}
	if p := ExplorationFor(state); p.Confidence != 0 || p.Fraction != 1 {
		t.Fatal("accumulated tiny skips manufactured confidence", p)
	}
}

func TestCompilationAndPreviousAlbumReceiveRepeatPenalty(t *testing.T) {
	state := Context{Preferences: []Preference{{Track: library.Track{ID: "old", Artist: "Old"}, ListenedMS: 30000}}}
	tracks := []library.Track{
		{ID: "a", Artist: "Singer A", AlbumArtist: "Various Artists", Album: "Soundtrack"},
		{ID: "b", Artist: "Singer B", AlbumArtist: "Various Artists", Album: "Soundtrack"},
		{ID: "c", Artist: "Singer C", AlbumArtist: "Various Artists", Album: "Other"},
	}
	scores := map[string]float64{"a": 1, "b": 1, "c": .9}
	got := exploratorySelection(state, tracks, scores, 2, "", func() float64 { return .99 })
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Fatal("compilation not penalized", got)
	}
	state.Seed = tracks[0]
	got = exploratorySelection(state, tracks[1:], scores, 1, "Singer A", func() float64 { return .99 })
	if got[0].ID != "c" {
		t.Fatal("previous album ignored", got)
	}
}

func TestExplorationRetainsBoundedRankingInfluence(t *testing.T) {
	state := Context{}
	tracks := []library.Track{{ID: "a", Artist: "A", Album: "A"}, {ID: "b", Artist: "B", Album: "B"}}
	rng := rand.New(rand.NewPCG(100, 200))
	wins := 0
	for range 10000 {
		if exploratorySelection(state, tracks, map[string]float64{"a": 1, "b": 0}, 1, "", rng.Float64)[0].ID == "a" {
			wins++
		}
	}
	if wins < 5300 || wins > 5800 {
		t.Fatalf("ranking should nudge, not dominate or disappear: %d", wins)
	}
}
