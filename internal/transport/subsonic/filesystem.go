package subsonic

import (
	"crypto/sha256"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// filesystem projects directory identities independently of ID3 album identities.
// Legacy directory IDs must round-trip through search and album lists.
func (c collection) filesystem() collection {
	out := collection{artists: []artist{}, albums: []album{}, songs: []song{}, tracks: c.tracks, lastPlayed: map[string]time.Time{}, history: c.history}
	albums := map[string]*album{}
	artists := map[string]*artist{}
	for _, source := range c.songs {
		t := c.tracks[source.ID]
		directory := path.Dir(t.Path)
		parts := strings.Split(directory, "/")
		root := parts[0]
		if directory == "." {
			root = "."
		}
		artistID := directoryID(root)
		albumID := directoryID(directory)
		art := artists[artistID]
		if art == nil {
			name := root
			if root == "." {
				name = "Music"
			}
			art = &artist{ID: artistID, Name: name, Albums: []album{}}
			artists[artistID] = art
		}
		alb := albums[albumID]
		if alb == nil {
			name := path.Base(directory)
			if directory == "." {
				name = "Music"
			}
			alb = &album{
				ID:       albumID,
				Name:     name,
				Artist:   art.Name,
				ArtistID: artistID,
				Created:  source.Created,
				Genre:    source.Genre,
				Year:     source.Year,
				CoverArt: source.ID,
				Songs:    []song{},
			}
			albums[albumID] = alb
		}
		s := source
		s.Parent = albumID
		alb.Songs = append(alb.Songs, s)
		alb.SongCount++
		alb.Duration += s.Duration
		alb.PlayCount += s.PlayCount
		if s.Created < alb.Created {
			alb.Created = s.Created
		}
		if stamp, _ := time.Parse(time.RFC3339Nano, c.history[s.ID].LastPlayed); stamp.After(out.lastPlayed[albumID]) {
			out.lastPlayed[albumID] = stamp
		}
		art.CoverArt = alb.CoverArt
		out.songs = append(out.songs, s)
	}
	for _, alb := range albums {
		out.albums = append(out.albums, *alb)
		art := artists[alb.ArtistID]
		art.Albums = append(art.Albums, *alb)
		art.AlbumCount++
	}
	for _, art := range artists {
		sort.Slice(art.Albums, func(i, j int) bool { return art.Albums[i].Name < art.Albums[j].Name })
		out.artists = append(out.artists, *art)
	}
	sort.Slice(out.albums, func(i, j int) bool { return strings.ToLower(out.albums[i].Name) < strings.ToLower(out.albums[j].Name) })
	sort.Slice(out.artists, func(i, j int) bool {
		return strings.ToLower(out.artists[i].Name) < strings.ToLower(out.artists[j].Name)
	})
	return out
}
func (c collection) directoryTracks(key string) []song {
	songs := []song{}
	for _, s := range c.songs {
		directory := path.Dir(c.tracks[s.ID].Path)
		for {
			if directoryID(directory) == key {
				songs = append(songs, s)
				break
			}
			if directory == "." {
				break
			}
			directory = path.Dir(directory)
		}
	}
	return songs
}
func directoryID(path string) string {
	if path == "." || path == "" {
		return "1"
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte("directory\x00"+path)))
}
