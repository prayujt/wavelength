package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/database"
	"golang.org/x/oauth2"
)

type registeredClient struct {
	ID        string   `json:"client_id"`
	Secret    string   `json:"client_secret"`
	Redirects []string `json:"redirect_uris"`
}
type delegatedToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
}

func requestBody(t *testing.T, client *http.Client, method, target, contentType string, body io.Reader, headers map[string]string) *http.Response {
	t.Helper()
	r, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}
func form(t *testing.T, c *http.Client, target string, values url.Values, headers map[string]string) *http.Response {
	t.Helper()
	return requestBody(t, c, "POST", target, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()), headers)
}
func decode[T any](t *testing.T, r *http.Response, want int) T {
	t.Helper()
	var result T
	if r.StatusCode != want {
		t.Fatalf("unexpected HTTP status %d, expected %d", r.StatusCode, want)
	}
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
func register(t *testing.T, s *httptest.Server, method string) registeredClient {
	t.Helper()
	input := map[string]any{"client_name": "Test AI", "redirect_uris": []string{"http://127.0.0.1:54321/callback?keep=1"}, "token_endpoint_auth_method": method}
	body, _ := json.Marshal(input)
	r := requestBody(t, http.DefaultClient, "POST", s.URL+"/oauth/register", "application/json", bytes.NewReader(body), nil)
	return decode[registeredClient](t, r, 201)
}
func authorizationURL(s *httptest.Server, c registeredClient, verifier, scope string) string {
	q := url.Values{"client_id": {c.ID}, "redirect_uri": {c.Redirects[0]}, "response_type": {"code"}, "state": {"test-state"}, "code_challenge": {oauth2.S256ChallengeFromVerifier(verifier)}, "code_challenge_method": {"S256"}, "resource": {s.URL + "/mcp"}, "scope": {scope}}
	return s.URL + "/oauth/authorize?" + q.Encode()
}

// This follows real OIDC redirects, verifies the consent screen, and submits a
// same-origin CSRF-protected approval. No provider or resource auth is bypassed.
func authorizeClient(t *testing.T, s *httptest.Server, c registeredClient, verifier, scope, subject string) (string, *http.Client) {
	t.Helper()
	redirect, b := approveAuthorization(t, s, authorizationURL(s, c, verifier, scope), subject)
	return redirect.Query().Get("code"), b
}
func approveAuthorization(t *testing.T, s *httptest.Server, target, subject string) (*url.URL, *http.Client) {
	t.Helper()
	input, _ := url.Parse(target)
	b := browser(t)
	start := do(t, b, "GET", target, nil)
	if start.StatusCode != 302 {
		t.Fatalf("authorization start %d", start.StatusCode)
	}
	upstream, _ := url.Parse(start.Header.Get("Location"))
	q := upstream.Query()
	if q.Get("client_id") != "wavelength" {
		t.Fatal("delegated clients must use the original upstream app")
	}
	q.Set("login_hint", subject)
	upstream.RawQuery = q.Encode()
	authorized := do(t, b, "GET", upstream.String(), nil)
	if authorized.StatusCode != 302 {
		t.Fatal("OIDC authorization failed")
	}
	callback := do(t, b, "GET", authorized.Header.Get("Location"), nil)
	if callback.StatusCode != 303 {
		t.Fatalf("OIDC callback %d", callback.StatusCode)
	}
	resume := callback.Header.Get("Location")
	consent := do(t, b, "GET", s.URL+resume, nil)
	if consent.StatusCode != 200 {
		t.Fatalf("consent screen %d", consent.StatusCode)
	}
	html, _ := io.ReadAll(consent.Body)
	if !bytes.Contains(html, []byte("Test AI")) || !bytes.Contains(html, []byte("not verified")) || consent.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing client consent and framing restrictions")
	}
	u, _ := url.Parse(resume)
	values := url.Values{"request": {u.Query().Get("request")}, "csrf": {me(t, b, s.URL).CSRF}, "decision": {"allow"}}
	approved := form(t, b, s.URL+"/oauth/authorize", values, map[string]string{"Origin": s.URL})
	if approved.StatusCode != 303 {
		t.Fatalf("consent approval %d", approved.StatusCode)
	}
	redirect, _ := url.Parse(approved.Header.Get("Location"))
	if redirect.Query().Get("state") != input.Query().Get("state") || redirect.Query().Get("iss") != s.URL || redirect.Query().Get("keep") != "1" {
		t.Fatal("authorization redirect lost binding values")
	}
	return redirect, b
}
func exchange(t *testing.T, s *httptest.Server, c registeredClient, code, verifier string) *http.Response {
	t.Helper()
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {c.ID}, "redirect_uri": {c.Redirects[0]}, "code": {code}, "code_verifier": {verifier}, "resource": {s.URL + "/mcp"}}
	if c.Secret != "" {
		values.Set("client_secret", c.Secret)
	}
	return form(t, http.DefaultClient, s.URL+"/oauth/token", values, nil)
}
func grant(t *testing.T, s *httptest.Server, c registeredClient, scope, subject string) delegatedToken {
	t.Helper()
	v := oauth2.GenerateVerifier()
	code, _ := authorizeClient(t, s, c, v, scope, subject)
	return decode[delegatedToken](t, exchange(t, s, c, code, v), 200)
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(copy)
}
func mcpClient(t *testing.T, s *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "wavelength-integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token}, Timeout: 5 * time.Second}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func tool(t *testing.T, c *mcp.ClientSession, name string, args map[string]any, wantError bool) map[string]any {
	t.Helper()
	result, err := c.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError != wantError {
		t.Fatalf("tool %s error=%v, expected %v", name, result.IsError, wantError)
	}
	if wantError {
		return nil
	}
	value, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("tool %s missing structured object output", name)
	}
	return value
}
func oauthFixture(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	music := t.TempDir()
	if err := os.WriteFile(filepath.Join(music, "tone.wav"), []byte("RIFF test audio"), 0600); err != nil {
		t.Fatal(err)
	}
	issuer := provider(t, "")
	return app(t, issuer.URL, t.TempDir(), music)
}
func rpcRequest[T any](token string, message *T) *connect.Request[T] {
	r := connect.NewRequest(message)
	r.Header().Set("Authorization", "Bearer "+token)
	return r
}

func TestMCPAndNativeShareIdentityCollectionsAndOAuth(t *testing.T) {
	s, db := oauthFixture(t)
	c := register(t, s, "none")
	a := grant(t, s, c, auth.ScopeRead+" "+auth.ScopeWrite+" "+auth.ScopeStream, "subject-1")
	b := grant(t, s, c, auth.ScopeRead+" "+auth.ScopeWrite, "subject-2")
	alice, bob := mcpClient(t, s, a.Access), mcpClient(t, s, b.Access)
	listing, err := alice.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Tools) != 21 {
		t.Fatalf("tools count %d", len(listing.Tools))
	}
	for _, item := range listing.Tools {
		if strings.Contains(item.Name, "download") || item.Name == "stream_audio" || item.Annotations == nil || item.InputSchema == nil {
			t.Fatalf("unexpected tool contract: %s", item.Name)
		}
	}
	// Every native operation has a tool, except transport-specific audio bytes.
	names := []string{}
	for _, item := range listing.Tools {
		names = append(names, item.Name)
	}
	services := pb.File_wavelength_v1_library_proto.Services()
	for i := 0; i < services.Len(); i++ {
		methods := services.Get(i).Methods()
		for j := 0; j < methods.Len(); j++ {
			name := string(methods.Get(j).Name())
			if name == "StreamAudio" {
				continue
			}
			name = strings.ToLower(strings.TrimPrefix(regexp.MustCompile(`[A-Z]`).ReplaceAllString(name, `_$0`), "_"))
			if !slices.Contains(names, name) {
				t.Fatalf("native operation has no MCP equivalent: %s", name)
			}
		}
	}
	who := tool(t, alice, "get_current_user", map[string]any{}, false)["user"].(map[string]any)
	if who["id"] == tool(t, bob, "get_current_user", map[string]any{}, false)["user"].(map[string]any)["id"] {
		t.Fatal("different OIDC subjects share identity")
	}
	tracks := tool(t, alice, "list_tracks", map[string]any{"limit": 1}, false)["tracks"].([]any)
	track := tracks[0].(map[string]any)
	id := track["id"].(string)
	if _, exists := track["path"]; exists {
		t.Fatal("MCP leaked source paths")
	}
	tool(t, alice, "get_track", map[string]any{"id": id}, false)
	artists := tool(t, alice, "list_artists", map[string]any{}, false)["artists"].([]any)
	artistID := artists[0].(map[string]any)["id"].(string)
	tool(t, alice, "get_artist", map[string]any{"id": artistID}, false)
	albums := tool(t, alice, "list_albums", map[string]any{"artist_id": artistID}, false)["albums"].([]any)
	tool(t, alice, "get_album", map[string]any{"id": albums[0].(map[string]any)["id"]}, false)
	link := tool(t, alice, "get_playback_info", map[string]any{"id": id}, false)["url"].(string)
	if strings.Contains(link, a.Access) || strings.Contains(link, "?") {
		t.Fatal("playback URL contains credentials")
	}
	if r := do(t, http.DefaultClient, "GET", link, map[string]string{"Authorization": "Bearer " + a.Access, "Range": "bytes=0-3"}); r.StatusCode != 206 {
		t.Fatalf("scoped playback %d", r.StatusCode)
	}
	if r := do(t, http.DefaultClient, "GET", link, map[string]string{"Authorization": "Bearer " + b.Access}); r.StatusCode != 403 {
		t.Fatal("stream permission not enforced")
	}
	mediaClient := wavelengthv1connect.NewLibraryServiceClient(s.Client(), s.URL)
	deniedStream, err := mediaClient.StreamAudio(context.Background(), rpcRequest(b.Access, &pb.StreamAudioRequest{Id: id}))
	if err == nil {
		if deniedStream.Receive() {
			t.Fatal("stream-scoped native RPC accepted a read-only audio grant")
		}
		err = deniedStream.Err()
		deniedStream.Close()
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("native streaming scope was not enforced")
	}
	allowedStream, err := mediaClient.StreamAudio(context.Background(), rpcRequest(a.Access, &pb.StreamAudioRequest{Id: id, LengthBytes: 4}))
	if err != nil {
		t.Fatal(err)
	}
	if !allowedStream.Receive() || string(allowedStream.Msg().Data) != "RIFF" {
		t.Fatal("stream-scoped native audio failed")
	}
	allowedStream.Close()
	p := tool(t, alice, "create_playlist", map[string]any{"name": "Private", "track_ids": []string{id}}, false)["playlist"].(map[string]any)
	playlistID := p["id"].(string)
	if p["owner_id"] != who["id"] {
		t.Fatal("playlist owner does not match the stable public user ID")
	}
	tool(t, bob, "get_playlist", map[string]any{"id": playlistID}, true)
	tool(t, bob, "update_playlist", map[string]any{"id": playlistID, "name": "Hijack"}, true)
	tool(t, bob, "delete_playlist", map[string]any{"id": playlistID}, true)
	if tool(t, bob, "list_playlists", map[string]any{}, false)["total"] != float64(0) {
		t.Fatal("private playlist visible to other user")
	}
	// All four native protocols read the same playlist created through MCP.
	tls := httptest.NewUnstartedServer(s.Config.Handler)
	tls.EnableHTTP2 = true
	tls.StartTLS()
	defer tls.Close()
	for _, options := range [][]connect.ClientOption{nil, {connect.WithProtoJSON()}, {connect.WithGRPCWeb()}, {connect.WithGRPC()}} {
		native := wavelengthv1connect.NewUserDataServiceClient(tls.Client(), tls.URL, options...)
		got, err := native.GetPlaylist(context.Background(), rpcRequest(a.Access, &pb.GetPlaylistRequest{Id: playlistID}))
		if err != nil || got.Msg.Playlist.Name != "Private" {
			t.Fatalf("native/MCP playlist mismatch: %v", err)
		}
		if _, err = native.GetPlaylist(context.Background(), rpcRequest(b.Access, &pb.GetPlaylistRequest{Id: playlistID})); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatal("native cross-user read accepted")
		}
	}
	native := wavelengthv1connect.NewUserDataServiceClient(s.Client(), s.URL)
	current, err := native.GetCurrentUser(context.Background(), rpcRequest(a.Access, &pb.GetCurrentUserRequest{}))
	if err != nil || current.Msg.User.Id != who["id"] {
		t.Fatal("native and MCP identities differ")
	}
	created, err := native.CreatePlaylist(context.Background(), rpcRequest(a.Access, &pb.CreatePlaylistRequest{Name: "From native", TrackIds: []string{id}}))
	if err != nil {
		t.Fatal(err)
	}
	tool(t, alice, "get_playlist", map[string]any{"id": created.Msg.Playlist.Id}, false)
	tool(t, alice, "update_playlist", map[string]any{"id": playlistID, "public": true, "comment": "Shared", "add_track_ids": []string{id}}, false)
	tool(t, bob, "get_playlist", map[string]any{"id": playlistID}, false)
	tool(t, bob, "update_playlist", map[string]any{"id": playlistID, "name": "Still forbidden"}, true)
	tool(t, alice, "update_playlist", map[string]any{"id": playlistID, "replace_track_ids": []string{}}, false)
	if len(tool(t, alice, "get_playlist", map[string]any{"id": playlistID}, false)["playlist"].(map[string]any)["track_ids"].([]any)) != 0 {
		t.Fatal("empty replacement did not clear playlist")
	}
	tool(t, alice, "update_playlist", map[string]any{"id": playlistID, "replace_track_ids": []string{}, "add_track_ids": []string{id}}, true)
	tool(t, alice, "save_queue", map[string]any{"track_ids": []string{id}, "current_track_id": id, "position_ms": 1234}, false)
	queue, err := native.GetQueue(context.Background(), rpcRequest(a.Access, &pb.GetQueueRequest{}))
	if err != nil || queue.Msg.Queue.PositionMs != 1234 {
		t.Fatal("queue not shared across transports")
	}
	if len(tool(t, bob, "get_queue", map[string]any{}, false)["queue"].(map[string]any)["track_ids"].([]any)) != 0 {
		t.Fatal("queue leaked across users")
	}
	tool(t, alice, "save_bookmark", map[string]any{"track_id": id, "position_ms": 3456}, false)
	bookmarks, err := native.ListBookmarks(context.Background(), rpcRequest(a.Access, &pb.ListBookmarksRequest{}))
	if err != nil || bookmarks.Msg.Total != 1 || bookmarks.Msg.Bookmarks[0].PositionMs != 3456 {
		t.Fatal("bookmark not shared across transports")
	}
	tool(t, alice, "list_bookmarks", map[string]any{}, false)
	if tool(t, bob, "list_bookmarks", map[string]any{}, false)["total"] != float64(0) {
		t.Fatal("bookmark leaked across users")
	}
	tool(t, alice, "record_play", map[string]any{"track_id": id}, false)
	history, err := native.GetHistory(context.Background(), rpcRequest(a.Access, &pb.GetHistoryRequest{}))
	if err != nil || history.Msg.Entries[0].Plays != 1 {
		t.Fatal("history not shared across transports")
	}
	tool(t, alice, "get_history", map[string]any{}, false)
	tool(t, alice, "get_now_playing", map[string]any{}, false)
	if len(tool(t, bob, "get_now_playing", map[string]any{}, false)["entries"].([]any)) != 0 {
		t.Fatal("other listener activity leaked")
	}
	if _, err = native.SaveQueue(context.Background(), rpcRequest(a.Access, &pb.SaveQueueRequest{TrackIds: []string{}, PositionMs: 0})); err != nil {
		t.Fatal(err)
	}
	if _, err = native.SaveBookmark(context.Background(), rpcRequest(a.Access, &pb.SaveBookmarkRequest{TrackId: id, PositionMs: 22})); err != nil {
		t.Fatal(err)
	}
	if _, err = native.RecordPlay(context.Background(), rpcRequest(a.Access, &pb.RecordPlayRequest{TrackId: id, NowPlayingOnly: true})); err != nil {
		t.Fatal(err)
	}
	if _, err = native.GetNowPlaying(context.Background(), rpcRequest(a.Access, &pb.GetNowPlayingRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err = native.DeleteBookmark(context.Background(), rpcRequest(a.Access, &pb.DeleteBookmarkRequest{TrackId: id})); err != nil {
		t.Fatal(err)
	}
	tool(t, alice, "save_bookmark", map[string]any{"track_id": id}, false)
	tool(t, alice, "delete_bookmark", map[string]any{"track_id": id}, false)
	if _, err = native.UpdatePlaylist(context.Background(), rpcRequest(a.Access, &pb.UpdatePlaylistRequest{Id: playlistID, ReplaceTracks: &pb.TrackIDs{Ids: []string{id}}})); err != nil {
		t.Fatal(err)
	}
	if _, err = native.ListPlaylists(context.Background(), rpcRequest(a.Access, &pb.ListPlaylistsRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err = native.DeletePlaylist(context.Background(), rpcRequest(a.Access, &pb.DeletePlaylistRequest{Id: created.Msg.Playlist.Id})); err != nil {
		t.Fatal(err)
	}
	tool(t, alice, "delete_playlist", map[string]any{"id": playlistID}, false)
	// All persisted bearer credentials must be hashes, not their raw values.
	rows, err := db.Query("SELECT value FROM app_state WHERE scope LIKE 'oauth/%'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		for _, credential := range []string{a.Access, a.Refresh, b.Access, b.Refresh} {
			if strings.Contains(raw, credential) {
				t.Fatal("raw delegated credential persisted")
			}
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthDiscoveryAndAuthorizationBoundaries(t *testing.T) {
	s, _ := oauthFixture(t)
	challenge := do(t, http.DefaultClient, "POST", s.URL+"/mcp", nil)
	if challenge.StatusCode != 401 || !strings.Contains(challenge.Header.Get("WWW-Authenticate"), s.URL+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatal("missing OAuth discovery challenge")
	}
	meta := decode[map[string]any](t, do(t, http.DefaultClient, "GET", s.URL+"/.well-known/oauth-protected-resource/mcp", nil), 200)
	if meta["resource"] != s.URL+"/mcp" {
		t.Fatal("resource metadata incorrect")
	}
	issuer := decode[map[string]any](t, do(t, http.DefaultClient, "GET", s.URL+"/.well-known/oauth-authorization-server", nil), 200)
	if issuer["issuer"] != s.URL || issuer["registration_endpoint"] != s.URL+"/oauth/register" {
		t.Fatal("authorization metadata incorrect")
	}
	c := register(t, s, "none")
	for _, mode := range []string{"unknown-client", "redirect", "resource", "pkce-plain", "pkce-missing", "bad-scope", "implicit", "duplicate"} {
		u, _ := url.Parse(authorizationURL(s, c, oauth2.GenerateVerifier(), ""))
		q := u.Query()
		switch mode {
		case "unknown-client":
			q.Set("client_id", "unregistered")
		case "redirect":
			q.Set("redirect_uri", "https://evil.example/callback")
		case "resource":
			q.Set("resource", "https://other.example/mcp")
		case "pkce-plain":
			q.Set("code_challenge_method", "plain")
		case "pkce-missing":
			q.Del("code_challenge")
		case "bad-scope":
			q.Set("scope", "admin")
		case "implicit":
			q.Set("response_type", "token")
		case "duplicate":
			q.Add("resource", s.URL+"/mcp")
		}
		u.RawQuery = q.Encode()
		r := do(t, browser(t), "GET", u.String(), nil)
		if r.StatusCode != 400 || r.Header.Get("Location") != "" {
			t.Fatalf("invalid request %s redirected or was accepted", mode)
		}
	}
	for _, redirect := range []string{"http://public.example/callback", "https://example.com/callback#fragment", "https://user:password@example.com/", "javascript:alert(1)"} {
		body, _ := json.Marshal(map[string]any{"redirect_uris": []string{redirect}})
		r := requestBody(t, http.DefaultClient, "POST", s.URL+"/oauth/register", "application/json", bytes.NewReader(body), nil)
		if r.StatusCode != 400 {
			t.Fatal("unsafe registered redirect accepted")
		}
	}
	if r := do(t, http.DefaultClient, "POST", s.URL+"/mcp", map[string]string{"Authorization": "Bearer test-native"}); r.StatusCode != 401 {
		t.Fatal("static shared token accepted by MCP")
	}
}

func TestOAuthCodeBindingExpiryReplayAndConcurrentExchange(t *testing.T) {
	for _, mode := range []string{"verifier", "redirect", "client", "resource", "expired", "replay", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			s, db := oauthFixture(t)
			c := register(t, s, "none")
			v := oauth2.GenerateVerifier()
			code, _ := authorizeClient(t, s, c, v, "", "subject-1")
			values := url.Values{"grant_type": {"authorization_code"}, "client_id": {c.ID}, "redirect_uri": {c.Redirects[0]}, "code": {code}, "code_verifier": {v}, "resource": {s.URL + "/mcp"}}
			switch mode {
			case "verifier":
				values.Set("code_verifier", oauth2.GenerateVerifier())
			case "redirect":
				values.Set("redirect_uri", "https://evil.example/")
			case "client":
				values.Set("client_id", register(t, s, "none").ID)
			case "resource":
				values.Set("resource", "https://evil.example/mcp")
			case "expired":
				if _, err := db.Exec("UPDATE app_state SET value=json_set(value,'$.expires_at','2000-01-01T00:00:00Z') WHERE scope='oauth/codes'"); err != nil {
					t.Fatal(err)
				}
			case "replay":
				decode[delegatedToken](t, exchange(t, s, c, code, v), 200)
			case "concurrent":
				var wg sync.WaitGroup
				statuses := make(chan int, 2)
				for i := 0; i < 2; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						r, err := http.PostForm(s.URL+"/oauth/token", values)
						if err != nil {
							statuses <- 0
							return
						}
						defer r.Body.Close()
						statuses <- r.StatusCode
					}()
				}
				wg.Wait()
				close(statuses)
				got := []int{}
				for status := range statuses {
					got = append(got, status)
				}
				slices.Sort(got)
				if !slices.Equal(got, []int{200, 400}) {
					t.Fatal("code redeemed more than once", got)
				}
				return
			}
			if r := form(t, http.DefaultClient, s.URL+"/oauth/token", values, nil); r.StatusCode != 400 {
				t.Fatalf("invalid %s grant accepted", mode)
			}
		})
	}
}

func TestOAuthRefreshRotationScopesAndRevocation(t *testing.T) {
	for _, mode := range []string{"refresh", "replay", "wrong-client", "scope-escalation", "resource", "expired", "revoke", "confidential"} {
		t.Run(mode, func(t *testing.T) {
			s, db := oauthFixture(t)
			method := "none"
			if mode == "confidential" {
				method = "client_secret_post"
			}
			c := register(t, s, method)
			g := grant(t, s, c, auth.ScopeRead, "subject-1")
			values := url.Values{"grant_type": {"refresh_token"}, "client_id": {c.ID}, "refresh_token": {g.Refresh}}
			if c.Secret != "" {
				values.Set("client_secret", c.Secret)
			}
			switch mode {
			case "wrong-client":
				values.Set("client_id", register(t, s, "none").ID)
			case "scope-escalation":
				values.Set("scope", auth.ScopeRead+" "+auth.ScopeWrite)
			case "resource":
				values.Set("resource", "https://other.example/mcp")
			case "expired":
				if _, err := db.Exec("UPDATE app_state SET value=json_set(value,'$.expires_at','2000-01-01T00:00:00Z') WHERE scope='oauth/grants'"); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				r := form(t, http.DefaultClient, s.URL+"/oauth/revoke", url.Values{"client_id": {c.ID}, "token": {g.Refresh}}, nil)
				if r.StatusCode != 200 {
					t.Fatal("revoke failed")
				}
			}
			if slices.Contains([]string{"wrong-client", "scope-escalation", "resource", "expired", "revoke"}, mode) {
				if r := form(t, http.DefaultClient, s.URL+"/oauth/token", values, nil); r.StatusCode != 400 {
					t.Fatal("invalid refresh accepted")
				}
				return
			}
			rotated := decode[delegatedToken](t, form(t, http.DefaultClient, s.URL+"/oauth/token", values, nil), 200)
			if rotated.Refresh == g.Refresh || rotated.Access == g.Access {
				t.Fatal("credentials not rotated")
			}
			if r := do(t, http.DefaultClient, "POST", s.URL+"/mcp", map[string]string{"Authorization": "Bearer " + g.Access}); r.StatusCode != 401 {
				t.Fatal("old access credential survived rotation")
			}
			if mode == "replay" {
				if r := form(t, http.DefaultClient, s.URL+"/oauth/token", values, nil); r.StatusCode != 400 {
					t.Fatal("refresh replay accepted")
				}
				if r := do(t, http.DefaultClient, "POST", s.URL+"/mcp", map[string]string{"Authorization": "Bearer " + rotated.Access}); r.StatusCode != 401 {
					t.Fatal("refresh replay did not revoke token family")
				}
			} else {
				tool(t, mcpClient(t, s, rotated.Access), "get_current_user", map[string]any{}, false)
			}
		})
	}
}

func TestOAuthReadOnlyTokenAndPersonalIdentityRequired(t *testing.T) {
	s, _ := oauthFixture(t)
	c := register(t, s, "none")
	g := grant(t, s, c, auth.ScopeRead, "subject-1")
	ai := mcpClient(t, s, g.Access)
	tool(t, ai, "list_playlists", map[string]any{}, false)
	tool(t, ai, "create_playlist", map[string]any{"name": "Forbidden"}, true)
	tool(t, ai, "list_tracks", map[string]any{"limit": -1}, true)
	tool(t, ai, "list_tracks", map[string]any{"offset": -1}, true)
	tool(t, ai, "get_track", map[string]any{"id": "missing"}, true)
	native := wavelengthv1connect.NewUserDataServiceClient(s.Client(), s.URL)
	if _, err := native.CreatePlaylist(context.Background(), rpcRequest(g.Access, &pb.CreatePlaylistRequest{Name: "Forbidden"})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("read-only native mutation accepted")
	}
	if _, err := native.GetCurrentUser(context.Background(), rpcRequest("test-native", &pb.GetCurrentUserRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatal("static credential impersonated personal identity")
	}
	// MCP rejects browser cookies and foreign browser origins; native retains CSRF.
	v := oauth2.GenerateVerifier()
	_, b := authorizeClient(t, s, c, v, "", "subject-1")
	if r := do(t, b, "POST", s.URL+"/mcp", nil); r.StatusCode != 401 {
		t.Fatal("browser cookie accepted by MCP")
	}
	if r := do(t, http.DefaultClient, "POST", s.URL+"/mcp", map[string]string{"Authorization": "Bearer " + g.Access, "Origin": "https://evil.example"}); r.StatusCode != 403 {
		t.Fatal("foreign MCP origin accepted")
	}
}

func TestOAuthConsentCSRFAndFreshProviderPolicy(t *testing.T) {
	s, _ := oauthFixture(t)
	c := register(t, s, "none")
	b := browser(t)
	callback, _ := beginLogin(t, b, s.URL)
	if r := do(t, b, "GET", callback, nil); r.StatusCode != 303 {
		t.Fatal("initial login failed")
	}
	start := do(t, b, "GET", authorizationURL(s, c, oauth2.GenerateVerifier(), ""), nil)
	// Starting a flow must not let an existing local session skip provider checks.
	var pending *http.Cookie
	for _, cookie := range start.Cookies() {
		if cookie.Name == "wavelength_oauth" {
			pending = cookie
		}
	}
	if pending == nil {
		t.Fatal("missing browser-bound authorization request")
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(pending.Value)))
	resume := "/oauth/authorize?request=" + id
	if r := do(t, b, "GET", s.URL+resume, nil); r.StatusCode != 400 {
		t.Fatal("old local session bypassed fresh provider policy")
	}
	upstream := do(t, b, "GET", start.Header.Get("Location"), nil)
	if r := do(t, b, "GET", upstream.Header.Get("Location"), nil); r.StatusCode != 303 {
		t.Fatal("OIDC resume failed")
	}
	current := me(t, b, s.URL)
	values := url.Values{"request": {id}, "csrf": {current.CSRF}, "decision": {"allow"}}
	if r := form(t, b, s.URL+"/oauth/authorize", values, nil); r.StatusCode != 403 {
		t.Fatal("consent accepted without Origin")
	}
	for _, origin := range []string{"null", "https://evil.example"} {
		if r := form(t, b, s.URL+"/oauth/authorize", values, map[string]string{"Origin": origin}); r.StatusCode != 403 {
			t.Fatal("consent accepted a null or foreign Origin")
		}
	}
	values.Set("csrf", "wrong")
	if r := form(t, b, s.URL+"/oauth/authorize", values, map[string]string{"Origin": s.URL}); r.StatusCode != 403 {
		t.Fatal("consent accepted wrong CSRF")
	}
	values.Set("csrf", current.CSRF)
	values.Set("decision", "deny")
	r := form(t, b, s.URL+"/oauth/authorize", values, map[string]string{"Origin": s.URL})
	redirect, _ := url.Parse(r.Header.Get("Location"))
	if r.StatusCode != 303 || redirect.Query().Get("error") != "access_denied" || redirect.Query().Get("code") != "" {
		t.Fatal("denied consent issued a code")
	}
	if r := form(t, b, s.URL+"/oauth/authorize", values, map[string]string{"Origin": s.URL}); r.StatusCode != 400 {
		t.Fatal("consent was replayable")
	}
}

func TestOAuthClientAuthenticationMethods(t *testing.T) {
	for _, method := range []string{"none", "client_secret_basic", "client_secret_post"} {
		t.Run(method, func(t *testing.T) {
			s, _ := oauthFixture(t)
			c := register(t, s, method)
			v := oauth2.GenerateVerifier()
			code, _ := authorizeClient(t, s, c, v, "", "subject-1")
			values := url.Values{"grant_type": {"authorization_code"}, "client_id": {c.ID}, "redirect_uri": {c.Redirects[0]}, "code": {code}, "code_verifier": {v}}
			headers := map[string]string{}
			if method == "client_secret_post" {
				values.Set("client_secret", c.Secret)
			}
			if method == "client_secret_basic" {
				r, _ := http.NewRequest("POST", s.URL+"/oauth/token", nil)
				r.SetBasicAuth(c.ID, c.Secret)
				headers["Authorization"] = r.Header.Get("Authorization")
			}
			if method != "none" {
				if r := form(t, http.DefaultClient, s.URL+"/oauth/token", url.Values{"client_id": {c.ID}, "grant_type": {"authorization_code"}}, nil); r.StatusCode != 401 {
					t.Fatal("confidential client accepted without secret")
				}
			}
			g := decode[delegatedToken](t, form(t, http.DefaultClient, s.URL+"/oauth/token", values, headers), 200)
			tool(t, mcpClient(t, s, g.Access), "get_current_user", map[string]any{}, false)
		})
	}
}

func TestOAuthConnectionsCanOnlyBeRevokedByTheirOwner(t *testing.T) {
	s, _ := oauthFixture(t)
	c := register(t, s, "none")
	v := oauth2.GenerateVerifier()
	code, alice := authorizeClient(t, s, c, v, "", "subject-1")
	g := decode[delegatedToken](t, exchange(t, s, c, code, v), 200)
	_, bob := authorizeClient(t, s, c, oauth2.GenerateVerifier(), "", "subject-2")
	connections := decode[struct {
		Connections []struct {
			ID string `json:"id"`
		}
	}](t, do(t, alice, "GET", s.URL+"/auth/connections", nil), 200)
	if len(connections.Connections) != 1 {
		t.Fatal("owner cannot list their connection")
	}
	target := s.URL + "/auth/connections/" + connections.Connections[0].ID + "/revoke"
	if r := do(t, bob, "POST", target, map[string]string{"Origin": s.URL, "X-CSRF-Token": me(t, bob, s.URL).CSRF}); r.StatusCode != 404 {
		t.Fatal("another user revoked a connection")
	}
	if r := do(t, alice, "POST", target, nil); r.StatusCode != 403 {
		t.Fatal("connection revocation bypassed CSRF")
	}
	if r := do(t, alice, "POST", target, map[string]string{"Origin": s.URL, "X-CSRF-Token": me(t, alice, s.URL).CSRF}); r.StatusCode != 204 {
		t.Fatal("owner cannot revoke a connection")
	}
	if r := do(t, http.DefaultClient, "POST", s.URL+"/mcp", map[string]string{"Authorization": "Bearer " + g.Access}); r.StatusCode != 401 {
		t.Fatal("revoked credential remained valid")
	}
}

func TestOAuthGrantPersistenceResourceAndIssuerIsolation(t *testing.T) {
	issuer := provider(t, "")
	data, music := t.TempDir(), t.TempDir()
	s, db := app(t, issuer.URL, data, music)
	c := register(t, s, "none")
	g := grant(t, s, c, "", "subject-1")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, mode := range []string{"same", "other-resource", "other-issuer", "expired-access"} {
		config := auth.Config{Issuer: issuer.URL, ClientID: "wavelength", ClientSecret: "test-secret", PublicURL: s.URL}
		if mode == "other-resource" {
			config.PublicURL = "https://other.example"
		}
		if mode == "other-issuer" {
			config.Issuer = provider(t, "").URL
		}
		if mode == "expired-access" {
			if _, err := reopened.Exec("UPDATE app_state SET value=json_set(value,'$.AccessExpiresAt','2000-01-01T00:00:00Z') WHERE scope='oauth/grants'"); err != nil {
				t.Fatal(err)
			}
		}
		manager, err := auth.New(context.Background(), config, database.AuthStore{DB: reopened}, auth.WithOAuthStore(database.StateStore{DB: reopened}))
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", s.URL+"/mcp", nil)
		r.Header.Set("Authorization", "Bearer "+g.Access)
		ctx, err := manager.Authorize(r)
		if mode == "same" {
			if err != nil {
				t.Fatal("grant did not survive database reopen")
			}
			user, ok := auth.UserFromContext(ctx)
			if !ok || user.Subject != "subject-1" {
				t.Fatal("persisted identity lost")
			}
		} else if err == nil {
			t.Fatalf("credential accepted after %s", mode)
		}
	}
}

func TestMCPLegacyProtocolVersionsAndSessionIdentity(t *testing.T) {
	s, _ := oauthFixture(t)
	c := register(t, s, "none")
	g := grant(t, s, c, "", "subject-1")
	for _, version := range []string{"2025-03-26", "2025-06-18", "2025-11-25"} {
		headers := map[string]string{"Authorization": "Bearer " + g.Access, "Accept": "application/json, text/event-stream", "MCP-Protocol-Version": version, "Mcp-Session-Id": "untrusted-other-user-session"}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": version, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "legacy", "version": "1"}}})
		init := decode[map[string]any](t, requestBody(t, http.DefaultClient, "POST", s.URL+"/mcp", "application/json", bytes.NewReader(body), headers), 200)
		if init["error"] != nil {
			t.Fatalf("legacy initialize rejected: %s", version)
		}
		body = []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_current_user","arguments":{}}}`)
		call := decode[map[string]any](t, requestBody(t, http.DefaultClient, "POST", s.URL+"/mcp", "application/json", bytes.NewReader(body), headers), 200)
		result, ok := call["result"].(map[string]any)
		if !ok || result["isError"] == true {
			t.Fatalf("legacy tool call rejected: %s", version)
		}
		identity := result["structuredContent"].(map[string]any)["user"].(map[string]any)
		if identity["id"] == "" {
			t.Fatal("session header overrode authenticated identity")
		}
	}
}

func TestOfficialMCPOAuthClientDiscoversRegistersAndAuthorizes(t *testing.T) {
	s, _ := oauthFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	flows := 0
	handler, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "Test AI", RedirectURIs: []string{"http://127.0.0.1:54321/callback?keep=1"}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"},
		}},
		RequestRefreshToken: true,
		AuthorizationCodeFetcher: func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			flows++
			redirect, _ := approveAuthorization(t, s, args.URL, "subject-1")
			q := redirect.Query()
			return &mcpauth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "automatic-oauth", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.URL + "/mcp", OAuthHandler: handler, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if flows != 1 {
		t.Fatalf("expected one consent flow, got %d", flows)
	}
	tool(t, session, "get_current_user", map[string]any{}, false)
	// The discovery challenge must request management permissions by default.
	tool(t, session, "create_playlist", map[string]any{"name": "Created after automatic OAuth"}, false)
	source, err := handler.TokenSource(ctx)
	if err != nil || source == nil {
		t.Fatal("OAuth client has no token source")
	}
	token, err := source.Token()
	if err != nil || token.RefreshToken == "" {
		t.Fatal("automatic client did not receive a renewable grant")
	}
}
