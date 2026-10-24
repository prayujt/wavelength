package library

import (
	"context"
	"sort"
	"strings"
	"time"
)

type Artist struct {
	ID, Name string
	Albums   []Album
}
type Album struct {
	ID, Name, ArtistID, Artist, Genre string
	Year                              int
	CreatedAt                         time.Time
	Tracks                            []Track
	Duration                          float64
}

// Catalog is a protocol-independent projection shared by native RPC and legacy adapters.
type Catalog struct {
	Artists []Artist
	Albums  []Album
	Tracks  []Track
}

type AlbumIdentity struct {
	Artist, Album     string
	ArtistID, AlbumID string
}

// AlbumIdentity applies the same grouping and fallback rules for every transport.
func (t Track) AlbumIdentity() AlbumIdentity {
	artist := t.AlbumArtist
	if artist == "" {
		artist = t.Artist
	}
	if artist == "" {
		artist = "Unknown Artist"
	}
	album := t.Album
	if album == "" {
		album = "Unknown Album"
	}
	return AlbumIdentity{
		Artist:   artist,
		Album:    album,
		ArtistID: identifier("artist\x00" + artist),
		AlbumID:  identifier("album\x00" + artist + "\x00" + album),
	}
}

func (l *Library) Catalog(ctx context.Context) (Catalog, error) {
	c := Catalog{Artists: []Artist{}, Albums: []Album{}, Tracks: []Track{}}
	artistIndices := map[string]int{}
	albumIndices := map[string]int{}
	for offset := 0; ; offset += 500 {
		page, err := l.List(ctx, Query{Limit: 500, Offset: offset})
		if err != nil {
			return c, err
		}
		for _, track := range page.Tracks {
			group := track.AlbumIdentity()
			artist, name, artistID, albumID := group.Artist, group.Album, group.ArtistID, group.AlbumID
			if _, ok := artistIndices[artistID]; !ok {
				artistIndices[artistID] = len(c.Artists)
				c.Artists = append(c.Artists, Artist{ID: artistID, Name: artist, Albums: []Album{}})
			}
			index, ok := albumIndices[albumID]
			if !ok {
				index = len(c.Albums)
				albumIndices[albumID] = index
				c.Albums = append(c.Albums, Album{
					ID:        albumID,
					Name:      name,
					ArtistID:  artistID,
					Artist:    artist,
					Genre:     track.Genre,
					Year:      track.Year,
					CreatedAt: track.CreatedAt,
					Tracks:    []Track{},
				})
			}
			album := &c.Albums[index]
			album.Tracks = append(album.Tracks, track)
			album.Duration += track.Duration
			if album.CreatedAt.IsZero() || (!track.CreatedAt.IsZero() && track.CreatedAt.Before(album.CreatedAt)) {
				album.CreatedAt = track.CreatedAt
			}
			c.Tracks = append(c.Tracks, track)
		}
		if len(page.Tracks) < 500 {
			break
		}
	}
	for i := range c.Albums {
		album := &c.Albums[i]
		sort.SliceStable(album.Tracks, func(i, j int) bool {
			x, y := album.Tracks[i], album.Tracks[j]
			if x.DiscNumber != y.DiscNumber {
				return x.DiscNumber < y.DiscNumber
			}
			if x.TrackNumber != y.TrackNumber {
				return x.TrackNumber < y.TrackNumber
			}
			return x.Path < y.Path
		})
		index := artistIndices[album.ArtistID]
		c.Artists[index].Albums = append(c.Artists[index].Albums, *album)
	}
	sort.SliceStable(c.Artists, func(i, j int) bool { return strings.ToLower(c.Artists[i].Name) < strings.ToLower(c.Artists[j].Name) })
	sort.SliceStable(c.Albums, func(i, j int) bool {
		return strings.ToLower(c.Albums[i].Artist+"\x00"+c.Albums[i].Name) < strings.ToLower(c.Albums[j].Artist+"\x00"+c.Albums[j].Name)
	})
	return c, nil
}
