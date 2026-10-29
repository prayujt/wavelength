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
