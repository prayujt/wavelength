package rpc

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/listening"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type listeningService struct{ data *listening.Service }

func ListeningHandler(data *listening.Service, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewListeningServiceHandler(&listeningService{data}, connect.WithReadMaxBytes(1<<20))
	return path, protect(handler, manager)
}
func listeningSession(s listening.Session) *pb.ListeningSession {
	out := &pb.ListeningSession{Id: s.ID, Track: track(s.Track), ArtistId: s.ArtistID, AlbumId: s.AlbumID, Origin: s.Origin, Outcome: s.Outcome, StartedAt: timestamppb.New(s.StartedAt), UpdatedAt: timestamppb.New(s.UpdatedAt), ListenedMs: s.ListenedMS, PositionMs: s.PositionMS, Playing: s.Playing}
	if !s.EndedAt.IsZero() {
		out.EndedAt = timestamppb.New(s.EndedAt)
	}
	return out
}
func (s *listeningService) StartListeningSession(ctx context.Context, r *connect.Request[pb.StartListeningSessionRequest]) (*connect.Response[pb.ListeningSession], error) {
	return personal(ctx, true, func(owner string) (*pb.ListeningSession, error) {
		item, err := s.data.Start(ctx, owner, r.Msg.TrackId, r.Msg.Origin, r.Msg.RequestId)
		return listeningSession(item), err
	})
}
func (s *listeningService) ReportListeningProgress(ctx context.Context, r *connect.Request[pb.ReportListeningProgressRequest]) (*connect.Response[pb.ListeningSession], error) {
	return personal(ctx, true, func(owner string) (*pb.ListeningSession, error) {
		item, err := s.data.Report(ctx, owner, r.Msg.SessionId, listening.Progress{ListenedMS: r.Msg.ListenedMs, PositionMS: r.Msg.PositionMs, Playing: r.Msg.Playing})
		return listeningSession(item), err
	})
}
func (s *listeningService) FinishListeningSession(ctx context.Context, r *connect.Request[pb.FinishListeningSessionRequest]) (*connect.Response[pb.ListeningSession], error) {
	return personal(ctx, true, func(owner string) (*pb.ListeningSession, error) {
		item, err := s.data.Finish(ctx, owner, r.Msg.SessionId, r.Msg.Outcome, listening.Progress{ListenedMS: r.Msg.ListenedMs, PositionMS: r.Msg.PositionMs})
		return listeningSession(item), err
	})
}
func (s *listeningService) ListListeningSessions(ctx context.Context, r *connect.Request[pb.ListListeningSessionsRequest]) (*connect.Response[pb.ListListeningSessionsResponse], error) {
	return personal(ctx, false, func(owner string) (*pb.ListListeningSessionsResponse, error) {
		var since, until time.Time
		for i, stamp := range []*timestamppb.Timestamp{r.Msg.Since, r.Msg.Until} {
			if stamp == nil {
				continue
			}
			if stamp.CheckValid() != nil {
				return nil, listening.ErrInvalid
			}
			if i == 0 {
				since = stamp.AsTime()
			} else {
				until = stamp.AsTime()
			}
		}
		page, err := s.data.List(ctx, owner, listening.Query{TrackID: r.Msg.TrackId, ArtistID: r.Msg.ArtistId, AlbumID: r.Msg.AlbumId, Origin: r.Msg.Origin, Outcome: r.Msg.Outcome, Since: since, Until: until, Limit: int(r.Msg.Limit), Offset: int(r.Msg.Offset)})
		if err != nil {
			return nil, err
		}
		out := &pb.ListListeningSessionsResponse{Total: int32(page.Total)}
		for _, item := range page.Sessions {
			out.Sessions = append(out.Sessions, listeningSession(item))
		}
		return out, nil
	})
}
