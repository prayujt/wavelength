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
	return &pb.Track{
		Id:              t.ID,
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
	}
}
func artist(a library.Artist) *pb.Artist {
	return &pb.Artist{Id: a.ID, Name: a.Name, AlbumCount: int32(len(a.Albums))}
}
func album(a library.Album) *pb.Album {
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
	}
}
