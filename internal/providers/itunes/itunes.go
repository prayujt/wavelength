// Package itunes reads Apple's public music metadata API.
package itunes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"golang.org/x/time/rate"
)

type Client struct {
	http    *http.Client
	base    string
	limiter *rate.Limiter
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second}, base: "https://itunes.apple.com", limiter: rate.NewLimiter(rate.Every(time.Minute/18), 3)}
}

type entry struct {
	Kind     string `json:"wrapperType"`
	ArtistID int64  `json:"artistId"`
	AlbumID  int64  `json:"collectionId"`
	TrackID  int64  `json:"trackId"`
	Artist   string `json:"artistName"`
	Album    string `json:"collectionName"`
	Title    string `json:"trackName"`
	Date     string `json:"releaseDate"`
	Number   int    `json:"trackNumber"`
	Disc     int    `json:"discNumber"`
	Millis   int    `json:"trackTimeMillis"`
	Artwork  string `json:"artworkUrl100"`
	Genre    string `json:"primaryGenreName"`
}

func ref(id int64) discovery.Ref {
	return discovery.Ref{Provider: "itunes", ID: strconv.FormatInt(id, 10)}
}
func track(e entry) discovery.Track {
	return discovery.Track{Ref: ref(e.TrackID), Title: e.Title, Artist: e.Artist, Album: e.Album, AlbumArtist: e.Artist, ReleaseDate: e.Date, TrackNumber: e.Number, DiscNumber: e.Disc, Duration: float64(e.Millis) / 1000, ArtworkURL: strings.Replace(e.Artwork, "100x100bb", "600x600bb", 1), Genre: e.Genre}
}
func release(e entry) discovery.Release {
	return discovery.Release{Ref: ref(e.AlbumID), Title: e.Album, Artist: e.Artist, ArtworkURL: strings.Replace(e.Artwork, "100x100bb", "600x600bb", 1), Tracks: []discovery.Track{}}
}
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]entry, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, discovery.ErrUnavailable
		}
		req, err := http.NewRequestWithContext(ctx, "GET", c.base+path+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Wavelength/1.0 (+https://github.com/prayujt/wavelength)")
		response, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
		response.Body.Close()
		if response.StatusCode == 403 || response.StatusCode == 429 || response.StatusCode >= 500 {
			continue
		}
		if response.StatusCode != 200 || readErr != nil || len(raw) > 4<<20 {
			return nil, discovery.ErrUnavailable
		}
		var result struct {
			Results []entry `json:"results"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			return nil, discovery.ErrUnavailable
		}
		return result.Results, nil
	}
	return nil, discovery.ErrUnavailable
}
func (c *Client) Search(ctx context.Context, query string, limit int) (discovery.Results, error) {
	out := discovery.Results{Tracks: []discovery.Track{}, Releases: []discovery.Release{}, Artists: []discovery.Artist{}}
	for _, entity := range []string{"song", "album", "musicArtist"} {
		entries, err := c.get(ctx, "/search", url.Values{"term": {query}, "media": {"music"}, "entity": {entity}, "limit": {strconv.Itoa(limit)}})
		if err != nil {
			return out, err
		}
		for _, e := range entries {
			switch e.Kind {
			case "track":
				if e.TrackID > 0 {
					out.Tracks = append(out.Tracks, track(e))
				}
			case "collection":
				out.Releases = append(out.Releases, release(e))
			case "artist":
				out.Artists = append(out.Artists, discovery.Artist{Ref: ref(e.ArtistID), Name: e.Artist, Releases: []discovery.Release{}})
			}
		}
	}
	return out, nil
}
func (c *Client) Track(ctx context.Context, r discovery.Ref) (discovery.Track, error) {
	entries, err := c.get(ctx, "/lookup", url.Values{"id": {r.ID}, "entity": {"song"}})
	if err != nil {
		return discovery.Track{}, err
	}
	for _, e := range entries {
		if e.Kind == "track" && ref(e.TrackID) == r {
			return track(e), nil
		}
	}
	return discovery.Track{}, discovery.ErrNotFound
}
func (c *Client) Release(ctx context.Context, r discovery.Ref) (discovery.Release, error) {
	entries, err := c.get(ctx, "/lookup", url.Values{"id": {r.ID}, "entity": {"song"}, "limit": {"200"}})
	if err != nil {
		return discovery.Release{}, err
	}
	out := discovery.Release{Tracks: []discovery.Track{}}
	for _, e := range entries {
		if e.Kind == "collection" && ref(e.AlbumID) == r {
			out = release(e)
			break
		}
	}
	if out.Ref.Empty() {
		return out, discovery.ErrNotFound
	}
	for _, e := range entries {
		if e.Kind == "track" && ref(e.AlbumID) == r {
			t := track(e)
			t.AlbumArtist = out.Artist
			out.Tracks = append(out.Tracks, t)
		}
	}
	if len(out.Tracks) == 0 {
		return out, fmt.Errorf("%w: empty release", discovery.ErrNotFound)
	}
	sort.SliceStable(out.Tracks, func(i, j int) bool {
		a, b := out.Tracks[i], out.Tracks[j]
		if a.DiscNumber != b.DiscNumber {
			return a.DiscNumber < b.DiscNumber
		}
		return a.TrackNumber < b.TrackNumber
	})
	return out, nil
}
func (c *Client) Artist(ctx context.Context, r discovery.Ref) (discovery.Artist, error) {
	entries, err := c.get(ctx, "/lookup", url.Values{"id": {r.ID}, "entity": {"album"}, "limit": {"200"}})
	if err != nil {
		return discovery.Artist{}, err
	}
	out := discovery.Artist{Releases: []discovery.Release{}}
	for _, e := range entries {
		if e.Kind == "artist" && ref(e.ArtistID) == r {
			out.Ref = r
			out.Name = e.Artist
		}
		if e.Kind == "collection" {
			out.Releases = append(out.Releases, release(e))
		}
	}
	if out.Ref.Empty() {
		return out, discovery.ErrNotFound
	}
	return out, nil
}

var _ discovery.Metadata = (*Client)(nil)
