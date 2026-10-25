package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestListeningSessionsAcrossMCPConnectAndGRPC(t *testing.T) {
	ctx := context.Background()
	server, db := oauthFixture(t)
	if _, err := db.Exec("UPDATE tracks SET artist='The Morning',album='Small Hours',genre='Folk',duration=180"); err != nil {
		t.Fatal(err)
	}
	registration := register(t, server, "none")
	alice := grant(t, server, registration, auth.ScopeRead+" "+auth.ScopeWrite, "alice")
	bob := grant(t, server, registration, auth.ScopeRead+" "+auth.ScopeWrite, "bob")
	read := grant(t, server, registration, auth.ScopeRead, "alice")
	client := wavelengthv1connect.NewListeningServiceClient(server.Client(), server.URL)
	grpc := wavelengthv1connect.NewListeningServiceClient(server.Client(), server.URL, connect.WithGRPC())
	lib := wavelengthv1connect.NewLibraryServiceClient(server.Client(), server.URL)
	tracks, err := lib.ListTracks(ctx, rpcRequest(alice.Access, &pb.ListTracksRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	track := tracks.Msg.Tracks[0]
	in := &pb.StartListeningSessionRequest{TrackId: track.Id, Origin: "direct", RequestId: "listen-session-1"}
	for _, token := range []string{"", "test-native", read.Access} {
		_, err := client.StartListeningSession(ctx, rpcRequest(token, in))
		want := connect.CodeUnauthenticated
		if token == read.Access {
			want = connect.CodePermissionDenied
		}
		if connect.CodeOf(err) != want {
			t.Fatal("invalid caller started session", token, err)
		}
	}
	first, err := grpc.StartListeningSession(ctx, rpcRequest(alice.Access, in))
	if err != nil {
		t.Fatal(err)
	}
	retry, err := client.StartListeningSession(ctx, rpcRequest(alice.Access, in))
	if err != nil || !retry.Msg.StartedAt.AsTime().Equal(first.Msg.StartedAt.AsTime()) {
		t.Fatal("retry duplicated session", retry, err)
	}
	_, err = client.StartListeningSession(ctx, rpcRequest(alice.Access, &pb.StartListeningSessionRequest{TrackId: track.Id, Origin: "queue", RequestId: in.RequestId}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatal("conflicting request accepted", err)
	}
	mcp := mcpClient(t, server, alice.Access)
	tool(t, mcp, "report_listening_progress", map[string]any{"session_id": in.RequestId, "listened_ms": 30000, "position_ms": 90000, "playing": true}, false)
	// Parallel delayed updates cannot roll cumulative time backwards.
	var wg sync.WaitGroup
	for _, ms := range []int64{1000, 35000, 32000, 45000, 40000} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.ReportListeningProgress(ctx, rpcRequest(alice.Access, &pb.ReportListeningProgressRequest{SessionId: in.RequestId, ListenedMs: ms, PositionMs: ms, Playing: true}))
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	done, err := grpc.FinishListeningSession(ctx, rpcRequest(alice.Access, &pb.FinishListeningSessionRequest{SessionId: in.RequestId, Outcome: "skipped", ListenedMs: 45000, PositionMs: 120000}))
	if err != nil || done.Msg.ListenedMs != 45000 || done.Msg.Outcome != "skipped" || done.Msg.Playing || done.Msg.EndedAt == nil {
		t.Fatal(done, err)
	}
	tool(t, mcp, "report_listening_progress", map[string]any{"session_id": in.RequestId, "listened_ms": 99000, "position_ms": 150000, "playing": true}, false)
	final, err := client.FinishListeningSession(ctx, rpcRequest(alice.Access, &pb.FinishListeningSessionRequest{SessionId: in.RequestId, Outcome: "skipped", ListenedMs: 45000, PositionMs: 120000}))
	if err != nil || final.Msg.ListenedMs != 45000 || !final.Msg.EndedAt.AsTime().Equal(done.Msg.EndedAt.AsTime()) {
		t.Fatal("finished session changed", final, err)
	}
	_, err = client.FinishListeningSession(ctx, rpcRequest(alice.Access, &pb.FinishListeningSessionRequest{SessionId: in.RequestId, Outcome: "completed"}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatal(err)
	}
	_, err = client.ReportListeningProgress(ctx, rpcRequest(bob.Access, &pb.ReportListeningProgressRequest{SessionId: in.RequestId}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatal("other listener updated session", err)
	}
	tool(t, mcpClient(t, server, read.Access), "start_listening_session", map[string]any{"track_id": track.Id, "origin": "direct", "request_id": "readonly-session"}, true)
	tool(t, mcp, "start_listening_session", map[string]any{"track_id": track.Id, "origin": "queue", "request_id": "listen-session-2"}, false)
	tool(t, mcp, "finish_listening_session", map[string]any{"session_id": "listen-session-2", "outcome": "completed", "listened_ms": 180000, "position_ms": 180000}, false)
	tool(t, mcp, "start_listening_session", map[string]any{"track_id": track.Id, "origin": "restored", "request_id": "listen-session-3"}, false)
	// History keeps its original metadata and catalog identities after an edit.
	if _, err := db.Exec("UPDATE tracks SET artist='Changed',album='Changed',genre='Rock'"); err != nil {
		t.Fatal(err)
	}
	query := &pb.ListListeningSessionsRequest{TrackId: track.Id, ArtistId: track.ArtistId, AlbumId: track.AlbumId, Since: timestamppb.New(first.Msg.StartedAt.AsTime().Add(-time.Second)), Until: timestamppb.New(time.Now().Add(time.Second)), Limit: 1}
	page, err := client.ListListeningSessions(ctx, rpcRequest(read.Access, query))
	if err != nil || page.Msg.Total != 3 || len(page.Msg.Sessions) != 1 || page.Msg.Sessions[0].Id != "listen-session-3" || page.Msg.Sessions[0].Outcome != "" {
		t.Fatal(page, err)
	}
	if page.Msg.Sessions[0].Track.Artist != "The Morning" || page.Msg.Sessions[0].Track.Genre != "Folk" {
		t.Fatal("snapshot changed", page.Msg)
	}
	query.Offset = 1
	page, err = grpc.ListListeningSessions(ctx, rpcRequest(alice.Access, query))
	if err != nil || page.Msg.Total != 3 || page.Msg.Sessions[0].Id != "listen-session-2" {
		t.Fatal(page, err)
	}
	result := tool(t, mcp, "list_listening_sessions", map[string]any{"artist_id": track.ArtistId, "album_id": track.AlbumId, "outcome": "skipped", "origin": "direct"}, false)
	if result["total"] != float64(1) {
		t.Fatal(result)
	}
	query.AlbumId = "unknown"
	page, err = client.ListListeningSessions(ctx, rpcRequest(alice.Access, query))
	if err != nil || page.Msg.Total != 0 {
		t.Fatal("album filter ignored", page, err)
	}
	other, err := client.ListListeningSessions(ctx, rpcRequest(bob.Access, &pb.ListListeningSessionsRequest{}))
	if err != nil || other.Msg.Total != 0 {
		t.Fatal("other listener read history", other, err)
	}
	for _, invalid := range []*pb.ReportListeningProgressRequest{{SessionId: in.RequestId, ListenedMs: -1}, {SessionId: in.RequestId, PositionMs: -1}, {SessionId: in.RequestId, ListenedMs: 1 << 54}, {SessionId: "bad"}} {
		if _, err := client.ReportListeningProgress(ctx, rpcRequest(alice.Access, invalid)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal("invalid progress accepted", err)
		}
	}
	query = &pb.ListListeningSessionsRequest{Since: timestamppb.New(time.Now()), Until: timestamppb.New(time.Now().Add(-time.Hour))}
	if _, err := client.ListListeningSessions(ctx, rpcRequest(alice.Access, query)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid range accepted", err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM listening_sessions").Scan(&count); err != nil || count != 3 {
		t.Fatal("unexpected duplicate records", count, err)
	}
}
