package auth_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOfflineListeningReplayPreservesTimesAndCountsOnce(t *testing.T) {
	ctx := context.Background()
	server, db := oauthFixture(t)
	registration := register(t, server, "none")
	alice := grant(t, server, registration, auth.ScopeRead+" "+auth.ScopeWrite, "alice")
	library := wavelengthv1connect.NewLibraryServiceClient(server.Client(), server.URL)
	tracks, err := library.ListTracks(ctx, rpcRequest(alice.Access, &pb.ListTracksRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	client := wavelengthv1connect.NewListeningServiceClient(server.Client(), server.URL)
	started := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Millisecond)
	in := &pb.StartListeningSessionRequest{TrackId: tracks.Msg.Tracks[0].Id, Origin: "direct", RequestId: "offline-replay-1", OccurredAt: timestamppb.New(started), CountPlay: true}
	for range 3 {
		result, err := client.StartListeningSession(ctx, rpcRequest(alice.Access, in))
		if err != nil || !result.Msg.StartedAt.AsTime().Equal(started) {
			t.Fatal(result, err)
		}
	}
	finished := started.Add(8 * time.Second)
	finish := &pb.FinishListeningSessionRequest{SessionId: in.RequestId, Outcome: "completed", ListenedMs: 8000, PositionMs: 8000, OccurredAt: timestamppb.New(finished)}
	for range 2 {
		result, err := client.FinishListeningSession(ctx, rpcRequest(alice.Access, finish))
		if err != nil || !result.Msg.EndedAt.AsTime().Equal(finished) {
			t.Fatal(result, err)
		}
	}
	var count int
	var stamp string
	err = db.QueryRow("SELECT json_extract(value,'$.plays'),json_extract(value,'$.last_played') FROM app_state WHERE scope LIKE 'annotations/%' AND key=?", in.TrackId).Scan(&count, &stamp)
	if err != nil || count != 1 || stamp != started.Format(time.RFC3339Nano) {
		t.Fatal(count, stamp, err)
	}
	invalid := &pb.StartListeningSessionRequest{TrackId: in.TrackId, Origin: in.Origin, CountPlay: true}
	invalid.RequestId = "future-replay-1"
	invalid.OccurredAt = timestamppb.New(time.Now().Add(time.Hour))
	if _, err := client.StartListeningSession(ctx, rpcRequest(alice.Access, invalid)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("future event accepted", err)
	}
	invalid.OccurredAt = &timestamppb.Timestamp{Seconds: 1, Nanos: -1}
	if _, err := client.StartListeningSession(ctx, rpcRequest(alice.Access, invalid)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("malformed timestamp accepted", err)
	}
}
