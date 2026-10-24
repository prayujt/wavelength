package subsonic

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

func asSong(t library.Track) song {
	group := t.AlbumIdentity()
	created := t.CreatedAt
	if created.IsZero() {
		created = t.ModifiedAt
	}
	kind := "music"
	isVideo := strings.HasPrefix(t.ContentType, "video/")
	if isVideo {
		kind = "video"
	}
	return song{
		IsVideo:     isVideo,
		Path:        t.Path,
		Duration:    int(t.Duration + 0.5),
		Genre:       t.Genre,
		BitRate:     t.BitRate,
		DiscNumber:  t.DiscNumber,
		Created:     created.UTC().Format(time.RFC3339),
		CoverArt:    t.ID,
		ID:          t.ID,
		Parent:      directoryID(filepath.ToSlash(filepath.Dir(t.Path))),
		Title:       t.Title,
		Artist:      t.Artist,
		Album:       group.Album,
		ArtistID:    group.ArtistID,
		AlbumID:     group.AlbumID,
		Track:       t.TrackNumber,
		Year:        t.Year,
		Size:        t.Size,
		ContentType: t.ContentType,
		Suffix:      strings.TrimPrefix(strings.ToLower(filepath.Ext(t.Path)), "."),
		Type:        kind,
	}
}
func catalog(ctx context.Context, lib *library.Library) ([]artist, error) {
	snapshot, err := lib.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	out := []artist{}
	for _, entry := range snapshot.Artists {
		art := artist{ID: entry.ID, Name: entry.Name, AlbumCount: len(entry.Albums), Albums: []album{}}
		for _, source := range entry.Albums {
			value := album{
				ID:        source.ID,
				Name:      source.Name,
				Artist:    source.Artist,
				ArtistID:  source.ArtistID,
				Year:      source.Year,
				Genre:     source.Genre,
				Created:   source.CreatedAt.UTC().Format(time.RFC3339),
				SongCount: len(source.Tracks),
				Duration:  int(source.Duration + 0.5),
				Songs:     []song{},
			}
			for _, track := range source.Tracks {
				value.Songs = append(value.Songs, asSong(track))
			}
			if len(value.Songs) > 0 {
				value.CoverArt = value.Songs[0].ID
				art.CoverArt = value.CoverArt
			}
			art.Albums = append(art.Albums, value)
		}
		out = append(out, art)
	}
	return out, nil
}
