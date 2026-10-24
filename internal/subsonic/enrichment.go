package subsonic

import (
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
)

// Enrichment uses only real catalog relationships and local listening history.
// External biographies/Last.fm rankings are intentionally absent without a provider.
func (a *api) enrichment(r *http.Request, result *response, c collection, endpoint string) {
	if endpoint == "getArtistInfo" || endpoint == "getAlbumInfo" || endpoint == "getSimilarSongs" {
		if tracks := c.directoryTracks(r.Form.Get("id")); len(tracks) > 0 {
			r.Form.Set("id", tracks[0].ID)
		}
	}
	if a.providerEnrichment(r, result, c, endpoint) {
		return
	}
	if endpoint == "getTopSongs" {
		name := required(r, result, "artist")
		if result.Error != nil {
			return
		}
		count, err := number(r, "count", 50, 500)
		if err != nil {
			failure(result, 10, err.Error())
			return
		}
		songs := []song{}
		for _, s := range c.songs {
			if strings.EqualFold(name, s.Artist) && s.PlayCount > 0 {
				songs = append(songs, s)
			}
		}
		sort.SliceStable(songs, func(i, j int) bool { return songs[i].PlayCount > songs[j].PlayCount })
		payload(result, "topSongs", map[string]any{"song": pageSlice(songs, 0, count)})
		return
	}
	key := required(r, result, "id")
	if result.Error != nil {
		return
	}
	var artistID, albumID string
	for _, art := range c.artists {
		if art.ID == key {
			artistID = key
		}
	}
	for _, alb := range c.albums {
		if alb.ID == key {
			artistID = alb.ArtistID
			albumID = alb.ID
		}
	}
	for _, s := range c.songs {
		if s.ID == key {
			artistID = s.ArtistID
			albumID = s.AlbumID
		}
	}
	if artistID == "" {
		failure(result, 70, "Media not found")
		return
	}
	if endpoint == "getAlbumInfo" || endpoint == "getAlbumInfo2" {
		if albumID == "" {
			failure(result, 70, "Album not found")
			return
		}
		payload(result, "albumInfo", map[string]any{})
		return
	}
	genres := map[string]bool{}
	for _, s := range c.songs {
		if s.ArtistID == artistID && s.Genre != "" {
			genres[strings.ToLower(s.Genre)] = true
		}
	}
	if endpoint == "getArtistInfo" || endpoint == "getArtistInfo2" {
		count, err := number(r, "count", 20, 500)
		if err != nil {
			failure(result, 10, err.Error())
			return
		}
		similar := []artist{}
		for _, art := range c.artists {
			if art.ID == artistID {
				continue
			}
			matched := false
			for _, alb := range art.Albums {
				matched = matched || genres[strings.ToLower(alb.Genre)]
			}
			if matched {
				art.Albums = nil
				similar = append(similar, art)
			}
		}
		name := "artistInfo2"
		if endpoint == "getArtistInfo" {
			name = "artistInfo"
		}
		payload(result, name, map[string]any{"similarArtist": pageSlice(similar, 0, count)})
		return
	}
	count, err := number(r, "count", 50, 500)
	if err != nil {
		failure(result, 10, err.Error())
		return
	}
	songs := []song{}
	for _, s := range c.songs {
		if s.ArtistID == artistID || genres[strings.ToLower(s.Genre)] {
			songs = append(songs, s)
		}
	}
	rand.Shuffle(len(songs), func(i, j int) { songs[i], songs[j] = songs[j], songs[i] })
	name := "similarSongs2"
	if endpoint == "getSimilarSongs" {
		name = "similarSongs"
	}
	payload(result, name, map[string]any{"song": pageSlice(songs, 0, count)})
}
