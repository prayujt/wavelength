package rpc

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/realtime"
)

type realtimeService struct{ ingest *ingest.Service }

func RealtimeHandler(imports *ingest.Service, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewRealtimeServiceHandler(&realtimeService{ingest: imports}, connect.WithReadMaxBytes(1<<20))
	return path, protect(handler, manager)
}

func (s *realtimeService) Subscribe(ctx context.Context, _ *connect.Request[pb.SubscribeRequest], stream *connect.ServerStream[pb.RealtimeEvent]) error {
	if err := auth.RequireScope(ctx, auth.ScopeRead); err != nil {
		return failure(err)
	}
	owner, err := auth.PersonalID(ctx, false)
	if err != nil {
		return failure(err)
	}
	// Reconnect periodically so a long-lived subscription is reauthorized.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	events, unsubscribe := s.ingest.Events().Subscribe(owner)
	defer unsubscribe()
	stream.ResponseHeader().Set("X-Accel-Buffering", "no")
	if err := stream.Send(&pb.RealtimeEvent{Payload: &pb.RealtimeEvent_Ready{Ready: &pb.StreamReady{}}}); err != nil {
		return err
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		var event pb.RealtimeEvent
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-heartbeat.C:
			event.Payload = &pb.RealtimeEvent_Heartbeat{Heartbeat: &pb.StreamHeartbeat{}}
		case change, ok := <-events:
			if !ok {
				return connect.NewError(connect.CodeResourceExhausted, errors.New("reconnect and refresh state"))
			}
			switch change := change.Change.(type) {
			case realtime.ImportChanged:
				job, err := s.ingest.Get(ctx, owner, change.ID)
				if err != nil {
					return failure(err)
				}
				event.Payload = &pb.RealtimeEvent_ImportUpdated{ImportUpdated: importJob(job)}
			case realtime.TrackDeleted:
				event.Payload = &pb.RealtimeEvent_LibraryUpdated{LibraryUpdated: &pb.LibraryUpdate{DeletedTrackId: change.TrackID}}
			case realtime.TrackPublished:
				event.Payload = &pb.RealtimeEvent_LibraryUpdated{LibraryUpdated: &pb.LibraryUpdate{Track: &pb.ImportedTrack{MetadataRef: refMessage(change.Ref), TrackId: change.TrackID}}}
			default:
				continue
			}
		}
		if err := stream.Send(&event); err != nil {
			return err
		}
	}
}
