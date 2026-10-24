package library

import (
	"strconv"
	"strings"
)

// fillTags supplies missing fields without replacing values read by the primary parser.
func fillTags(track *Track, tags map[string]string) {
	for _, field := range []struct {
		target *string
		key    string
	}{
		{&track.Title, "title"}, {&track.Artist, "artist"}, {&track.Album, "album"},
		{&track.AlbumArtist, "albumartist"}, {&track.Genre, "genre"},
	} {
		if *field.target == "" {
			*field.target = tags[field.key]
		}
	}
	if track.TrackNumber == 0 {
		track.TrackNumber = tagNumber(tags, "track", "tracknumber")
	}
	if track.DiscNumber == 0 {
		track.DiscNumber = tagNumber(tags, "disc", "discnumber")
	}
	if track.Year <= 1 {
		for _, key := range []string{"date", "year"} {
			value := tags[key]
			if len(value) >= 4 {
				if year, err := strconv.Atoi(value[:4]); err == nil {
					track.Year = year
					break
				}
			}
		}
	}
}

func tagNumber(tags map[string]string, keys ...string) int {
	for _, key := range keys {
		value, _, _ := strings.Cut(tags[key], "/")
		if number, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return number
		}
	}
	return 0
}
