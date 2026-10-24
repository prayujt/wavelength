package subsonic

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/prayujt/wavelength/internal/metadata"
)

func WithMetadataProvider(provider metadata.Provider) Option {
	return func(a *api) error { a.metadata = provider; return nil }
}
func WithScrobbler(provider metadata.Scrobbler) Option {
	return func(a *api) error { a.scrobbler = provider; return nil }
}
func imageFields(images []metadata.Image) map[string]any {
	out := map[string]any{}
	for _, image := range images {
		switch image.Size {
		case "small":
			out["smallImageUrl"] = image.URL
		case "medium":
			out["mediumImageUrl"] = image.URL
		case "large", "extralarge", "mega":
			out["largeImageUrl"] = image.URL
		}
	}
	return out
}
func (a *api) providerEnrichment(r *http.Request, result *response, c collection, endpoint string) bool {
	if a.metadata == nil {
		return false
	}
	if endpoint == "getTopSongs" {
		name := required(r, result, "artist")
		if result.Error != nil {
			return true
		}
		count, err := number(r, "count", 50, 500)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		ranked, err := a.metadata.TopTracks(r.Context(), name, count)
		if err != nil {
			failure(result, 0, "Music metadata provider unavailable")
			return true
		}
		songs := []song{}
		seen := map[string]bool{}
		for _, entry := range ranked {
			for _, s := range c.songs {
				if strings.EqualFold(s.Title, entry.Name) && strings.EqualFold(s.Artist, entry.Artist.Name) && !seen[s.ID] {
					songs = append(songs, s)
					seen[s.ID] = true
				}
			}
		}
		payload(result, "topSongs", map[string]any{"song": pageSlice(songs, 0, count)})
		return true
	}
	key := r.Form.Get("id")
	artistName := ""
	albumName := ""
	for _, art := range c.artists {
		if art.ID == key {
			artistName = art.Name
		}
	}
	for _, alb := range c.albums {
		if alb.ID == key {
			artistName = alb.Artist
			albumName = alb.Name
		}
	}
	for _, s := range c.songs {
		if s.ID == key {
			artistName = s.Artist
			albumName = s.Album
		}
	}
	if artistName == "" {
		return false
	}
	if endpoint == "getAlbumInfo" || endpoint == "getAlbumInfo2" {
		if albumName == "" {
			return false
		}
		info, err := a.metadata.Album(r.Context(), artistName, albumName)
		if err != nil {
			failure(result, 0, "Music metadata provider unavailable")
			return true
		}
		out := imageFields(info.Images)
		out["notes"] = info.Wiki.Content
		if out["notes"] == "" {
			out["notes"] = info.Wiki.Summary
		}
		out["musicBrainzId"] = info.MusicBrainzID
		out["lastFmUrl"] = info.URL
		payload(result, "albumInfo", out)
		return true
	}
	info, err := a.metadata.Artist(r.Context(), artistName)
	if err != nil {
		failure(result, 0, "Music metadata provider unavailable")
		return true
	}
	similar := []artist{}
	known := map[string]artist{}
	for _, art := range c.artists {
		art.Albums = nil
		known[strings.ToLower(art.Name)] = art
	}
	include := false
	if r.Form.Has("includeNotPresent") {
		include, err = strconv.ParseBool(r.Form.Get("includeNotPresent"))
		if err != nil {
			failure(result, 10, "Invalid includeNotPresent")
			return true
		}
	}
	for _, entry := range info.Similar.Artists {
		if art, ok := known[strings.ToLower(entry.Name)]; ok {
			similar = append(similar, art)
		} else if include {
			similar = append(similar, artist{ID: "-1", Name: entry.Name})
		}
	}
	if endpoint == "getArtistInfo" || endpoint == "getArtistInfo2" {
		count, err := number(r, "count", 20, 500)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		out := imageFields(info.Images)
		out["biography"] = info.Bio.Content
		if out["biography"] == "" {
			out["biography"] = info.Bio.Summary
		}
		out["musicBrainzId"] = info.MusicBrainzID
		out["lastFmUrl"] = info.URL
		out["similarArtist"] = pageSlice(similar, 0, count)
		name := "artistInfo2"
		if endpoint == "getArtistInfo" {
			name = "artistInfo"
		}
		payload(result, name, out)
		return true
	}
	if endpoint == "getSimilarSongs" || endpoint == "getSimilarSongs2" {
		count, err := number(r, "count", 50, 500)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		names := map[string]bool{strings.ToLower(artistName): true}
		for _, entry := range similar {
			names[strings.ToLower(entry.Name)] = true
		}
		songs := []song{}
		for _, s := range c.songs {
			if names[strings.ToLower(s.Artist)] {
				songs = append(songs, s)
			}
		}
		name := "similarSongs2"
		if endpoint == "getSimilarSongs" {
			name = "similarSongs"
		}
		payload(result, name, map[string]any{"song": pageSlice(songs, 0, count)})
		return true
	}
	return false
}
