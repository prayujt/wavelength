package rpc

import (
	"connectrpc.com/connect"
	"context"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/library"
)

func (s *service) DeleteTrack(ctx context.Context, r *connect.Request[pb.DeleteTrackRequest]) (*connect.Response[pb.EmptyResponse], error) {
	if _, err := auth.PersonalID(ctx, true); err != nil {
		return nil, failure(err)
	}
	if err := auth.RequireScope(ctx, auth.ScopeIngest); err != nil {
		return nil, failure(err)
	}
	if !r.Msg.Confirm {
		return nil, failure(library.ErrInvalid)
	}
	if err := s.library.Delete(ctx, r.Msg.Id); err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.EmptyResponse{}), nil
}
