package subsonic

import (
	"context"
	"crypto/md5" // Required only for the legacy Subsonic token scheme.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
func New(addr string, lib *library.Library, user, password string, options ...Option) (*http.Server, error) {
	if user == "" || password == "" {
		return nil, errors.New("legacy Subsonic requires a username and password")
	}
	api := newAPI(lib, user, password)
	for _, option := range options {
		if err := option(api); err != nil {
			return nil, err
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := response{XMLNS: "http://subsonic.org/restapi", Status: "ok", Version: "1.16.1", Type: "wavelength", ServerVersion: "0.1.0"}
		fail := func(code int, message string) {
			result.Status = "failed"
			result.Error = &apiError{Code: code, Message: message}
		}
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if result.Status != "streamed" {
				writeResponse(w, r, result)
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
		if format := r.Form.Get("f"); format != "" && format != "xml" && format != "json" && format != "jsonp" {
			fail(0, "Unsupported response format")
			return
		}
		current, ok, authErr := api.authenticate(r)
		if authErr != nil {
			failure(&result, 0, "Credential storage unavailable")
			return
		}
		if !ok {
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
		if err := validateVersion(r.Form.Get("v")); err != nil {
			fail(err.Code, err.Message)
			return
		}
		if r.Form.Get("f") == "jsonp" && !callbackPattern.MatchString(r.Form.Get("callback")) {
			fail(10, "Invalid callback")
			return
		}
		if !api.authorized(r, endpoint, current, &result) {
			return
		}
		if api.handle(w, r, endpoint, &result) {
			return
		}
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
	created := t.CreatedAt
	if created.IsZero() {
		created = t.ModifiedAt
	}
	return song{Duration: int(t.Duration + 0.5), Genre: t.Genre, BitRate: t.BitRate, DiscNumber: t.DiscNumber, Created: created.UTC().Format(time.RFC3339), CoverArt: t.ID, ID: t.ID, Parent: albumID, Title: t.Title, Artist: t.Artist, Album: album, ArtistID: artistID, AlbumID: albumID, Track: t.TrackNumber, Year: t.Year, Size: t.Size, ContentType: t.ContentType, Suffix: strings.TrimPrefix(strings.ToLower(filepath.Ext(t.Path)), "."), Type: "music"}
}
func catalog(ctx context.Context, lib *library.Library) ([]artist, error) {
	snapshot, err := lib.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	out := []artist{}
	for _, entry := range snapshot.Artists {
		art := artist{ID: entry.ID, Name: entry.Name, AlbumCount: len(entry.Albums), Albums: []album{}}
		for _, source := range entry.Albums {
			value := album{ID: source.ID, Name: source.Name, Artist: source.Artist, ArtistID: source.ArtistID, Year: source.Year, Genre: source.Genre, Created: source.CreatedAt.UTC().Format(time.RFC3339), SongCount: len(source.Tracks), Duration: int(source.Duration + 0.5), Songs: []song{}}
			for _, track := range source.Tracks {
				value.Songs = append(value.Songs, asSong(track))
			}
			if len(value.Songs) > 0 {
				value.CoverArt = value.Songs[0].ID
				art.CoverArt = value.CoverArt
			}
			art.Albums = append(art.Albums, value)
		}
		out = append(out, art)
	}
	return out, nil
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
