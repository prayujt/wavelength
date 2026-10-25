package youtube

import (
	"github.com/prayujt/wavelength/internal/discovery"
	"math"
	"strings"
	"unicode"
)

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}
func overlap(want, have string) float64 {
	required := tokens(want)
	if len(required) == 0 {
		return 0
	}
	available := map[string]bool{}
	for _, word := range tokens(have) {
		available[word] = true
	}
	hits := 0
	for _, word := range required {
		if available[word] {
			hits++
		}
	}
	return float64(hits) / float64(len(required))
}
func match(t discovery.Track, title, uploader string, duration float64) (float64, bool, string) {
	titleMatch := overlap(t.Title, title)
	artistMatch := overlap(t.Artist, title+" "+uploader)
	delta := math.Abs(duration - t.Duration)
	durationMatch := math.Max(0, 1-delta/math.Max(t.Duration, 1))
	score := .45*titleMatch + .3*artistMatch + .25*durationMatch
	suitable := duration > 0 && t.Duration > 0 && delta <= math.Max(10, t.Duration*.08) && titleMatch >= .8 && artistMatch >= .8
	reason := "title, artist and duration match"
	if !suitable {
		reason = "title, artist or duration does not match closely"
	}
	// Alternative versions are valid explicit choices, but should not be automatic substitutions.
	for _, version := range []string{"live", "cover", "remix", "acoustic", "karaoke", "instrumental", "slowed", "sped"} {
		if overlap(version, title) > 0 && overlap(version, t.Title) == 0 {
			suitable = false
			score -= .3
			reason = "different version: " + version
			break
		}
	}
	return math.Max(0, score), suitable, reason
}
