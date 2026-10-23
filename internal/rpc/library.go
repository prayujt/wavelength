// Package rpc adapts the library application services to versioned protobuf RPCs.
package rpc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/library"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct{ library *library.Library }

var _ wavelengthv1connect.LibraryServiceHandler = (*Service)(nil)

func New(lib *library.Library) *Service { return &Service{library: lib} }
func Handler(lib *library.Library, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewLibraryServiceHandler(New(lib))
	writer := connect.NewErrorWriter()
	return path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, err := manager.Authorize(r)
		if err != nil {
			code := connect.CodeInternal
			var failure *auth.RequestError
			if errors.As(err, &failure) {
				switch failure.StatusCode {
				case 401:
					code = connect.CodeUnauthenticated
				case 403:
					code = connect.CodePermissionDenied
				}
			}
			_ = writer.Write(w, r, connect.NewError(code, err))
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
func failure(err error) error {
	if errors.Is(err, library.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, errors.New("media not found"))
	}
	if errors.Is(err, library.ErrScanRunning) {
		return connect.NewError(connect.CodeAlreadyExists, err)
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	return connect.NewError(connect.CodeInternal, errors.New("library operation failed"))
}
func invalid(message string) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New(message))
}
func pagination(limit, offset int32) (int, int, error) {
	if limit < 0 || limit > 500 || offset < 0 {
		return 0, 0, invalid("limit must be 0..500 and offset must be nonnegative")
	}
	if limit == 0 {
		limit = 100
	}
	return int(limit), int(offset), nil
}
func slice[T any](items []T, offset, count int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + count
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
func track(t library.Track) *pb.Track {
	return &pb.Track{Id: t.ID, Title: t.Title, Artist: t.Artist, Album: t.Album, AlbumArtist: t.AlbumArtist, TrackNumber: int32(t.TrackNumber), DiscNumber: int32(t.DiscNumber), Year: int32(t.Year), Genre: t.Genre, DurationSeconds: t.Duration, SizeBytes: t.Size, ContentType: t.ContentType, BitRateKbps: int32(t.BitRate), CreatedAt: timestamp(t.CreatedAt), ModifiedAt: timestamp(t.ModifiedAt)}
}
func artist(a library.Artist) *pb.Artist {
	return &pb.Artist{Id: a.ID, Name: a.Name, AlbumCount: int32(len(a.Albums))}
}
func album(a library.Album) *pb.Album {
	return &pb.Album{Id: a.ID, Name: a.Name, ArtistId: a.ArtistID, Artist: a.Artist, Year: int32(a.Year), Genre: a.Genre, TrackCount: int32(len(a.Tracks)), DurationSeconds: a.Duration, CreatedAt: timestamp(a.CreatedAt)}
}
func (s *Service) ListTracks(ctx context.Context, r *connect.Request[pb.ListTracksRequest]) (*connect.Response[pb.ListTracksResponse], error) {
	limit, offset, err := pagination(r.Msg.Limit, r.Msg.Offset)
	if err != nil {
		return nil, err
	}
	page, err := s.library.List(ctx, library.Query{Search: r.Msg.Query, Limit: limit, Offset: offset})
	if err != nil {
		return nil, failure(err)
	}
	out := &pb.ListTracksResponse{Total: int32(page.Total)}
	for _, t := range page.Tracks {
		out.Tracks = append(out.Tracks, track(t))
	}
	return connect.NewResponse(out), nil
}
func (s *Service) GetTrack(ctx context.Context, r *connect.Request[pb.GetTrackRequest]) (*connect.Response[pb.GetTrackResponse], error) {
	if r.Msg.Id == "" {
		return nil, invalid("id is required")
	}
	t, err := s.library.Get(ctx, r.Msg.Id)
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.GetTrackResponse{Track: track(t)}), nil
}
func (s *Service) ListArtists(ctx context.Context, r *connect.Request[pb.ListArtistsRequest]) (*connect.Response[pb.ListArtistsResponse], error) {
	limit, offset, err := pagination(r.Msg.Limit, r.Msg.Offset)
	if err != nil {
		return nil, err
	}
	c, err := s.library.Catalog(ctx)
	if err != nil {
		return nil, failure(err)
	}
	items := []*pb.Artist{}
	for _, a := range c.Artists {
		if strings.Contains(strings.ToLower(a.Name), strings.ToLower(r.Msg.Query)) {
			items = append(items, artist(a))
		}
	}
	return connect.NewResponse(&pb.ListArtistsResponse{Artists: slice(items, offset, limit), Total: int32(len(items))}), nil
}
func (s *Service) GetArtist(ctx context.Context, r *connect.Request[pb.GetArtistRequest]) (*connect.Response[pb.GetArtistResponse], error) {
	if r.Msg.Id == "" {
		return nil, invalid("id is required")
	}
	c, err := s.library.Catalog(ctx)
	if err != nil {
		return nil, failure(err)
	}
	for _, a := range c.Artists {
		if a.ID == r.Msg.Id {
			out := &pb.GetArtistResponse{Artist: artist(a)}
			for _, item := range a.Albums {
				out.Albums = append(out.Albums, album(item))
			}
			return connect.NewResponse(out), nil
		}
	}
	return nil, failure(library.ErrNotFound)
}
func (s *Service) ListAlbums(ctx context.Context, r *connect.Request[pb.ListAlbumsRequest]) (*connect.Response[pb.ListAlbumsResponse], error) {
	limit, offset, err := pagination(r.Msg.Limit, r.Msg.Offset)
	if err != nil {
		return nil, err
	}
	c, err := s.library.Catalog(ctx)
	if err != nil {
		return nil, failure(err)
	}
	items := []*pb.Album{}
	for _, a := range c.Albums {
		if r.Msg.ArtistId != "" && r.Msg.ArtistId != a.ArtistID {
			continue
		}
		if strings.Contains(strings.ToLower(a.Name+" "+a.Artist), strings.ToLower(r.Msg.Query)) {
			items = append(items, album(a))
		}
	}
	return connect.NewResponse(&pb.ListAlbumsResponse{Albums: slice(items, offset, limit), Total: int32(len(items))}), nil
}
func (s *Service) GetAlbum(ctx context.Context, r *connect.Request[pb.GetAlbumRequest]) (*connect.Response[pb.GetAlbumResponse], error) {
	if r.Msg.Id == "" {
		return nil, invalid("id is required")
	}
	c, err := s.library.Catalog(ctx)
	if err != nil {
		return nil, failure(err)
	}
	for _, a := range c.Albums {
		if a.ID == r.Msg.Id {
			out := &pb.GetAlbumResponse{Album: album(a)}
			for _, t := range a.Tracks {
				out.Tracks = append(out.Tracks, track(t))
			}
			return connect.NewResponse(out), nil
		}
	}
	return nil, failure(library.ErrNotFound)
}
func (s *Service) StartScan(ctx context.Context, r *connect.Request[pb.StartScanRequest]) (*connect.Response[pb.StartScanResponse], error) {
	result, err := s.library.Scan(ctx)
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.StartScanResponse{Indexed: int32(result.Indexed), MetadataWarnings: int32(result.MetadataWarnings)}), nil
}
func (s *Service) GetScanStatus(ctx context.Context, r *connect.Request[pb.GetScanStatusRequest]) (*connect.Response[pb.GetScanStatusResponse], error) {
	scanning, count := s.library.ScanStatus()
	return connect.NewResponse(&pb.GetScanStatusResponse{Scanning: scanning, Indexed: count}), nil
}
func (s *Service) GetPlaybackInfo(ctx context.Context, r *connect.Request[pb.GetPlaybackInfoRequest]) (*connect.Response[pb.GetPlaybackInfoResponse], error) {
	if r.Msg.Id == "" {
		return nil, invalid("id is required")
	}
	t, err := s.library.Get(ctx, r.Msg.Id)
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.GetPlaybackInfoResponse{Track: track(t), HttpPath: "/media/tracks/" + t.ID, SupportsRanges: true}), nil
}
func (s *Service) StreamAudio(ctx context.Context, r *connect.Request[pb.StreamAudioRequest], stream *connect.ServerStream[pb.StreamAudioResponse]) error {
	if r.Msg.Id == "" || r.Msg.OffsetBytes < 0 || r.Msg.LengthBytes < 0 {
		return invalid("id and nonnegative byte offset/length are required")
	}
	file, _, err := s.library.OpenTrack(ctx, r.Msg.Id)
	if err != nil {
		return failure(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return failure(err)
	}
	if r.Msg.OffsetBytes > info.Size() {
		return connect.NewError(connect.CodeOutOfRange, errors.New("offset exceeds file size"))
	}
	if _, err = file.Seek(r.Msg.OffsetBytes, io.SeekStart); err != nil {
		return failure(err)
	}
	remaining := info.Size() - r.Msg.OffsetBytes
	if r.Msg.LengthBytes > 0 && r.Msg.LengthBytes < remaining {
		remaining = r.Msg.LengthBytes
	}
	offset := r.Msg.OffsetBytes
	buffer := make([]byte, 64<<10)
	for remaining > 0 {
		if err = ctx.Err(); err != nil {
			return failure(err)
		}
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		n, err := io.ReadFull(file, buffer[:chunk])
		if err != nil {
			return failure(err)
		}
		if err = stream.Send(&pb.StreamAudioResponse{Data: append([]byte(nil), buffer[:n]...), OffsetBytes: offset}); err != nil {
			return err
		}
		offset += int64(n)
		remaining -= int64(n)
	}
	return nil
}
