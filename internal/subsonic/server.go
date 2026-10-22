package subsonic

import (
	"context"
	"crypto/md5" // Required only for the legacy Subsonic token scheme.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/playback"
)

// New constructs a legacy-only listener. Credentials must be nonempty.
func New(addr string, lib *library.Library, user, password string) (*http.Server, error) {
	if user == "" || password == "" {
		return nil, errors.New("legacy Subsonic requires a username and password")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := response{XMLNS: "http://subsonic.org/restapi", Status: "ok", Version: "1.16.1", Type: "wavelength", ServerVersion: "0.1.0"}
		fail := func(code int, message string) {
			result.Status = "failed"
			result.Error = &apiError{Code: code, Message: message}
		}
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if result.Status == "streamed" {
				return
			}
			if r.Form.Get("f") == "json" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": result})
			} else {
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				_, _ = w.Write([]byte(xml.Header))
				_ = xml.NewEncoder(w).Encode(result)
			}
		}()
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			fail(10, "Invalid request parameters")
			return
		}
		if r.Method != "GET" && r.Method != "POST" && r.Method != "HEAD" {
			fail(0, "Unsupported HTTP method")
			return
		}
		if format := r.Form.Get("f"); format != "" && format != "xml" && format != "json" {
			fail(0, "Unsupported response format")
			return
		}
		if !authenticated(r, user, password) {
			fail(40, "Wrong username or password")
			return
		}
		if r.Form.Get("v") == "" || r.Form.Get("c") == "" {
			fail(10, "Parameters v and c are required")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/rest/") {
			fail(70, "Endpoint not found")
			return
		}
		endpoint := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/rest/"), ".view")
		switch endpoint {
		case "ping":
		case "getLicense":
			result.License = &license{Valid: true}
		case "getMusicFolders":
			result.Folders = &folders{Folders: []folder{{ID: "1", Name: "Music"}}}
		case "stream":
			if r.Form.Get("id") == "" {
				fail(10, "Parameter id is required")
				return
			}
			if err := playback.Stream(w, r, lib, r.Form.Get("id")); err != nil {
				legacyError(&result, err)
				return
			}
			result.Status = "streamed"
		case "getSong":
			if r.Form.Get("id") == "" {
				fail(10, "Parameter id is required")
				return
			}
			track, err := lib.Get(r.Context(), r.Form.Get("id"))
			if err != nil {
				legacyError(&result, err)
				return
			}
			value := asSong(track)
			result.Song = &value
		case "search3":
			count, err := number(r, "songCount", 20, 500)
			if err != nil {
				fail(10, err.Error())
				return
			}
			offset, err := number(r, "songOffset", 0, int(^uint(0)>>1))
			if err != nil {
				fail(10, err.Error())
				return
			}
			songs := []song{}
			if count > 0 {
				page, err := lib.List(r.Context(), library.Query{Search: r.Form.Get("query"), Limit: count, Offset: offset})
				if err != nil {
					legacyError(&result, err)
					return
				}
				for _, track := range page.Tracks {
					songs = append(songs, asSong(track))
				}
			}
			result.Search = &searchResult{Songs: songs}
		case "getArtists", "getArtist", "getAlbum":
			if endpoint != "getArtists" && r.Form.Get("id") == "" {
				fail(10, "Parameter id is required")
				return
			}
			all, err := catalog(r.Context(), lib)
			if err != nil {
				legacyError(&result, err)
				return
			}
			switch endpoint {
			case "getArtists":
				list := []artist{}
				for _, a := range all {
					a.Albums = nil
					list = append(list, a)
				}
				result.Artists = &artists{Index: []artistIndex{{Name: "All", Artists: list}}}
			case "getArtist":
				for _, a := range all {
					if a.ID == r.Form.Get("id") {
						for i := range a.Albums {
							a.Albums[i].Songs = nil
						}
						result.Artist = &a
						break
					}
				}
				if result.Artist == nil {
					fail(70, "Artist not found")
				}
			case "getAlbum":
				for _, a := range all {
					for _, album := range a.Albums {
						if album.ID == r.Form.Get("id") {
							result.Album = &album
							break
						}
					}
				}
				if result.Album == nil {
					fail(70, "Album not found")
				}
			}
		default:
			fail(70, "Endpoint not implemented")
		}
	})
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}, nil
}

func authenticated(r *http.Request, user, password string) bool {
	if !equal(r.Form.Get("u"), user) {
		return false
	}
	if token := r.Form.Get("t"); token != "" {
		salt := r.Form.Get("s")
		if salt == "" {
			return false
		}
		expected := fmt.Sprintf("%x", md5.Sum([]byte(password+salt)))
		return equal(token, expected)
	}
	value := r.Form.Get("p")
	if strings.HasPrefix(value, "enc:") {
		decoded, err := hex.DecodeString(strings.TrimPrefix(value, "enc:"))
		if err != nil {
			return false
		}
		value = string(decoded)
	}
	return equal(value, password)
}
func equal(a, b string) bool {
	x := sha256.Sum256([]byte(a))
	y := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func id(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }
func names(t library.Track) (string, string) {
	artist := t.AlbumArtist
	if artist == "" {
		artist = t.Artist
	}
	if artist == "" {
		artist = "Unknown Artist"
	}
	album := t.Album
	if album == "" {
		album = "Unknown Album"
	}
	return artist, album
}
func asSong(t library.Track) song {
	artist, album := names(t)
	artistID := id("artist\x00" + artist)
	albumID := id("album\x00" + artist + "\x00" + album)
	return song{ID: t.ID, Parent: albumID, Title: t.Title, Artist: t.Artist, Album: album, ArtistID: artistID, AlbumID: albumID, Track: t.TrackNumber, Year: t.Year, Size: t.Size, ContentType: t.ContentType, Suffix: strings.TrimPrefix(strings.ToLower(filepath.Ext(t.Path)), "."), Type: "music"}
}
func catalog(ctx context.Context, lib *library.Library) ([]artist, error) {
	result := []artist{}
	artists := map[string]int{}
	albums := map[string][2]int{}
	for offset := 0; ; offset += 500 {
		page, err := lib.List(ctx, library.Query{Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, track := range page.Tracks {
			artistName, albumName := names(track)
			item := asSong(track)
			ai, ok := artists[item.ArtistID]
			if !ok {
				ai = len(result)
				artists[item.ArtistID] = ai
				result = append(result, artist{ID: item.ArtistID, Name: artistName, Albums: []album{}})
			}
			location, ok := albums[item.AlbumID]
			if !ok {
				location = [2]int{ai, len(result[ai].Albums)}
				albums[item.AlbumID] = location
				result[ai].Albums = append(result[ai].Albums, album{ID: item.AlbumID, Name: albumName, Artist: artistName, ArtistID: item.ArtistID, Songs: []song{}})
				result[ai].AlbumCount++
			}
			a := &result[location[0]].Albums[location[1]]
			a.Songs = append(a.Songs, item)
			a.SongCount++
		}
		if len(page.Tracks) < 500 {
			break
		}
	}
	return result, nil
}
func number(r *http.Request, key string, fallback, max int) (int, error) {
	value := r.Form.Get(key)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > max {
		return 0, fmt.Errorf("Invalid %s", key)
	}
	return n, nil
}
func legacyError(result *response, err error) {
	result.Status = "failed"
	result.Error = &apiError{Code: 0, Message: "Library operation failed"}
	if errors.Is(err, library.ErrNotFound) {
		result.Error = &apiError{Code: 70, Message: "Track not found"}
		return
	}
	slog.Error("legacy library operation failed", "error", err)
}
