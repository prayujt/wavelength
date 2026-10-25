package rpc

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/sharing"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sharingService struct{ links *sharing.Service }

func SharingHandler(links *sharing.Service, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewSharingServiceHandler(&sharingService{links: links}, connect.WithReadMaxBytes(1<<20))
	return path, protect(handler, manager)
}
func accessLink(g sharing.Grant) *pb.AccessLink {
	out := &pb.AccessLink{Id: g.ID, Kind: g.Kind, Name: g.Name, TrackIds: g.TrackIDs, CreatedAt: timestamppb.New(g.CreatedAt)}
	if !g.ExpiresAt.IsZero() {
		out.ExpiresAt = timestamppb.New(g.ExpiresAt)
	}

	return out
}
func (s *sharingService) CreatePlaybackLink(ctx context.Context, r *connect.Request[pb.CreatePlaybackLinkRequest]) (*connect.Response[pb.CreateAccessLinkResponse], error) {
	return personal(ctx, false, func(owner string) (*pb.CreateAccessLinkResponse, error) {
		if err := auth.RequireScope(ctx, auth.ScopeStream); err != nil {
			return nil, err
		}
		link, err := s.links.CreatePlayback(ctx, owner, r.Msg.TrackId, time.Duration(r.Msg.ExpiresInSeconds)*time.Second)
		return &pb.CreateAccessLinkResponse{Link: accessLink(link.Grant), Url: link.URL}, err
	})
}
func (s *sharingService) CreateShare(ctx context.Context, r *connect.Request[pb.CreateShareRequest]) (*connect.Response[pb.CreateAccessLinkResponse], error) {
	return personal(ctx, true, func(owner string) (*pb.CreateAccessLinkResponse, error) {
		if err := auth.RequireScope(ctx, auth.ScopeStream); err != nil {
			return nil, err
		}
		var expires time.Time
		if r.Msg.ExpiresAt != nil {
			if err := r.Msg.ExpiresAt.CheckValid(); err != nil {
				return nil, sharing.ErrInvalid
			}
			expires = r.Msg.ExpiresAt.AsTime()
		}
		link, err := s.links.CreateShare(ctx, owner, sharing.ShareRequest{Name: r.Msg.Name, TrackIDs: r.Msg.TrackIds, ExpiresAt: expires, NoExpiry: r.Msg.NoExpiry})
		return &pb.CreateAccessLinkResponse{Link: accessLink(link.Grant), Url: link.URL}, err
	})
}
func (s *sharingService) ListAccessLinks(ctx context.Context, r *connect.Request[pb.ListAccessLinksRequest]) (*connect.Response[pb.ListAccessLinksResponse], error) {
	return personal(ctx, false, func(owner string) (*pb.ListAccessLinksResponse, error) {
		kind, limit := r.Msg.Kind, int(r.Msg.Limit)
		if kind == "" {
			kind = sharing.Share
		}
		if limit == 0 {
			limit = 100
		}
		grants, err := s.links.List(ctx, owner, kind, limit, int(r.Msg.Offset))
		if err != nil {
			return nil, err
		}
		out := &pb.ListAccessLinksResponse{}
		for _, g := range grants {
			out.Links = append(out.Links, accessLink(g))
		}
		return out, nil
	})
}
func (s *sharingService) RevokeAccessLink(ctx context.Context, r *connect.Request[pb.RevokeAccessLinkRequest]) (*connect.Response[pb.RevokeAccessLinkResponse], error) {
	return personal(ctx, true, func(owner string) (*pb.RevokeAccessLinkResponse, error) {
		return &pb.RevokeAccessLinkResponse{}, s.links.Revoke(ctx, owner, r.Msg.Id)
	})
}
