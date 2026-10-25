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
	"sync"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"golang.org/x/time/rate"
)

type Client struct {
	http    *http.Client
	base    string
	limiter *rate.Limiter
	mu      sync.Mutex
	artists map[int64]cachedAlbumArtist
}

type cachedAlbumArtist struct {
	name    string
	expires time.Time
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
	Count    int    `json:"trackCount"`
}

func ref(id int64) discovery.Ref {
	return discovery.Ref{Provider: "itunes", ID: strconv.FormatInt(id, 10)}
}
func track(e entry) discovery.Track {
	return discovery.Track{Ref: ref(e.TrackID), Title: e.Title, Artist: e.Artist, Album: e.Album, ReleaseDate: e.Date, TrackNumber: e.Number, DiscNumber: e.Disc, Duration: float64(e.Millis) / 1000, ArtworkURL: strings.Replace(e.Artwork, "100x100bb", "600x600bb", 1), Genre: e.Genre}
}
func release(e entry) discovery.Release {
	r := discovery.Release{Ref: ref(e.AlbumID), Title: e.Album, Artist: e.Artist, ArtworkURL: strings.Replace(e.Artwork, "100x100bb", "600x600bb", 1), Tracks: []discovery.Track{}}
	if e.ArtistID > 0 {
		r.ArtistRef = ref(e.ArtistID)
	}
	return r
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
	entities := []string{"song", "album", "musicArtist"}
	type result struct {
		entries []entry
		err     error
	}
	results := make([]result, len(entities))
	var requests sync.WaitGroup
	for i, entity := range entities {
		requests.Go(func() {
			results[i].entries, results[i].err = c.get(ctx, "/search", url.Values{"term": {query}, "media": {"music"}, "entity": {entity}, "limit": {strconv.Itoa(limit)}})
		})
	}
	requests.Wait()
	artwork := make(map[int64]string)
	for _, result := range results {
		if result.err != nil {
			return out, result.err
		}
		for _, e := range result.entries {
			if e.ArtistID > 0 && e.Artwork != "" {
				artwork[e.ArtistID] = strings.Replace(e.Artwork, "100x100bb", "600x600bb", 1)
			}
			switch e.Kind {
			case "track":
				if e.TrackID > 0 {
					out.Tracks = append(out.Tracks, track(e))
				}
			case "collection":
				out.Releases = append(out.Releases, release(e))
			case "artist":
				out.Artists = append(out.Artists, discovery.Artist{Ref: ref(e.ArtistID), Name: e.Artist, ArtworkURL: artwork[e.ArtistID], Releases: []discovery.Release{}})
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
			artist, err := c.albumArtist(ctx, e.AlbumID)
			if err != nil {
				return discovery.Track{}, err
			}
			t := track(e)
			t.AlbumArtist = artist
			return t, nil
		}
	}
	return discovery.Track{}, discovery.ErrNotFound
}

// Resolve the exact collection rather than treating a guest performer as the
// album artist. Reuse successful lookups across individually selected songs.
func (c *Client) albumArtist(ctx context.Context, id int64) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("%w: track has no collection", discovery.ErrUnavailable)
	}
	c.mu.Lock()
	cached, ok := c.artists[id]
	c.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.name, nil
	}
	entries, err := c.get(ctx, "/lookup", url.Values{"id": {strconv.FormatInt(id, 10)}, "entity": {"album"}})
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Kind != "collection" || e.AlbumID != id || strings.TrimSpace(e.Artist) == "" {
			continue
		}
		c.mu.Lock()
		// Bound this per-client cache; collection credits are refreshed hourly.
		if c.artists == nil || len(c.artists) >= 256 {
			c.artists = make(map[int64]cachedAlbumArtist)
		}
		c.artists[id] = cachedAlbumArtist{name: e.Artist, expires: time.Now().Add(time.Hour)}
		c.mu.Unlock()
		return e.Artist, nil
	}
	return "", fmt.Errorf("%w: collection artist is missing", discovery.ErrUnavailable)
}

func (c *Client) Release(ctx context.Context, r discovery.Ref) (discovery.Release, error) {
	releases, err := c.Releases(ctx, []discovery.Ref{r})
	if err != nil {
		return discovery.Release{}, err
	}
	if len(releases) == 0 {
		return discovery.Release{}, discovery.ErrNotFound
	}
	return releases[0], nil
}

// One provider request covers the visible albums; fetching each card separately
// would quickly exhaust the public API's request allowance.
func (c *Client) Releases(ctx context.Context, refs []discovery.Ref) ([]discovery.Release, error) {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	entries, err := c.get(ctx, "/lookup", url.Values{"id": {strings.Join(ids, ",")}, "entity": {"song"}, "limit": {"200"}})
	if err != nil {
		return nil, err
	}
	albums := make(map[discovery.Ref]*discovery.Release)
	for _, e := range entries {
		if e.Kind == "collection" && e.Count <= 200 {
			r := release(e)
			albums[r.Ref] = &r
		}
	}
	invalid := make(map[discovery.Ref]bool)
	for _, e := range entries {
		r := albums[ref(e.AlbumID)]
		if e.Kind != "track" || r == nil {
			continue
		}
		if e.TrackID <= 0 || e.Title == "" || e.Artist == "" || e.Millis <= 0 {
			invalid[r.Ref] = true
			continue
		}
		t := track(e)
		t.AlbumArtist = r.Artist
		r.Tracks = append(r.Tracks, t)
	}
	out := make([]discovery.Release, 0, len(refs))
	seen := make(map[discovery.Ref]bool)
	for _, ref := range refs {
		r := albums[ref]
		if r == nil || len(r.Tracks) == 0 || invalid[ref] || seen[ref] {
			continue
		}
		seen[ref] = true
		sort.SliceStable(r.Tracks, func(i, j int) bool {
			a, b := r.Tracks[i], r.Tracks[j]
			if a.DiscNumber != b.DiscNumber {
				return a.DiscNumber < b.DiscNumber
			}
			return a.TrackNumber < b.TrackNumber
		})
		out = append(out, *r)
	}
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
			if e.Artwork != "" {
				out.ArtworkURL = strings.Replace(e.Artwork, "100x100bb", "600x600bb", 1)
			}
		}
		if e.Kind == "collection" {
			out.Releases = append(out.Releases, release(e))
			if out.ArtworkURL == "" {
				out.ArtworkURL = release(e).ArtworkURL
			}
		}
	}
	if out.Ref.Empty() {
		return out, discovery.ErrNotFound
	}
	return out, nil
}

var _ discovery.Metadata = (*Client)(nil)
