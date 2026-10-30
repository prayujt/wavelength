package playfeed

import (
	"math"
	"math/rand/v2"
	"strings"

	"github.com/prayujt/wavelength/internal/library"
)

// ExplorationPolicy measures breadth of actual listening, not raw repeat count.
// Replaying one artist many times must not turn a cold-start guess into certainty.
type ExplorationPolicy struct {
	MeaningfulTracks int     `json:"meaningful_tracks"`
	Artists          int     `json:"artists"`
	ListenedMS       int64   `json:"actual_listened_ms"`
	Confidence       float64 `json:"confidence"`
	Fraction         float64 `json:"exploration_fraction"`
}

func ExplorationFor(state Context) ExplorationPolicy {
	p := ExplorationPolicy{}
	artists := map[string]bool{}
	tracks := map[string]bool{}
	for _, pref := range state.Preferences {
		// Tiny skips, seeks and zero-time session reports do not establish taste.
		if !meaningful(pref) {
			continue
		}
		if tracks[pref.Track.ID] {
			continue
		}
		tracks[pref.Track.ID] = true
		p.MeaningfulTracks++
		if artist := artistKey(pref.Track); artist != "" {
			artists[artist] = true
		}
		// Cap repeated plays of one recording at one track's evidence budget.
		p.ListenedMS += min(pref.ListenedMS, 300000)
	}
	p.Artists = len(artists)
	// Broad listening establishes confidence sooner. Focused taste can also become
	// well established, but needs up to twice the distinct tracks/time. Artist
	// breadth is not a permanent ceiling on what a narrow library can learn.
	breadth := min(1, float64(p.Artists)/40)
	evidenceTarget := 2 - breadth
	p.Confidence = min(1, float64(p.MeaningfulTracks)/(200*evidenceTarget), float64(p.ListenedMS)/(float64(10*60*60*1000)*evidenceTarget))
	// A slow quadratic ramp keeps small histories overwhelmingly exploratory.
	progress := max(0, (p.Confidence-.25)/.75)
	p.Fraction = .95 - .75*progress*progress
	if p.MeaningfulTracks == 0 {
		p.Fraction = 1
	}
	return p
}
func meaningful(p Preference) bool {
	return p.ListenedMS >= 30000 && !(p.Sessions > 0 && p.EarlySkips == p.Sessions && p.Completed == 0)
}
func albumKey(t library.Track) string {
	identity := t.AlbumIdentity()
	return strings.ToLower(strings.TrimSpace(identity.Artist)) + "\x00" + strings.ToLower(strings.TrimSpace(identity.Album))
}
func artistKey(t library.Track) string {
	artist := t.AlbumArtist
	if strings.TrimSpace(artist) == "" || strings.EqualFold(strings.TrimSpace(artist), "Various Artists") {
		artist = t.Artist
	}
	return strings.ToLower(strings.TrimSpace(artist))
}
func genreKey(t library.Track) string { return strings.ToLower(strings.TrimSpace(t.Genre)) }

func exploratorySelection(state Context, tracks []library.Track, scores map[string]float64, n int, anchor string, draw func() float64) []library.Track {
	remaining := append([]library.Track{}, tracks...)
	out := []library.Track{}
	policy := ExplorationFor(state)
	heardArtists := map[string]bool{}
	heardGenres := map[string]bool{}
	negative := map[string]int{}
	for _, p := range state.Preferences {
		if meaningful(p) {
			heardArtists[artistKey(p.Track)] = true
			if g := genreKey(p.Track); g != "" {
				heardGenres[g] = true
			}
		}
		negative[p.Track.ID] += max(0, p.EarlySkips-p.Completed)
	}
	artistCounts := map[string]int{}
	genreCounts := map[string]int{}
	for _, t := range tracks {
		artistCounts[artistKey(t)]++
		genreCounts[genreKey(t)]++
	}
	usedArtists := map[string]int{}
	usedAlbums := map[string]int{}
	usedGenres := map[string]int{}
	if len(state.Upcoming) > 0 {
		usedAlbums[albumKey(state.Upcoming[len(state.Upcoming)-1])]++
	} else if state.Seed.ID != "" {
		usedAlbums[albumKey(state.Seed)]++
	}
	anchor = strings.ToLower(strings.TrimSpace(anchor))
	for len(out) < n && len(remaining) > 0 {
		explore := draw() < policy.Fraction
		best := 0
		bestValue := math.Inf(-1)
		// Weighting the artist rather than each recording prevents large discographies
		// and soundtrack albums from buying most of the random exploration slots.
		for i, t := range remaining {
			artist := artistKey(t)
			genre := genreKey(t)
			album := albumKey(t)
			value := scores[t.ID] - .3*float64(usedArtists[artist]) - .2*float64(usedAlbums[album])
			if artist != "" && (artist == anchor || strings.EqualFold(t.Artist, anchor)) {
				value -= .35
			}
			if explore {
				// Keep Jev's opinion a bounded nudge even while exploring. A
				// maximum score cannot overwhelm artist/genre diversity weights.
				weight := (.9 + .2*scores[t.ID]) / float64(artistCounts[artist])
				weight /= math.Sqrt(float64(genreCounts[genre]))
				if heardArtists[artist] {
					weight *= .15
				}
				if heardGenres[genre] {
					weight *= .5
				}
				weight /= math.Pow(8, float64(usedArtists[artist])) * math.Pow(4, float64(usedAlbums[album])) * math.Pow(2, float64(usedGenres[genre]))
				if artist != "" && (artist == anchor || strings.EqualFold(t.Artist, anchor)) {
					weight *= .1
				}
				if negative[t.ID] > 0 {
					weight /= 1 + 19*float64(negative[t.ID])
				}
				// Independent exponential clocks implement weighted sampling without replacement.
				value = math.Log(max(draw(), math.SmallestNonzeroFloat64)) / weight
			}
			if value > bestValue {
				best, bestValue = i, value
			}
		}
		selected := remaining[best]
		out = append(out, selected)
		artist := artistKey(selected)
		genre := genreKey(selected)
		usedArtists[artist]++
		usedGenres[genre]++
		usedAlbums[albumKey(selected)]++
		anchor = artist
		remaining = append(remaining[:best], remaining[best+1:]...)
	}
	return out
}
func selectExploring(state Context, tracks []library.Track, scores map[string]float64, n int, anchor string) []library.Track {
	return exploratorySelection(state, tracks, scores, n, anchor, rand.Float64)
}
