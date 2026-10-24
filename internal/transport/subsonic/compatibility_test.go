package subsonic_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/transport/subsonic"
	"github.com/prayujt/wavelength/internal/userdata"
)

type fixture struct {
	lib         *library.Library
	db          *sql.DB
	server      *http.Server
	data, music string
}

func newFixture(t *testing.T, data, music string) *fixture {
	t.Helper()
	if data == "" {
		data = t.TempDir()
	}
	if music == "" {
		music = t.TempDir()
		writeSong(t, music, "A/First/one.mp3", "One", "Artist A", "First", 1)
		writeSong(t, music, "A/First/two.mp3", "Two", "Artist A", "First", 2)
		writeSong(t, music, "B/Second/three.mp3", "Three", "Artist B", "Second", 1)
	}
	db, err := database.Open(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lib.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	dependencies, close := testDependencies(ctx, lib, database.StateStore{DB: db})
	server, err := subsonic.New(ctx, subsonic.Config{Username: "admin", Password: "secret"}, dependencies)
	if err != nil {
		close()
		cancel()
		lib.Close()
		db.Close()
		t.Fatal(err)
	}
	f := &fixture{lib: lib, db: db, server: server, data: data, music: music}
	t.Cleanup(func() { cancel(); close(); lib.Close(); db.Close() })
	return f
}
func writeSong(t *testing.T, root, path, title, artist, album string, track byte) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 256)
	copy(data[128:], "TAG")
	copy(data[131:161], title)
	copy(data[161:191], artist)
	copy(data[191:221], album)
	copy(data[221:225], "2020")
	data[254] = track
	data[255] = 17
	if err := os.WriteFile(full, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) raw(t *testing.T, endpoint string, args url.Values) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{"u": {"admin"}, "p": {"secret"}, "v": {"1.16.1"}, "c": {"tests"}, "f": {"json"}}
	for key, value := range args {
		values[key] = value
	}
	r := httptest.NewRequest("GET", "/rest/"+endpoint+".view?"+values.Encode(), nil)
	w := httptest.NewRecorder()
	f.server.Handler.ServeHTTP(w, r)
	return w
}
func (f *fixture) call(t *testing.T, endpoint string, args url.Values) map[string]any {
	t.Helper()
	w := f.raw(t, endpoint, args)
	var outer map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &outer); err != nil {
		t.Fatalf("%s: %s (%v)", endpoint, w.Body, err)
	}
	return outer["subsonic-response"]
}
func (f *fixture) ok(t *testing.T, endpoint string, args url.Values) map[string]any {
	t.Helper()
	body := f.call(t, endpoint, args)
	if body["status"] != "ok" {
		t.Fatalf("%s: %v", endpoint, body)
	}
	return body
}
func expectCode(t *testing.T, body map[string]any, code float64) {
	t.Helper()
	if body["status"] != "failed" || body["error"].(map[string]any)["code"] != code {
		t.Fatal(body)
	}
}
func list(body map[string]any, parent, key string) []any {
	value := body[parent].(map[string]any)[key]
	if value == nil {
		return []any{}
	}
	return value.([]any)
}
func (f *fixture) ids(t *testing.T) (string, string, string) {
	t.Helper()
	body := f.ok(t, "search3", url.Values{"query": {"One"}})
	s := list(body, "searchResult3", "song")[0].(map[string]any)
	return s["id"].(string), s["albumId"].(string), s["artistId"].(string)
}
func TestBrowseSearchListsAndProtocolValidation(t *testing.T) {
	f := newFixture(t, "", "")
	track, album, artist := f.ids(t)
	for _, format := range []string{"xml", "json", "jsonp"} {
		response := f.raw(t, "ping", url.Values{"f": {format}, "callback": {"app.done"}})
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
		if format == "jsonp" && !strings.HasPrefix(response.Body.String(), "app.done(") {
			t.Fatalf("JSONP: %s", response.Body)
		}
	}
	expectCode(t, f.call(t, "ping", url.Values{"v": {"2.0.0"}}), 30)
	expectCode(t, f.call(t, "ping", url.Values{"v": {"nonsense"}}), 10)
	expectCode(t, f.call(t, "ping", url.Values{"f": {"jsonp"}, "callback": {"alert(1)"}}), 10)
	search := f.ok(t, "search3", url.Values{"query": {"Artist"}, "songCount": {"0"}})
	if len(list(search, "searchResult3", "artist")) != 2 || len(list(search, "searchResult3", "album")) != 2 || len(list(search, "searchResult3", "song")) != 0 {
		t.Fatal(search)
	}
	search = f.ok(t, "search3", url.Values{"query": {""}, "songCount": {"1"}, "songOffset": {"1"}})
	if len(list(search, "searchResult3", "song")) != 1 {
		t.Fatal(search)
	}
	f.ok(t, "search2", url.Values{"query": {"First"}})
	f.ok(t, "search", url.Values{"title": {"One"}})
	genres := f.ok(t, "getGenres", nil)
	if len(list(genres, "genres", "genre")) != 1 {
		t.Fatal(genres)
	}
	for _, kind := range []string{"random", "newest", "alphabeticalByName", "alphabeticalByArtist", "byYear", "byGenre", "frequent", "recent", "starred", "highest"} {
		args := url.Values{"type": {kind}, "size": {"1"}, "fromYear": {"2019"}, "toYear": {"2021"}, "genre": {"Rock"}}
		f.ok(t, "getAlbumList", args)
		if kind != "highest" {
			f.ok(t, "getAlbumList2", args)
		}
	}
	f.ok(t, "getRandomSongs", url.Values{"genre": {"Rock"}, "size": {"2"}})
	f.ok(t, "getSongsByGenre", url.Values{"genre": {"Rock"}})
	expectCode(t, f.call(t, "getAlbums", nil), 70)
	expectCode(t, f.call(t, "getAlbum", url.Values{"id": {"missing"}}), 70)
	expectCode(t, f.call(t, "getArtists", url.Values{"musicFolderId": {"2"}}), 70)
	indexes := f.ok(t, "getIndexes", nil)
	indices := list(indexes, "indexes", "index")
	if len(indices) == 0 {
		t.Fatal(indexes)
	}
	folder := indices[0].(map[string]any)["artist"].([]any)[0].(map[string]any)["id"].(string)
	f.ok(t, "getMusicDirectory", url.Values{"id": {folder}})
	f.ok(t, "getMusicDirectory", url.Values{"id": {album}})
	f.ok(t, "getArtistInfo2", url.Values{"id": {artist}})
	f.ok(t, "getAlbumInfo2", url.Values{"id": {album}})
	f.ok(t, "getSimilarSongs2", url.Values{"id": {artist}})
	if !strings.Contains(f.raw(t, "getSong", url.Values{"f": {"xml"}, "id": {track}}).Body.String(), `genre="Rock"`) {
		t.Fatal("genre missing from XML")
	}
}
func TestPersistentPlaylistsHistoryAndQueue(t *testing.T) {
	f := newFixture(t, "", "")
	track, _, _ := f.ids(t)
	created := f.ok(t, "createPlaylist", url.Values{"name": {"Road"}, "songId": {track, track}})["playlist"].(map[string]any)
	id := created["id"].(string)
	f.ok(t, "updatePlaylist", url.Values{"playlistId": {id}, "songIndexToRemove": {"0"}, "comment": {"Road trip"}})
	f.ok(t, "scrobble", url.Values{"id": {track}})
	f.ok(t, "scrobble", url.Values{"id": {track}, "submission": {"false"}})
	if len(list(f.ok(t, "getNowPlaying", nil), "nowPlaying", "entry")) != 1 {
		t.Fatal("missing now playing")
	}
	f.ok(t, "createBookmark", url.Values{"id": {track}, "position": {"12000"}, "comment": {"Resume"}})
	f.ok(t, "savePlayQueue", url.Values{"id": {track}, "current": {track}, "position": {"12000"}})
	f.lib.Close()
	f.db.Close()
	f = newFixture(t, f.data, f.music)
	if len(list(f.ok(t, "getPlaylist", url.Values{"id": {id}}), "playlist", "entry")) != 1 {
		t.Fatal("playlist lost on restart")
	}
	if len(list(f.ok(t, "getBookmarks", nil), "bookmarks", "bookmark")) != 1 {
		t.Fatal("bookmark lost")
	}
	if len(list(f.ok(t, "getPlayQueue", nil), "playQueue", "entry")) != 1 {
		t.Fatal("queue lost")
	}
	if f.ok(t, "getSong", url.Values{"id": {track}})["song"].(map[string]any)["playCount"] != float64(1) {
		t.Fatal("listening history lost")
	}
	f.ok(t, "deleteBookmark", url.Values{"id": {track}})
	f.ok(t, "deletePlaylist", url.Values{"id": {id}})
	expectCode(t, f.call(t, "getPlaylist", url.Values{"id": {id}}), 70)
}
func TestArtworkLyricsDownloadAndScanning(t *testing.T) {
	f := newFixture(t, "", "")
	track, _, _ := f.ids(t)
	file, err := os.Create(filepath.Join(f.music, "A/First/cover.png"))
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 80, 40))
	picture.Set(0, 0, color.White)
	if err = png.Encode(file, picture); err != nil {
		t.Fatal(err)
	}
	file.Close()
	cover := f.raw(t, "getCoverArt", url.Values{"id": {track}, "size": {"16"}})
	config, _, err := image.DecodeConfig(bytes.NewReader(cover.Body.Bytes()))
	if err != nil || config.Width != 16 || config.Height != 8 {
		t.Fatalf("cover %v %v %s", config, err, cover.Body)
	}
	if err = os.WriteFile(filepath.Join(f.music, "A/First/one.lrc"), []byte("[00:01.00]Actual lyrics"), 0600); err != nil {
		t.Fatal(err)
	}
	lyrics := f.ok(t, "getLyrics", url.Values{"artist": {"Artist A"}, "title": {"One"}})
	if !strings.Contains(lyrics["lyrics"].(map[string]any)["value"].(string), "Actual lyrics") {
		t.Fatal(lyrics)
	}
	download := f.raw(t, "download", url.Values{"id": {track}})
	if download.Header().Get("Content-Disposition") == "" || download.Body.Len() != 256 {
		t.Fatal("download failed")
	}
	f.ok(t, "startScan", nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		status := f.ok(t, "getScanStatus", nil)["scanStatus"].(map[string]any)
		if !status["scanning"].(bool) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scan did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := f.ok(t, "getScanStatus", nil)["scanStatus"].(map[string]any)
	if status["count"] != float64(3) {
		t.Fatal(status)
	}
	if _, err = io.Copy(io.Discard, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
}

// Optional export consumed by scripts/validate-subsonic.py's independent XSD validator.
func TestExportXMLContractFixtures(t *testing.T) {
	dir := os.Getenv("WAVELENGTH_XML_FIXTURES")
	if dir == "" {
		t.Skip("XML export is requested by the schema-validation script")
	}
	f := newFixture(t, "", "")
	track, album, artist := f.ids(t)
	playlist := f.ok(t, "createPlaylist", url.Values{"name": {"Contract"}, "songId": {track}})["playlist"].(map[string]any)["id"].(string)
	f.ok(t, "star", url.Values{"id": {track}, "albumId": {album}, "artistId": {artist}})
	f.ok(t, "scrobble", url.Values{"id": {track}, "submission": {"false"}})
	f.ok(t, "createBookmark", url.Values{"id": {track}, "position": {"0"}})
	f.ok(t, "savePlayQueue", url.Values{"id": {track}, "current": {track}})
	f.ok(t, "addChatMessage", url.Values{"message": {"Contract"}})
	f.ok(t, "createInternetRadioStation", url.Values{"name": {"Contract"}, "streamUrl": {"https://radio.example.test"}})
	cases := map[string]url.Values{
		"ping": {}, "getLicense": {}, "getMusicFolders": {}, "getArtists": {}, "getArtist": {"id": {artist}}, "getAlbum": {"id": {album}}, "getSong": {"id": {track}},
		"getIndexes": {}, "getMusicDirectory": {"id": {"1"}}, "getGenres": {}, "getAlbumList": {"type": {"alphabeticalByName"}}, "getAlbumList2": {"type": {"alphabeticalByName"}}, "getRandomSongs": {}, "getSongsByGenre": {"genre": {"Rock"}},
		"search": {"any": {"One"}}, "search2": {"query": {"Artist"}}, "search3": {"query": {"Artist"}}, "getStarred": {}, "getStarred2": {},
		"getArtistInfo": {"id": {artist}}, "getArtistInfo2": {"id": {artist}}, "getAlbumInfo": {"id": {album}}, "getAlbumInfo2": {"id": {album}}, "getSimilarSongs": {"id": {track}}, "getSimilarSongs2": {"id": {artist}}, "getTopSongs": {"artist": {"Artist A"}},
		"getUser": {"username": {"admin"}}, "getUsers": {}, "getPlaylists": {}, "getPlaylist": {"id": {playlist}}, "getNowPlaying": {}, "getBookmarks": {}, "getPlayQueue": {}, "getInternetRadioStations": {}, "getChatMessages": {}, "getScanStatus": {}, "getShares": {}, "getPodcasts": {}, "getNewestPodcasts": {}, "getVideos": {}, "getVideoInfo": {"id": {"missing"}},
	}
	for endpoint, args := range cases {
		args.Set("f", "xml")
		response := f.raw(t, endpoint, args)
		if err := os.WriteFile(filepath.Join(dir, endpoint+".xml"), response.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func testDependencies(ctx context.Context, lib *library.Library, store state.Store) (subsonic.Dependencies, func()) {
	transcoder := transcode.New(ctx, lib)
	return subsonic.Dependencies{Library: lib, UserData: userdata.New(store, lib), Transcoder: transcoder}, transcoder.Close
}
