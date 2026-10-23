package rpc_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/httpserver"
	"github.com/prayujt/wavelength/internal/library"
)

func TestNativeRPCProtocolsAndAudio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	music := t.TempDir()
	audio := bytes.Repeat([]byte("RIFF-wavelength-audio-"), 10000)
	if err = os.WriteFile(filepath.Join(music, "tone.wav"), audio, 0600); err != nil {
		t.Fatal(err)
	}
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if _, err = lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.New(ctx, auth.Config{Token: "native-secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpserver.New("", db.PingContext, httpserver.WithAuth(manager), httpserver.WithNative(lib, manager)).Handler)
	defer server.Close()
	for _, options := range [][]connect.ClientOption{nil, {connect.WithProtoJSON()}, {connect.WithGRPCWeb()}} {
		client := wavelengthv1connect.NewLibraryServiceClient(server.Client(), server.URL, options...)
		request := connect.NewRequest(&pb.ListTracksRequest{})
		request.Header().Set("Authorization", "Bearer native-secret")
		response, err := client.ListTracks(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if response.Msg.Total != 1 || response.Msg.Tracks[0].Title != "tone" {
			t.Fatal(response.Msg)
		}
		if _, err = client.ListTracks(ctx, connect.NewRequest(&pb.ListTracksRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("authentication: %v", err)
		}
	}
	// Native Connect client: binary protobuf over ordinary HTTP, including server streaming.
	client := wavelengthv1connect.NewLibraryServiceClient(server.Client(), server.URL)
	call := func(message *pb.ListTracksRequest) *connect.Request[pb.ListTracksRequest] {
		request := connect.NewRequest(message)
		request.Header().Set("Authorization", "Bearer native-secret")
		return request
	}
	listing, err := client.ListTracks(ctx, call(&pb.ListTracksRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	id := listing.Msg.Tracks[0].Id
	if _, err = client.GetTrack(ctx, authorizedRequest(&pb.GetTrackRequest{Id: "missing"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("not found: %v", err)
	}
	if _, err = client.ListTracks(ctx, call(&pb.ListTracksRequest{Limit: -1})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("validation: %v", err)
	}
	artists, err := client.ListArtists(ctx, authorizedRequest(&pb.ListArtistsRequest{}))
	if err != nil || len(artists.Msg.Artists) != 1 {
		t.Fatalf("artists %v %v", artists, err)
	}
	artist, err := client.GetArtist(ctx, authorizedRequest(&pb.GetArtistRequest{Id: artists.Msg.Artists[0].Id}))
	if err != nil || len(artist.Msg.Albums) != 1 {
		t.Fatalf("artist %v %v", artist, err)
	}
	album, err := client.GetAlbum(ctx, authorizedRequest(&pb.GetAlbumRequest{Id: artist.Msg.Albums[0].Id}))
	if err != nil || len(album.Msg.Tracks) != 1 {
		t.Fatalf("album %v %v", album, err)
	}
	playback, err := client.GetPlaybackInfo(ctx, authorizedRequest(&pb.GetPlaybackInfoRequest{Id: id}))
	if err != nil || !playback.Msg.SupportsRanges {
		t.Fatalf("playback %v %v", playback, err)
	}
	stream, err := client.StreamAudio(ctx, authorizedRequest(&pb.StreamAudioRequest{Id: id, OffsetBytes: 11, LengthBytes: 140000}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var actual []byte
	offset := int64(11)
	for stream.Receive() {
		chunk := stream.Msg()
		if chunk.OffsetBytes != offset || len(chunk.Data) > 64<<10 {
			t.Fatal("invalid stream framing")
		}
		actual = append(actual, chunk.Data...)
		offset += int64(len(chunk.Data))
	}
	if err = stream.Err(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, audio[11:140011]) {
		t.Fatal("Connect audio bytes differ")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+playback.Msg.HttpPath, nil)
	req.Header.Set("Authorization", "Bearer native-secret")
	req.Header.Set("Range", "bytes=4-20")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 206 || !bytes.Equal(body, audio[4:21]) {
		t.Fatal("HTTP seeking failed")
	}
	// JSON REST controls are no longer the main native interface.
	response, err = server.Client().Get(server.URL + "/api/tracks")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("old REST control route unexpectedly exposed")
	}
}

func authorizedRequest[T any](message *T) *connect.Request[T] {
	request := connect.NewRequest(message)
	request.Header().Set("Authorization", "Bearer native-secret")
	return request
}
