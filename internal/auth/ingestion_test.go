package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	transportmcp "github.com/prayujt/wavelength/internal/transport/mcp"
	"github.com/prayujt/wavelength/internal/userdata"
)

var externalRef = discovery.Ref{Provider: "itunes", ID: "123"}
var audioRef = discovery.Ref{Provider: "youtube", ID: "BaW_jenozKc"}

type ingestMetadata struct{}

func (ingestMetadata) Track(_ context.Context, ref discovery.Ref) (discovery.Track, error) {
	if ref.ID == "456" {
		return discovery.Track{Ref: ref, Title: "Streamed Tone", Artist: "Streamed Artist", Album: "Streamed Album", AlbumArtist: "Streamed Artist", Duration: .25}, nil
	}
	return discovery.Track{Ref: ref, Title: "Imported Tone", Artist: "Imported Artist", Album: "Imported Album", AlbumArtist: "Imported Artist", Duration: .25}, nil
}
func (m ingestMetadata) Search(ctx context.Context, query string, _ int) (discovery.Results, error) {
	ref := externalRef
	if query == "Streamed Artist" {
		ref = discovery.Ref{Provider: "itunes", ID: "456"}
	}
	track, _ := m.Track(ctx, ref)
	return discovery.Results{Tracks: []discovery.Track{track}, Releases: []discovery.Release{}, Artists: []discovery.Artist{}}, nil
}
func (m ingestMetadata) Release(ctx context.Context, ref discovery.Ref) (discovery.Release, error) {
	track, _ := m.Track(ctx, externalRef)
	return discovery.Release{Ref: ref, Title: "Imported Album", Artist: "Imported Artist", Tracks: []discovery.Track{track}}, nil
}
func (m ingestMetadata) Releases(ctx context.Context, refs []discovery.Ref) ([]discovery.Release, error) {
	out := []discovery.Release{}
	for _, ref := range refs {
		release, err := m.Release(ctx, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, release)
	}
	return out, nil
}
func (ingestMetadata) Artist(_ context.Context, ref discovery.Ref) (discovery.Artist, error) {
	return discovery.Artist{Ref: ref, Name: "Imported Artist", Releases: []discovery.Release{}}, nil
}

type ingestSource struct {
	audio     []byte
	downloads atomic.Int32
	block     bool
	started   chan struct{}
}

func (*ingestSource) Candidates(context.Context, discovery.Track) ([]discovery.Candidate, error) {
	return []discovery.Candidate{{Ref: audioRef, Title: "Imported Tone", Uploader: "Imported Artist", Duration: .25, Score: .99, Suitable: true}}, nil
}
func (s *ingestSource) Download(ctx context.Context, ref discovery.Ref, path string) error {
	s.downloads.Add(1)
	if ref.ID == "abcdefghijk" {
		return errors.New("fixture recording unavailable")
	}
	if s.block {
		select {
		case s.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return os.WriteFile(path, s.audio, 0600)
}

type ingestMedia struct{}

func (ingestMedia) Prepare(_ context.Context, path string, t discovery.Track) (library.Track, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return library.Track{}, nil, err
	}
	return library.Track{Title: t.Title, Artist: t.Artist, Album: t.Album, ContentType: "audio/mpeg", Duration: .25, Size: info.Size(), ModifiedAt: info.ModTime().UTC()}, []string{}, nil
}
func acquisitionFixture(t *testing.T, source *ingestSource) *httptest.Server {
	t.Helper()
	issuer := provider(t, "")
	server, _ := app(t, issuer.URL, t.TempDir(), t.TempDir(), func(db *sql.DB, lib *library.Library, data *userdata.Service) transportmcp.Acquisition {
		d := discovery.New(ingestMetadata{}, source)
		root, path := lib.SourceRoot()
		service, err := ingest.New(context.Background(), ingest.Root{ID: root, Path: path}, t.TempDir(), ingest.Dependencies{Store: database.IngestStore{DB: db}, Discovery: d, Downloader: source, Media: ingestMedia{}, Playlists: data})
		if err != nil {
			t.Fatal(err)
		}
		return transportmcp.Acquisition{Discovery: d, Ingest: service}
	})
	return server
}
func waitImport(t *testing.T, client wavelengthv1connect.IngestionServiceClient, token, id string, states ...string) *pb.ImportJob {
	t.Helper()
	want := ingest.Completed
	if len(states) > 0 {
		want = states[0]
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := client.GetImport(context.Background(), rpcRequest(token, &pb.GetImportRequest{Id: id}))
		if err != nil {
			t.Fatal(err)
		}
		if r.Msg.State == want {
			return r.Msg
		}
		time.Sleep(5 * time.Millisecond)
	}
	r, _ := client.GetImport(context.Background(), rpcRequest(token, &pb.GetImportRequest{Id: id}))
	t.Fatal("import did not finish", r)
	return nil
}
func TestAcquisitionFlowsThroughOAuthMCPAndAllNativeProtocols(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		if os.Getenv("WAVELENGTH_REQUIRE_MEDIA_TOOLS") != "" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	file := filepath.Join(t.TempDir(), "tone.mp3")
	if raw, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.25", file).CombinedOutput(); err != nil {
		t.Fatal(err, string(raw))
	}
	audio, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	source := &ingestSource{audio: audio}
	s := acquisitionFixture(t, source)
	registration := register(t, s, "none")
	alice := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeIngest+" "+auth.ScopeWrite+" "+auth.ScopeStream, "subject-1")
	readonly := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeWrite, "subject-1")
	bob := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeIngest, "subject-2")
	mcpAlice, mcpRead, mcpBob := mcpClient(t, s, alice.Access), mcpClient(t, s, readonly.Access), mcpClient(t, s, bob.Access)
	listing, err := mcpAlice.ListTools(context.Background(), nil)
	if err != nil || len(listing.Tools) != 32 {
		t.Fatal(listing, err)
	}
	for _, tool := range listing.Tools {
		if tool.Name == "search_music" && (tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint) {
			t.Fatal("external discovery marked local-only")
		}
	}
	found := tool(t, mcpAlice, "search_music", map[string]any{"query": "Imported Artist"}, false)
	if len(found["tracks"].([]any)) != 1 {
		t.Fatal(found)
	}
	tool(t, mcpAlice, "get_music_artist", map[string]any{"ref": externalRef}, false)
	tool(t, mcpAlice, "get_music_release", map[string]any{"ref": externalRef}, false)
	tool(t, mcpAlice, "find_audio_sources", map[string]any{"metadata_ref": externalRef}, false)
	input := map[string]any{"request_id": "cross-transport", "items": []any{map[string]any{"metadata_ref": externalRef, "source_ref": audioRef}}}
	tool(t, mcpRead, "import_music", input, true)
	created := tool(t, mcpAlice, "import_music", input, false)
	id := created["id"].(string)
	client := wavelengthv1connect.NewIngestionServiceClient(s.Client(), s.URL)
	job := waitImport(t, client, alice.Access, id)
	matched := tool(t, mcpRead, "lookup_imports", map[string]any{"metadata_refs": []discovery.Ref{externalRef}}, false)["tracks"].([]any)
	if len(matched) != 1 || matched[0].(map[string]any)["track_id"] != job.Items[0].TrackId {
		t.Fatal("MCP lookup did not match the completed import", matched)
	}
	tool(t, mcpBob, "get_import", map[string]any{"id": id}, true)
	tool(t, mcpBob, "cancel_import", map[string]any{"id": id}, true)
	tool(t, mcpRead, "retry_import", map[string]any{"id": id}, true)
	tool(t, mcpRead, "cancel_import", map[string]any{"id": id}, true)
	if _, err = client.GetImport(context.Background(), rpcRequest(bob.Access, &pb.GetImportRequest{Id: id})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatal("native job leaked", err)
	}
	tool(t, mcpAlice, "list_imports", map[string]any{}, false)
	tool(t, mcpAlice, "get_playback_info", map[string]any{"id": job.Items[0].TrackId}, false)
	tls := httptest.NewUnstartedServer(s.Config.Handler)
	tls.EnableHTTP2 = true
	tls.StartTLS()
	defer tls.Close()
	for protocol, options := range [][]connect.ClientOption{nil, {connect.WithProtoJSON()}, {connect.WithGRPCWeb()}, {connect.WithGRPC()}} {
		d := wavelengthv1connect.NewDiscoveryServiceClient(tls.Client(), tls.URL, options...)
		result, err := d.Search(context.Background(), rpcRequest(alice.Access, &pb.SearchMusicRequest{Query: "Imported"}))
		if err != nil || len(result.Msg.Tracks) != 1 {
			t.Fatal(result, err)
		}
		albums, err := d.GetReleases(context.Background(), rpcRequest(readonly.Access, &pb.GetMusicReleasesRequest{Refs: []*pb.ProviderRef{{Provider: "itunes", Id: "789"}}}))
		if err != nil || len(albums.Msg.Releases) != 1 || albums.Msg.Releases[0].Tracks[0].Ref.Id != externalRef.ID {
			t.Fatal("batch release lookup differs across protocols", albums, err)
		}
		imports := wavelengthv1connect.NewIngestionServiceClient(tls.Client(), tls.URL, options...)
		queued, err := imports.CreateImport(context.Background(), rpcRequest(alice.Access, &pb.CreateImportRequest{RequestId: "queued-preview-" + strconv.Itoa(protocol), Items: []*pb.ImportSelection{{MetadataRef: &pb.ProviderRef{Provider: "itunes", Id: externalRef.ID}, SourceRef: &pb.ProviderRef{Provider: "youtube", Id: audioRef.ID}}}}))
		if err != nil || queued.Msg.Items[0].Metadata == nil || queued.Msg.Items[0].Metadata.Title != "Imported Tone" {
			t.Fatal("queued receipt omitted discovered metadata", queued, err)
		}
		matches, err := imports.LookupImports(context.Background(), rpcRequest(readonly.Access, &pb.LookupImportsRequest{MetadataRefs: []*pb.ProviderRef{{Provider: "itunes", Id: "123"}, {Provider: "itunes", Id: "789"}}}))
		if err != nil || len(matches.Msg.Tracks) != 1 || matches.Msg.Tracks[0].TrackId != job.Items[0].TrackId {
			t.Fatal("exact import lookup differs across protocols", matches, err)
		}
		origins, err := imports.LookupImports(context.Background(), rpcRequest(readonly.Access, &pb.LookupImportsRequest{TrackIds: []string{job.Items[0].TrackId}}))
		if err != nil || len(origins.Msg.Tracks) != 1 || origins.Msg.Tracks[0].MetadataRef.Id != externalRef.ID {
			t.Fatal("saved track provenance differs across protocols", origins, err)
		}
		resultJob, err := imports.GetImport(context.Background(), rpcRequest(alice.Access, &pb.GetImportRequest{Id: id}))
		if err != nil || resultJob.Msg.Items[0].TrackId != job.Items[0].TrackId {
			t.Fatal(resultJob, err)
		}
		req := &pb.CreateImportRequest{RequestId: "cross-transport", Items: []*pb.ImportSelection{{MetadataRef: &pb.ProviderRef{Provider: "itunes", Id: "123"}, SourceRef: &pb.ProviderRef{Provider: "youtube", Id: audioRef.ID}}}}
		replay, err := imports.CreateImport(context.Background(), rpcRequest(alice.Access, req))
		if err != nil || replay.Msg.Id != id {
			t.Fatal("idempotency differs across transports", replay, err)
		}
		if _, err = imports.CreateImport(context.Background(), rpcRequest(readonly.Access, req)); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatal("missing ingestion scope accepted", err)
		}
		if _, err = imports.CreateImport(context.Background(), rpcRequest("test-native", req)); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatal("shared token became an import owner", err)
		}
	}
	playback := requestBody(t, s.Client(), "GET", s.URL+"/media/tracks/"+job.Items[0].TrackId, "", nil, map[string]string{"Authorization": "Bearer " + alice.Access, "Range": "bytes=0-15"})
	got, err := io.ReadAll(playback.Body)
	if err != nil || playback.StatusCode != http.StatusPartialContent || string(got) != string(audio[:16]) {
		t.Fatal("import is not playable", playback.StatusCode, err)
	}
	if source.downloads.Load() != 1 {
		t.Fatal("duplicate acquisition", source.downloads.Load())
	}
	// A failed source can be replaced through MCP after the job was created by RPC.
	failed, err := client.CreateImport(context.Background(), rpcRequest(alice.Access, &pb.CreateImportRequest{RequestId: "failed-source", Items: []*pb.ImportSelection{{MetadataRef: &pb.ProviderRef{Provider: "itunes", Id: "456"}, SourceRef: &pb.ProviderRef{Provider: "youtube", Id: "abcdefghijk"}}}}))
	if err != nil {
		t.Fatal(err)
	}
	waitImport(t, client, alice.Access, failed.Msg.Id, ingest.Failed)
	tool(t, mcpAlice, "retry_import", map[string]any{"id": failed.Msg.Id, "overrides": []any{map[string]any{"index": 0, "source_ref": audioRef}}}, false)
	waitImport(t, client, alice.Access, failed.Msg.Id)
	// Release expansion and ordered playlist attachment use the same completed ID.
	playlist := tool(t, mcpAlice, "create_playlist", map[string]any{"name": "Imported album"}, false)["playlist"].(map[string]any)
	release := tool(t, mcpAlice, "import_music", map[string]any{"request_id": "album", "release_ref": externalRef, "playlist_id": playlist["id"]}, false)
	waitImport(t, client, alice.Access, release["id"].(string))
	saved := tool(t, mcpAlice, "get_playlist", map[string]any{"id": playlist["id"]}, false)["playlist"].(map[string]any)
	if saved["track_ids"].([]any)[0] != job.Items[0].TrackId {
		t.Fatal(saved)
	}
	oldPlayback := requestBody(t, s.Client(), "GET", s.URL+"/media/tracks/"+job.Items[0].TrackId, "", nil, map[string]string{"Authorization": "Bearer " + alice.Access})
	oldTag := oldPlayback.Header.Get("ETag")
	oldPlayback.Body.Close()
	replace := &pb.CreateImportRequest{RequestId: "replace-audio", Items: []*pb.ImportSelection{{MetadataRef: &pb.ProviderRef{Provider: "itunes", Id: externalRef.ID}, SourceRef: &pb.ProviderRef{Provider: "youtube", Id: audioRef.ID}, ReplaceTrackId: job.Items[0].TrackId}}}
	if _, err = client.CreateImport(context.Background(), rpcRequest(readonly.Access, replace)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("read-only listener could replace audio", err)
	}
	replacing, err := client.CreateImport(context.Background(), rpcRequest(alice.Access, replace))
	if err != nil {
		t.Fatal(err)
	}
	replaced := waitImport(t, client, alice.Access, replacing.Msg.Id)
	if replaced.Items[0].TrackId != job.Items[0].TrackId || replaced.Items[0].Reused {
		t.Fatal("replacement did not retain its saved identity", replaced)
	}
	newPlayback := requestBody(t, s.Client(), "GET", s.URL+"/media/tracks/"+job.Items[0].TrackId, "", nil, map[string]string{"Authorization": "Bearer " + alice.Access, "If-None-Match": oldTag})
	defer newPlayback.Body.Close()
	if newPlayback.StatusCode != http.StatusOK || newPlayback.Header.Get("ETag") == oldTag {
		t.Fatal("replaced audio reused a stale HTTP validator", newPlayback.StatusCode, newPlayback.Header)
	}
	provenance := tool(t, mcpRead, "lookup_imports", map[string]any{"track_ids": []string{job.Items[0].TrackId}}, false)["tracks"].([]any)
	if len(provenance) != 1 || provenance[0].(map[string]any)["metadata_ref"].(map[string]any)["id"] != externalRef.ID {
		t.Fatal(provenance)
	}

}
func TestMCPImportCancellationAndSourceOverride(t *testing.T) {
	source := &ingestSource{block: true, started: make(chan struct{}, 1)}
	s := acquisitionFixture(t, source)
	registration := register(t, s, "none")
	grant := grant(t, s, registration, auth.ScopeRead+" "+auth.ScopeIngest, "subject-1")
	client := mcpClient(t, s, grant.Access)
	created := tool(t, client, "import_music", map[string]any{"request_id": "cancel", "items": []any{map[string]any{"metadata_ref": externalRef, "source_ref": audioRef}}}, false)
	id := created["id"].(string)
	select {
	case <-source.started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}
	result := tool(t, client, "cancel_import", map[string]any{"id": id}, false)
	if result["state"] != ingest.Cancelled {
		t.Fatal(result)
	}
	// Retry has its own authorization and schema, and cannot address an invalid item.
	tool(t, client, "retry_import", map[string]any{"id": id, "overrides": []any{map[string]any{"index": -1, "source_ref": audioRef}}}, true)
	tool(t, client, "get_import", map[string]any{"id": id}, false)
}
