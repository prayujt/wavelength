package rpc

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/internal/library"
)

func (s *service) ListTracks(ctx context.Context, r *connect.Request[pb.ListTracksRequest]) (*connect.Response[pb.ListTracksResponse], error) {
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
func (s *service) GetTrack(ctx context.Context, r *connect.Request[pb.GetTrackRequest]) (*connect.Response[pb.GetTrackResponse], error) {
	if r.Msg.Id == "" {
		return nil, invalid("id is required")
	}
	t, err := s.library.Get(ctx, r.Msg.Id)
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.GetTrackResponse{Track: track(t)}), nil
}
func (s *service) ListArtists(ctx context.Context, r *connect.Request[pb.ListArtistsRequest]) (*connect.Response[pb.ListArtistsResponse], error) {
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
func (s *service) GetArtist(ctx context.Context, r *connect.Request[pb.GetArtistRequest]) (*connect.Response[pb.GetArtistResponse], error) {
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
func (s *service) ListAlbums(ctx context.Context, r *connect.Request[pb.ListAlbumsRequest]) (*connect.Response[pb.ListAlbumsResponse], error) {
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
func (s *service) GetAlbum(ctx context.Context, r *connect.Request[pb.GetAlbumRequest]) (*connect.Response[pb.GetAlbumResponse], error) {
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
