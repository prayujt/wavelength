package auth_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
)

func TestPermanentDeletionAcrossMCPConnectAndGRPCRequiresConfirmationAndScopes(t *testing.T) {
	ctx := context.Background()
	music := t.TempDir()
	for _, name := range []string{"one.wav", "two.wav", "three.wav", "four.wav", "five.wav"} {
		if err := os.WriteFile(filepath.Join(music, name), []byte("RIFF test audio"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	issuer := provider(t, "")
	s, db := app(t, issuer.URL, t.TempDir(), music)
	registration := register(t, s, "none")
	all := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeWrite+" "+auth.ScopeIngest, "alice")
	write := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeWrite, "alice")
	ingest := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeIngest, "alice")
	client := wavelengthv1connect.NewLibraryServiceClient(s.Client(), s.URL)
	page, err := client.ListTracks(ctx, rpcRequest(all.Access, &pb.ListTracksRequest{}))
	if err != nil || len(page.Msg.Tracks) != 5 {
		t.Fatal(page, err)
	}
	id := page.Msg.Tracks[0].Id
	for _, token := range []string{write.Access, ingest.Access} {
		_, err = client.DeleteTrack(ctx, rpcRequest(token, &pb.DeleteTrackRequest{Id: id, Confirm: true}))
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatal("missing scope was allowed", err)
		}
	}
	_, err = client.DeleteTrack(ctx, rpcRequest(all.Access, &pb.DeleteTrackRequest{Id: id}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("missing confirmation allowed", err)
	}
	session := mcpClient(t, s, all.Access)
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "delete_track" {
			found = true
			if tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Fatal("missing destructive annotation")
			}
		}
	}
	if !found {
		t.Fatal("delete_track not exposed")
	}
	tool(t, session, "delete_track", map[string]any{"id": id}, true)
	tool(t, mcpClient(t, s, write.Access), "delete_track", map[string]any{"id": id, "confirm": true}, true)
	assertRemoved := func(id string, path string) {
		t.Helper()
		if _, err := os.Stat(filepath.Join(music, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("audio remains", err)
		}
		if _, err := client.GetTrack(ctx, rpcRequest(all.Access, &pb.GetTrackRequest{Id: id})); connect.CodeOf(err) != connect.CodeNotFound {
			t.Fatal("record remains", err)
		}
	}
	var path string
	if err := db.QueryRow("SELECT path FROM tracks WHERE id=?", id).Scan(&path); err != nil {
		t.Fatal(err)
	}
	tool(t, session, "delete_track", map[string]any{"id": id, "confirm": true}, false)
	assertRemoved(id, path)
	for n, options := range [][]connect.ClientOption{nil, {connect.WithProtoJSON()}, {connect.WithGRPCWeb()}, {connect.WithGRPC()}} {
		id = page.Msg.Tracks[n+1].Id
		if err := db.QueryRow("SELECT path FROM tracks WHERE id=?", id).Scan(&path); err != nil {
			t.Fatal(err)
		}
		native := wavelengthv1connect.NewLibraryServiceClient(s.Client(), s.URL, options...)
		for retry := 0; retry < 2; retry++ {
			if _, err = native.DeleteTrack(ctx, rpcRequest(all.Access, &pb.DeleteTrackRequest{Id: id, Confirm: true})); err != nil {
				t.Fatal(err)
			}
		}
		assertRemoved(id, path)
	}
}
