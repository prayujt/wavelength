package rpc

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/playfeed"
)

type playFeedService struct{ service *playfeed.Service }

func PlayFeedHandler(service *playfeed.Service, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewPlayFeedServiceHandler(&playFeedService{service}, connect.WithReadMaxBytes(1<<20))
	return path, protect(handler, manager)
}
func (s *playFeedService) GetPlayFeed(ctx context.Context, r *connect.Request[pb.GetPlayFeedRequest]) (*connect.Response[pb.GetPlayFeedResponse], error) {
	return personal(ctx, false, func(owner string) (*pb.GetPlayFeedResponse, error) {
		page, err := s.service.Get(ctx, owner, playfeed.Request{RequestID: r.Msg.RequestId, Cursor: r.Msg.Cursor, PageSize: int(r.Msg.PageSize), SeedTrackID: r.Msg.SeedTrackId, QueuedTrackIDs: r.Msg.QueuedTrackIds, ExcludedTrackIDs: r.Msg.ExcludedTrackIds})
		if err != nil {
			return nil, err
		}
		out := &pb.GetPlayFeedResponse{FeedId: page.FeedID, NextCursor: page.NextCursor, Exhausted: page.Exhausted}
		for _, item := range page.Items {
			out.Items = append(out.Items, &pb.PlayFeedItem{Id: item.ID, Track: track(item.Track)})
		}
		return out, nil
	})
}
