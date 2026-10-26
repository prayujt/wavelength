package auth_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"google.golang.org/protobuf/proto"
)

func TestPlayFeedNativeAuthPaginationAndListeningAttribution(t *testing.T) {
	ctx := context.Background()
	server, db := oauthFixture(t)
	if _, err := db.Exec(`INSERT INTO tracks(id,root_id,path,title,content_type,size,modified_at,created_at,search_text) SELECT 'second-fixture',root_id,'second.wav','Second',content_type,size,modified_at,created_at,'second' FROM tracks LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	registration := register(t, server, "none")
	alice := grant(t, server, registration, auth.ScopeRead+" "+auth.ScopeWrite, "alice")
	bob := grant(t, server, registration, auth.ScopeRead+" "+auth.ScopeWrite, "bob")
	read := grant(t, server, registration, auth.ScopeRead, "alice")
	client := wavelengthv1connect.NewPlayFeedServiceClient(server.Client(), server.URL)
	grpc := wavelengthv1connect.NewPlayFeedServiceClient(server.Client(), server.URL, connect.WithGRPC())
	for _, token := range []string{"", "test-native"} {
		_, err := client.GetPlayFeed(ctx, rpcRequest(token, &pb.GetPlayFeedRequest{RequestId: "feed-test-first"}))
		want := connect.CodeUnauthenticated
		if connect.CodeOf(err) != want {
			t.Fatal("unexpected authorization result", connect.CodeOf(err), want)
		}
	}
	first, err := client.GetPlayFeed(ctx, rpcRequest(read.Access, &pb.GetPlayFeedRequest{RequestId: "feed-test-first", PageSize: 1}))
	if err != nil || len(first.Msg.GetItems()) != 1 || first.Msg.NextCursor == "" {
		t.Fatal(first, err)
	}
	retry, err := grpc.GetPlayFeed(ctx, rpcRequest(alice.Access, &pb.GetPlayFeedRequest{RequestId: "feed-test-first", PageSize: 1}))
	if err != nil || !proto.Equal(first.Msg, retry.Msg) {
		t.Fatal("gRPC/Connect retry changed", retry, err)
	}
	_, err = client.GetPlayFeed(ctx, rpcRequest(bob.Access, &pb.GetPlayFeedRequest{RequestId: "feed-other-owner", Cursor: first.Msg.NextCursor, PageSize: 1}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("cursor crossed owners", err)
	}
	_, err = client.GetPlayFeed(ctx, rpcRequest(read.Access, &pb.GetPlayFeedRequest{RequestId: "feed-test-first", PageSize: 2}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatal("conflicting retry accepted", err)
	}
	listening := wavelengthv1connect.NewListeningServiceClient(server.Client(), server.URL)
	item := first.Msg.Items[0]
	start, err := listening.StartListeningSession(ctx, rpcRequest(alice.Access, &pb.StartListeningSessionRequest{RequestId: "feed-listen-alice", TrackId: item.Track.Id, Origin: "queue", PlayFeedItemId: item.Id}))
	if err != nil || start.Msg.PlayFeedItemId != item.Id {
		t.Fatal(start, err)
	}
	other, err := listening.StartListeningSession(ctx, rpcRequest(bob.Access, &pb.StartListeningSessionRequest{RequestId: "feed-listen-bob", TrackId: item.Track.Id, Origin: "queue", PlayFeedItemId: item.Id}))
	if err != nil || other.Msg.PlayFeedItemId != "" {
		t.Fatal("foreign attribution accepted", other, err)
	}
	finish, err := listening.FinishListeningSession(ctx, rpcRequest(alice.Access, &pb.FinishListeningSessionRequest{SessionId: start.Msg.Id, Outcome: "skipped", ChangeReason: "next", ListenedMs: 1000}))
	if err != nil || finish.Msg.ChangeReason != "next" || finish.Msg.PlayFeedItemId != item.Id {
		t.Fatal(finish, err)
	}
	_, err = listening.FinishListeningSession(ctx, rpcRequest(alice.Access, &pb.FinishListeningSessionRequest{SessionId: start.Msg.Id, Outcome: "skipped", ChangeReason: "selection", ListenedMs: 1000}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatal("finished reason changed", err)
	}
	_, err = listening.FinishListeningSession(ctx, rpcRequest(bob.Access, &pb.FinishListeningSessionRequest{SessionId: other.Msg.Id, Outcome: "stopped", ChangeReason: "next"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid outcome/reason accepted", err)
	}
	user := wavelengthv1connect.NewUserDataServiceClient(server.Client(), server.URL)
	saved, err := user.SaveQueue(ctx, rpcRequest(alice.Access, &pb.SaveQueueRequest{TrackIds: []string{item.Track.Id, item.Track.Id}, CurrentTrackId: item.Track.Id, Recommendations: []*pb.QueueRecommendation{{Index: 1, PlayFeedItemId: item.Id}}, PlayFeedEnabled: proto.Bool(false)}))
	if err != nil || len(saved.Msg.GetQueue().GetRecommendations()) != 1 || saved.Msg.GetQueue().PlayFeedEnabled == nil || saved.Msg.GetQueue().GetPlayFeedEnabled() {
		t.Fatal(saved, err)
	}
	restored, err := user.GetQueue(ctx, rpcRequest(alice.Access, &pb.GetQueueRequest{}))
	if err != nil || !proto.Equal(saved.Msg.Queue, restored.Msg.Queue) {
		t.Fatal("queue lost feed metadata", restored, err)
	}
	_, err = user.SaveQueue(ctx, rpcRequest(alice.Access, &pb.SaveQueueRequest{TrackIds: []string{item.Track.Id}, Recommendations: []*pb.QueueRecommendation{{Index: 5, PlayFeedItemId: item.Id}}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid queue reference accepted", err)
	}
}
