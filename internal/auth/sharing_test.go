package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPlaybackAndSharingAcrossRPCMCPAndHTTP(t *testing.T) {
	ctx := context.Background()
	music := t.TempDir()
	const audio = "0123456789abcdefghijklmnopqrstuvwxyz"
	for _, name := range []string{"one.mp3", "two.mp3"} {
		if err := os.WriteFile(filepath.Join(music, name), []byte(audio), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// The public page is served from the same built app, without requiring a session.
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>share player</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	issuer := provider(t, "")
	s, db := appWithOptions(t, issuer.URL, t.TempDir(), music, []httpserver.Option{httpserver.WithWeb(os.DirFS(web))})
	registration := register(t, s, "none")
	scopes := auth.ScopeRead + " " + auth.ScopeWrite + " " + auth.ScopeStream
	alice := grant(t, s, registration, scopes, "alice")
	bob := grant(t, s, registration, scopes, "bob")
	readOnly := grant(t, s, registration, auth.ScopeRead, "alice")
	streamOnly := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeStream, "alice")
	rpc := wavelengthv1connect.NewSharingServiceClient(s.Client(), s.URL)
	grpc := wavelengthv1connect.NewSharingServiceClient(s.Client(), s.URL, connect.WithGRPC())
	library := wavelengthv1connect.NewLibraryServiceClient(s.Client(), s.URL)
	page, err := library.ListTracks(ctx, rpcRequest(alice.Access, &pb.ListTracksRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	id, other := page.Msg.Tracks[0].Id, page.Msg.Tracks[1].Id
	mcp := mcpClient(t, s, alice.Access)
	noStream := mcpClient(t, s, readOnly.Access)
	tool(t, noStream, "create_playback_link", map[string]any{"track_id": id}, true)
	if _, err := rpc.CreatePlaybackLink(ctx, rpcRequest(readOnly.Access, &pb.CreatePlaybackLinkRequest{TrackId: id})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("read-only token minted audio access", err)
	}
	if _, err := rpc.CreateShare(ctx, rpcRequest(streamOnly.Access, &pb.CreateShareRequest{TrackIds: []string{id}})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("stream-only token created public share", err)
	}
	for _, token := range []string{"", "test-native"} {
		if _, err := rpc.CreatePlaybackLink(ctx, rpcRequest(token, &pb.CreatePlaybackLinkRequest{TrackId: id})); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatal("unnamed caller minted a link", err)
		}
	}
	// Playback links need stream permission, but not write permission.
	direct, err := grpc.CreatePlaybackLink(ctx, rpcRequest(streamOnly.Access, &pb.CreatePlaybackLinkRequest{TrackId: id}))
	if err != nil {
		t.Fatal(err)
	}
	if direct.Msg.Link.Kind != "playback" || direct.Msg.Link.ExpiresAt == nil {
		t.Fatal(direct.Msg)
	}
	assertAudio := func(target string) {
		t.Helper()
		for _, check := range []struct {
			method, rangeHeader        string
			code                       int
			body, contentRange, length string
		}{
			{"GET", "", 200, audio, "", "36"},
			{"HEAD", "", 200, "", "", "36"},
			{"GET", "bytes=4-9", 206, "456789", "bytes 4-9/36", "6"},
			{"GET", "bytes=10-", 206, audio[10:], "bytes 10-35/36", "26"},
			{"GET", "bytes=-3", 206, "xyz", "bytes 33-35/36", "3"},
			{"GET", "bytes=99-", 416, "", "bytes */36", ""},
		} {
			req, _ := http.NewRequest(check.method, target, nil)
			if check.rangeHeader != "" {
				req.Header.Set("Range", check.rangeHeader)
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != check.code || check.code != 416 && string(body) != check.body || response.Header.Get("Content-Range") != check.contentRange {
				t.Fatalf("%s %s: status=%d range=%s body=%s", check.method, check.rangeHeader, response.StatusCode, response.Header.Get("Content-Range"), body)
			}
			if check.code != 416 && (response.Header.Get("Content-Type") != "audio/mpeg" || response.Header.Get("Content-Length") != check.length || response.Header.Get("Accept-Ranges") != "bytes") {
				t.Fatal("incorrect audio response headers", response.Header)
			}
			if check.code != 416 && !strings.Contains(response.Header.Get("Cache-Control"), "no-store") && strings.Contains(target, "/play/") {
				t.Fatal("bearer audio was cacheable", response.Header)
			}
		}
	}
	assertAudio(direct.Msg.Url)
	native := do(t, http.DefaultClient, "GET", s.URL+"/media/tracks/"+id, nil)
	if native.StatusCode != 401 {
		t.Fatal("private audio became public", native.StatusCode)
	}
	// RPC and MCP use the same durable grant service and owner namespace.
	share := tool(t, mcp, "create_share", map[string]any{"name": "A shared song", "track_ids": []string{id}}, false)
	sharedURL := share["url"].(string)
	sharedID := share["link"].(map[string]any)["id"].(string)
	response := do(t, http.DefaultClient, "GET", sharedURL, nil)
	if response.StatusCode != 200 || response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal(response.StatusCode, response.Header)
	}
	info := do(t, http.DefaultClient, "GET", sharedURL+"/info", nil)
	body, _ := io.ReadAll(info.Body)
	if info.StatusCode != 200 || strings.Contains(string(body), "path") || strings.Contains(string(body), other) || strings.Contains(string(body), "owner") {
		t.Fatal("share leaked unrelated metadata", info.StatusCode, string(body))
	}
	denied := do(t, http.DefaultClient, "POST", sharedURL+"/play/"+other, nil)
	if denied.StatusCode != 404 {
		t.Fatal("share widened to unselected audio", denied.StatusCode)
	}
	denied = do(t, http.DefaultClient, "GET", sharedURL+"/artwork/"+other, nil)
	if denied.StatusCode != 404 {
		t.Fatal("share widened to unselected artwork", denied.StatusCode)
	}
	played := do(t, http.DefaultClient, "POST", sharedURL+"/play/"+id, nil)
	var derived struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(played.Body).Decode(&derived); err != nil {
		t.Fatal(err)
	}
	assertAudio(derived.URL)
	list, err := rpc.ListAccessLinks(ctx, rpcRequest(alice.Access, &pb.ListAccessLinksRequest{}))
	if err != nil || len(list.Msg.Links) != 1 || list.Msg.Links[0].Id != sharedID {
		t.Fatal(list, err)
	}
	otherList, err := rpc.ListAccessLinks(ctx, rpcRequest(bob.Access, &pb.ListAccessLinksRequest{}))
	if err != nil || len(otherList.Msg.Links) != 0 {
		t.Fatal("owner isolation failed", otherList, err)
	}
	if _, err := rpc.RevokeAccessLink(ctx, rpcRequest(bob.Access, &pb.RevokeAccessLinkRequest{Id: sharedID})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatal("another owner revoked a share", err)
	}
	// Only the hash is stored. URLs are intentionally available only on creation.
	u, _ := url.Parse(sharedURL)
	rawToken := strings.TrimPrefix(u.Path, "/s/")
	var stored string
	if err := db.QueryRow("SELECT token_hash FROM media_grants WHERE id=?", sharedID).Scan(&stored); err != nil || stored == rawToken || len(stored) != 64 {
		t.Fatal("raw share credential persisted", stored, err)
	}
	if _, err := rpc.RevokeAccessLink(ctx, rpcRequest(alice.Access, &pb.RevokeAccessLinkRequest{Id: sharedID})); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{sharedURL + "/info", derived.URL} {
		result := do(t, http.DefaultClient, "GET", target, nil)
		if result.StatusCode != 404 {
			t.Fatal("revocation did not block access", target, result.StatusCode)
		}
	}
	// Explicit revocation and expiry also apply to conditional/range requests.
	tool(t, mcp, "revoke_access_link", map[string]any{"id": direct.Msg.Link.Id}, false)
	if response := do(t, http.DefaultClient, "GET", direct.Msg.Url, map[string]string{"Range": "bytes=1-", "If-None-Match": "*"}); response.StatusCode != 404 {
		t.Fatal("revoked link bypassed checks", response.StatusCode)
	}
	fromMCP := tool(t, mcp, "create_playback_link", map[string]any{"track_id": id, "expires_in_seconds": 60}, false)
	playbackID := fromMCP["link"].(map[string]any)["id"].(string)
	if _, err := db.Exec("UPDATE media_grants SET expires_at=? WHERE id=?", time.Now().Add(-time.Second).UnixMilli(), playbackID); err != nil {
		t.Fatal(err)
	}
	if response := do(t, http.DefaultClient, "GET", fromMCP["url"].(string), nil); response.StatusCode != 404 {
		t.Fatal("expired link worked", response.StatusCode)
	}
	for _, ttl := range []int32{-1, 1, 86401} {
		if _, err := rpc.CreatePlaybackLink(ctx, rpcRequest(alice.Access, &pb.CreatePlaybackLinkRequest{TrackId: id, ExpiresInSeconds: ttl})); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal("invalid lifetime accepted", ttl, err)
		}
	}
	if _, err := rpc.CreateShare(ctx, rpcRequest(alice.Access, &pb.CreateShareRequest{TrackIds: []string{id}, ExpiresAt: timestamppb.New(time.Now().Add(-time.Hour))})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("expired share accepted", err)
	}
	// Parent expiry is checked again after playback has started.
	timed, err := rpc.CreateShare(ctx, rpcRequest(alice.Access, &pb.CreateShareRequest{TrackIds: []string{id}, ExpiresAt: timestamppb.New(time.Now().Add(time.Hour))}))
	if err != nil {
		t.Fatal(err)
	}
	play := do(t, http.DefaultClient, "POST", timed.Msg.Url+"/play/"+id, nil)
	if err := json.NewDecoder(play.Body).Decode(&derived); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE media_grants SET expires_at=? WHERE id=?", time.Now().Add(-time.Second).UnixMilli(), timed.Msg.Link.Id); err != nil {
		t.Fatal(err)
	}
	if result := do(t, http.DefaultClient, "GET", derived.URL, nil); result.StatusCode != 404 {
		t.Fatal("parent expiry not enforced", result.StatusCode)
	}
}
