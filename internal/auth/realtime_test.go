package auth_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
)

func TestRealtimeOwnedProgressAndSharedPublicationsAcrossProtocols(t *testing.T) {
	s := acquisitionFixture(t, &ingestSource{audio: []byte("fixture audio")})
	registration := register(t, s, "none")
	alice := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeIngest, "subject-1")
	aliceRead := grant(t, s, registration, auth.ScopeRead, "subject-1")
	bob := grant(t, s, registration, auth.ScopeRead, "subject-2")
	for _, test := range []struct {
		name    string
		options []connect.ClientOption
	}{
		{"connect", nil}, {"json", []connect.ClientOption{connect.WithProtoJSON()}},
		{"grpc-web", []connect.ClientOption{connect.WithGRPCWeb()}}, {"grpc", []connect.ClientOption{connect.WithGRPC()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client := wavelengthv1connect.NewRealtimeServiceClient(s.Client(), s.URL, test.options...)
			streams := make([]*connect.ServerStreamForClient[pb.RealtimeEvent], 0, 2)
			for _, token := range []string{aliceRead.Access, bob.Access} {
				stream, err := client.Subscribe(ctx, rpcRequest(token, &pb.SubscribeRequest{}))
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				if !stream.Receive() || stream.Msg().GetReady() == nil {
					t.Fatal("missing ready", stream.Err())
				}
				streams = append(streams, stream)
			}
			imports := wavelengthv1connect.NewIngestionServiceClient(s.Client(), s.URL, test.options...)
			job, err := imports.CreateImport(ctx, rpcRequest(alice.Access, &pb.CreateImportRequest{RequestId: "stream-" + test.name, Items: []*pb.ImportSelection{{MetadataRef: &pb.ProviderRef{Provider: "itunes", Id: "123"}, SourceRef: &pb.ProviderRef{Provider: "youtube", Id: "BaW_jenozKc"}}}}))
			if err != nil {
				t.Fatal(err)
			}
			complete, published := false, false
			for !(complete && published) && streams[0].Receive() {
				event := streams[0].Msg()
				if update := event.GetImportUpdated(); update != nil {
					if update.Id != job.Msg.Id {
						t.Fatalf("unexpected job %s", update.Id)
					}
					complete = update.State == "completed"
				}
				if update := event.GetLibraryUpdated(); update != nil {
					published = update.Track.GetTrackId() != "" && update.Track.GetMetadataRef().GetId() == "123"
				}
			}
			if !complete || !published {
				t.Fatal("missing committed updates", streams[0].Err())
			}
			if !streams[1].Receive() || streams[1].Msg().GetLibraryUpdated() == nil {
				t.Fatal("private job leaked or shared event missing", streams[1].Err())
			}
			snapshot, err := imports.GetImport(ctx, rpcRequest(alice.Access, &pb.GetImportRequest{Id: job.Msg.Id}))
			if err != nil || snapshot.Msg.State != "completed" {
				t.Fatal("event preceded durable commit", snapshot, err)
			}
			if snapshot.Msg.Items[0].AlbumId == "" || snapshot.Msg.Items[0].ArtistId == "" || snapshot.Msg.Items[0].Metadata == nil {
				t.Fatal("completed item has no catalog destination or display metadata", snapshot.Msg.Items[0])
			}
			for _, token := range []string{"", "not-a-valid-token", "test-native"} {
				stream, err := client.Subscribe(ctx, rpcRequest(token, &pb.SubscribeRequest{}))
				if err == nil {
					stream.Receive()
					err = stream.Err()
					stream.Close()
				}
				if connect.CodeOf(err) != connect.CodeUnauthenticated {
					t.Fatalf("unauthorized stream: %v", err)
				}
			}

		})
	}
}
