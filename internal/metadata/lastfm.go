// Package metadata supplies optional external music metadata and scrobble providers.
package metadata

import (
	"context"
	"crypto/md5" // Last.fm's documented request-signature algorithm.
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Image struct {
	URL  string `json:"#text"`
	Size string `json:"size"`
}
type Artist struct {
	Name          string  `json:"name"`
	MusicBrainzID string  `json:"mbid"`
	URL           string  `json:"url"`
	Images        []Image `json:"image"`
	Bio           struct {
		Content string `json:"content"`
		Summary string `json:"summary"`
	} `json:"bio"`
	Similar struct {
		Artists []Artist `json:"artist"`
	} `json:"similar"`
}
type Album struct {
	Name          string  `json:"name"`
	Artist        string  `json:"artist"`
	MusicBrainzID string  `json:"mbid"`
	URL           string  `json:"url"`
	Images        []Image `json:"image"`
	Wiki          struct {
		Content string `json:"content"`
		Summary string `json:"summary"`
	} `json:"wiki"`
}
type RankedTrack struct {
	Name   string `json:"name"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
}
type Provider interface {
	Artist(context.Context, string) (Artist, error)
	Album(context.Context, string, string) (Album, error)
	TopTracks(context.Context, string, int) ([]RankedTrack, error)
}
type Scrobbler interface {
	Enabled(string) bool
	Submit(context.Context, string, string, string, string, time.Time, bool) error
}
type LastFM struct {
	APIKey, Secret, BaseURL string
	Sessions                map[string]string
	Client                  *http.Client
}

func (l *LastFM) request(ctx context.Context, method string, values url.Values, post bool, target any) error {
	base := l.BaseURL
	if base == "" {
		base = "https://ws.audioscrobbler.com/2.0/"
	}
	values.Set("method", method)
	values.Set("api_key", l.APIKey)
	if post {
		if l.Secret == "" {
			return errors.New("Last.fm signing secret is not configured")
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			if key != "format" && key != "callback" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		var signature strings.Builder
		for _, key := range keys {
			signature.WriteString(key)
			signature.WriteString(values.Get(key))
		}
		signature.WriteString(l.Secret)
		values.Set("api_sig", fmt.Sprintf("%x", md5.Sum([]byte(signature.String()))))
	}
	values.Set("format", "json")
	httpMethod := http.MethodGet
	address := base
	var body io.Reader
	if post {
		httpMethod = http.MethodPost
		body = strings.NewReader(values.Encode())
	} else {
		u, err := url.Parse(base)
		if err != nil {
			return err
		}
		u.RawQuery = values.Encode()
		address = u.String()
	}
	req, err := http.NewRequestWithContext(ctx, httpMethod, address, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Wavelength/0.1")
	if post {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("music metadata provider request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("music metadata provider returned a non-success status")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	var failure struct {
		Error int `json:"error"`
	}
	if json.Unmarshal(raw, &failure) != nil {
		return errors.New("invalid music metadata response")
	}
	if failure.Error != 0 {
		return fmt.Errorf("music metadata provider error %d", failure.Error)
	}
	return json.Unmarshal(raw, target)
}
func (l *LastFM) Artist(ctx context.Context, name string) (Artist, error) {
	var data struct {
		Artist Artist `json:"artist"`
	}
	err := l.request(ctx, "artist.getInfo", url.Values{"artist": {name}}, false, &data)
	return data.Artist, err
}
func (l *LastFM) Album(ctx context.Context, artist, name string) (Album, error) {
	var data struct {
		Album Album `json:"album"`
	}
	err := l.request(ctx, "album.getInfo", url.Values{"artist": {artist}, "album": {name}}, false, &data)
	return data.Album, err
}
func (l *LastFM) TopTracks(ctx context.Context, artist string, count int) ([]RankedTrack, error) {
	var data struct {
		Top struct {
			Tracks []RankedTrack `json:"track"`
		} `json:"toptracks"`
	}
	err := l.request(ctx, "artist.getTopTracks", url.Values{"artist": {artist}, "limit": {strconv.Itoa(count)}}, false, &data)
	return data.Top.Tracks, err
}
func (l *LastFM) Enabled(user string) bool {
	return l.APIKey != "" && l.Secret != "" && l.Sessions[user] != ""
}
func (l *LastFM) Submit(ctx context.Context, user, artist, track, album string, stamp time.Time, submission bool) error {
	if !l.Enabled(user) {
		return nil
	}
	method := "track.updateNowPlaying"
	values := url.Values{"sk": {l.Sessions[user]}, "artist": {artist}, "track": {track}, "album": {album}}
	if submission {
		method = "track.scrobble"
		values.Set("timestamp", strconv.FormatInt(stamp.Unix(), 10))
	}
	var output map[string]json.RawMessage
	return l.request(ctx, method, values, true, &output)
}
