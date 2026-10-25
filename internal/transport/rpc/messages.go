package rpc

import (
	"time"

	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/internal/library"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
func track(t library.Track) *pb.Track {
	group := t.AlbumIdentity()
	return &pb.Track{
		Id:              t.ID,
		ArtistId:        group.ArtistID,
		AlbumId:         group.AlbumID,
		Title:           t.Title,
		Artist:          t.Artist,
		Album:           t.Album,
		AlbumArtist:     t.AlbumArtist,
		TrackNumber:     int32(t.TrackNumber),
		DiscNumber:      int32(t.DiscNumber),
		Year:            int32(t.Year),
		Genre:           t.Genre,
		DurationSeconds: t.Duration,
		SizeBytes:       t.Size,
		ContentType:     t.ContentType,
		BitRateKbps:     int32(t.BitRate),
		CreatedAt:       timestamp(t.CreatedAt),
		ModifiedAt:      timestamp(t.ModifiedAt),
		ArtworkPath:     artworkPath(t.ID),
	}
}
func artist(a library.Artist) *pb.Artist {
	var artwork string
	for _, album := range a.Albums {
		if len(album.Tracks) > 0 {
			artwork = artworkPath(album.Tracks[0].ID)
			break
		}
	}
	return &pb.Artist{Id: a.ID, Name: a.Name, AlbumCount: int32(len(a.Albums)), ArtworkPath: artwork}
}
func album(a library.Album) *pb.Album {
	var artwork string
	if len(a.Tracks) > 0 {
		artwork = artworkPath(a.Tracks[0].ID)
	}
	return &pb.Album{
		Id:              a.ID,
		Name:            a.Name,
		ArtistId:        a.ArtistID,
		Artist:          a.Artist,
		Year:            int32(a.Year),
		Genre:           a.Genre,
		TrackCount:      int32(len(a.Tracks)),
		DurationSeconds: a.Duration,
		CreatedAt:       timestamp(a.CreatedAt),
		ArtworkPath:     artwork,
	}
}

func artworkPath(id string) string { return "/media/tracks/" + id + "/artwork" }
