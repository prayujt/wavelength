package subsonic

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/state"
)

type podcastEpisode struct {
	ID, URL, Title, Description, Published, ContentType, Status, Error string
	Size                                                               int64
	Duration                                                           int
}
type podcastChannel struct {
	ID, URL, Title, Description, Image, Status, Error string
	Episodes                                          []podcastEpisode
}
type rssItem struct {
	GUID        string `xml:"guid"`
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Published   string `xml:"pubDate"`
	Duration    string `xml:"duration"`
	Enclosure   struct {
		URL    string `xml:"url,attr"`
		Type   string `xml:"type,attr"`
		Length int64  `xml:"length,attr"`
	} `xml:"enclosure"`
}
type atomFeed struct {
	Title    string `xml:"title"`
	Subtitle string `xml:"subtitle"`
	Logo     string `xml:"logo"`
	Entries  []struct {
		ID        string `xml:"id"`
		Title     string `xml:"title"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
		Links     []struct {
			Rel    string `xml:"rel,attr"`
			Href   string `xml:"href,attr"`
			Type   string `xml:"type,attr"`
			Length int64  `xml:"length,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

type rssFeed struct {
	Channel struct {
		Title       string `xml:"title"`
		Description string `xml:"description"`
		Image       struct {
			URL string `xml:"url"`
		} `xml:"image"`
		ITunesImage struct {
			Href string `xml:"href,attr"`
		} `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

func WithPodcastDirectory(dir string) Option {
	return func(a *api) error {
		if dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				return err
			}
			a.podcastDir = dir
		}
		return nil
	}
}
func validSource(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("source must be an HTTP(S) URL without embedded credentials")
	}
	return u, nil
}
func (a *api) fetch(ctx context.Context, address string, limit int64) ([]byte, string, error) {
	if _, err := validSource(address); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Wavelength/0.1")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		_, err := validSource(req.URL.String())
		return err
	}}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, "", errors.New("source returned a non-success status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > limit {
		return nil, "", errors.New("source exceeds size limit")
	}
	return body, response.Header.Get("Content-Type"), nil
}
func durationSeconds(value string) int {
	parts := strings.Split(value, ":")
	seconds := 0
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return 0
		}
		seconds = seconds*60 + n
	}
	return seconds
}
func podcastDate(value string) string {
	for _, format := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339} {
		if stamp, err := time.Parse(format, value); err == nil {
			return stamp.UTC().Format(time.RFC3339)
		}
	}
	return ""
}
func (a *api) refreshChannel(ctx context.Context, channel podcastChannel) error {
	body, _, err := a.fetch(ctx, channel.URL, 10<<20)
	if err != nil {
		return a.feedError(ctx, channel.ID, "Could not fetch podcast feed")
	}
	var feed rssFeed
	if err = xml.Unmarshal(body, &feed); err != nil {
		return a.feedError(ctx, channel.ID, "Invalid podcast XML")
	}
	if feed.Channel.Title == "" {
		var atom atomFeed
		if err = xml.Unmarshal(body, &atom); err != nil || atom.Title == "" {
			return a.feedError(ctx, channel.ID, "Source is not an RSS or Atom podcast")
		}
		feed.Channel.Title = atom.Title
		feed.Channel.Description = atom.Subtitle
		feed.Channel.Image.URL = atom.Logo
		for _, entry := range atom.Entries {
			for _, link := range entry.Links {
				if link.Rel == "enclosure" {
					item := rssItem{GUID: entry.ID, Title: entry.Title, Description: entry.Summary, Published: entry.Published}
					if item.Description == "" {
						item.Description = entry.Content
					}
					if item.Published == "" {
						item.Published = entry.Updated
					}
					item.Enclosure.URL = link.Href
					item.Enclosure.Type = link.Type
					item.Enclosure.Length = link.Length
					feed.Channel.Items = append(feed.Channel.Items, item)
					break
				}
			}
		}
	}
	base, _ := url.Parse(channel.URL)
	episodes := []podcastEpisode{}
	for _, item := range feed.Channel.Items {
		if item.Enclosure.URL == "" {
			continue
		}
		u, err := url.Parse(item.Enclosure.URL)
		if err != nil {
			continue
		}
		address := base.ResolveReference(u).String()
		if _, err = validSource(address); err != nil {
			continue
		}
		identity := item.GUID
		if identity == "" {
			identity = address
		}
		episodes = append(episodes, podcastEpisode{ID: id(channel.ID + "\x00" + identity), URL: address, Title: item.Title, Description: item.Description, Published: podcastDate(item.Published), ContentType: item.Enclosure.Type, Size: item.Enclosure.Length, Duration: durationSeconds(item.Duration), Status: "new"})
	}
	return a.store.Update(ctx, "podcasts", channel.ID, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &channel); err != nil {
				return nil, err
			}
		}
		previous := map[string]podcastEpisode{}
		for _, episode := range channel.Episodes {
			previous[episode.ID] = episode
		}
		for i := range episodes {
			if old, ok := previous[episodes[i].ID]; ok {
				episodes[i].Status = old.Status
				episodes[i].Error = old.Error
				if old.Status == "completed" {
					episodes[i].Size = old.Size
				}
			}
			delete(previous, episodes[i].ID)
		}
		for _, old := range previous {
			episodes = append(episodes, old)
		}
		channel.Title = feed.Channel.Title
		channel.Description = feed.Channel.Description
		channel.Image = feed.Channel.Image.URL
		if feed.Channel.ITunesImage.Href != "" {
			channel.Image = feed.Channel.ITunesImage.Href
		}
		channel.Status = "completed"
		channel.Error = ""
		channel.Episodes = episodes
		sort.SliceStable(channel.Episodes, func(i, j int) bool { return channel.Episodes[i].Published > channel.Episodes[j].Published })
		return json.Marshal(channel)
	})
}
func (a *api) podcastChannels(ctx context.Context) ([]podcastChannel, error) {
	records, err := a.store.List(ctx, "podcasts")
	if err != nil {
		return nil, err
	}
	out := []podcastChannel{}
	for _, raw := range records {
		var channel podcastChannel
		if err = json.Unmarshal(raw, &channel); err != nil {
			return nil, err
		}
		out = append(out, channel)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}
func episodeNode(channelID string, e podcastEpisode) map[string]any {
	out := map[string]any{"id": e.ID, "channelId": channelID, "isDir": false, "title": e.Title, "description": e.Description, "status": e.Status, "size": e.Size, "duration": e.Duration, "contentType": e.ContentType, "type": "podcast"}
	if e.Published != "" {
		out["publishDate"] = e.Published
	}
	if e.Status == "completed" {
		out["streamId"] = "podcast_" + e.ID
	}
	return out
}
func channelNode(c podcastChannel, include bool) map[string]any {
	out := map[string]any{"id": c.ID, "url": c.URL, "title": c.Title, "description": c.Description, "status": c.Status}
	if c.Error != "" {
		out["errorMessage"] = c.Error
	}
	if c.Image != "" {
		out["originalImageUrl"] = c.Image
	}
	if include {
		episodes := []map[string]any{}
		for _, e := range c.Episodes {
			if e.Status != "deleted" {
				episodes = append(episodes, episodeNode(c.ID, e))
			}
		}
		out["episode"] = episodes
	}
	return out
}
func (a *api) podcasts(r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getPodcasts", "getNewestPodcasts", "createPodcastChannel", "deletePodcastChannel", "refreshPodcasts", "downloadPodcastEpisode", "deletePodcastEpisode":
	default:
		return false
	}
	ctx := r.Context()
	channels, err := a.podcastChannels(ctx)
	if err != nil {
		legacyError(result, err)
		return true
	}
	if endpoint == "getPodcasts" {
		include := true
		if r.Form.Has("includeEpisodes") {
			include, err = strconv.ParseBool(r.Form.Get("includeEpisodes"))
			if err != nil {
				failure(result, 10, "Invalid includeEpisodes")
				return true
			}
		}
		out := []map[string]any{}
		for _, c := range channels {
			if key := r.Form.Get("id"); key != "" && key != c.ID {
				continue
			}
			out = append(out, channelNode(c, include))
		}
		if r.Form.Get("id") != "" && len(out) == 0 {
			failure(result, 70, "Podcast channel not found")
			return true
		}
		payload(result, "podcasts", map[string]any{"channel": out})
		return true
	}
	if endpoint == "getNewestPodcasts" {
		count, err := number(r, "count", 20, 500)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		episodes := []map[string]any{}
		for _, c := range channels {
			for _, e := range c.Episodes {
				if e.Status == "completed" {
					episodes = append(episodes, episodeNode(c.ID, e))
				}
			}
		}
		sort.SliceStable(episodes, func(i, j int) bool {
			return stringValue(episodes[i]["publishDate"]) > stringValue(episodes[j]["publishDate"])
		})
		payload(result, "newestPodcasts", map[string]any{"episode": pageSlice(episodes, 0, count)})
		return true
	}
	if endpoint == "createPodcastChannel" {
		address := required(r, result, "url")
		if result.Error != nil {
			return true
		}
		if _, err = validSource(address); err != nil {
			failure(result, 10, err.Error())
			return true
		}
		c := podcastChannel{ID: id("podcast\x00" + address), URL: address, Status: "new", Episodes: []podcastEpisode{}}
		if err = a.store.Update(ctx, "podcasts", c.ID, func(raw json.RawMessage) (json.RawMessage, error) {
			if len(raw) > 0 {
				return raw, nil
			}
			return json.Marshal(c)
		}); err != nil {
			legacyError(result, err)
			return true
		}
		if err = a.refreshChannel(ctx, c); err != nil {
			failure(result, 0, "Could not import podcast feed")
		}
		return true
	}
	if endpoint == "refreshPodcasts" {
		for _, c := range channels {
			if err = a.refreshChannel(ctx, c); err != nil {
				failure(result, 0, "Could not refresh podcast feed")
				return true
			}
		}
		return true
	}
	key := required(r, result, "id")
	if result.Error != nil {
		return true
	}
	if endpoint == "deletePodcastChannel" {
		var removed podcastChannel
		err = a.store.Update(ctx, "podcasts", key, func(raw json.RawMessage) (json.RawMessage, error) {
			if len(raw) == 0 {
				return nil, state.ErrNotFound
			}
			if err := json.Unmarshal(raw, &removed); err != nil {
				return nil, err
			}
			return nil, nil
		})
		if errors.Is(err, state.ErrNotFound) {
			failure(result, 70, "Podcast channel not found")
			return true
		}
		if err != nil {
			legacyError(result, err)
			return true
		}
		for _, e := range removed.Episodes {
			if a.podcastDir != "" {
				_ = os.Remove(filepath.Join(a.podcastDir, e.ID+".media"))
			}
		}
		return true
	}
	var channelID string
	var episode podcastEpisode
	for _, c := range channels {
		for _, e := range c.Episodes {
			if e.ID == key {
				channelID = c.ID
				episode = e
			}
		}
	}
	if channelID == "" {
		failure(result, 70, "Podcast episode not found")
		return true
	}
	if endpoint == "deletePodcastEpisode" {
		if err = a.updateEpisode(ctx, channelID, key, func(e *podcastEpisode) { e.Status = "deleted"; e.Error = "" }); err != nil {
			legacyError(result, err)
			return true
		}
		if a.podcastDir != "" {
			_ = os.Remove(filepath.Join(a.podcastDir, key+".media"))
		}
		return true
	}
	if a.podcastDir == "" {
		failure(result, 0, "Podcast download directory is not configured")
		return true
	}
	claimed := false
	if err = a.updateEpisode(ctx, channelID, key, func(e *podcastEpisode) {
		if e.Status != "completed" && e.Status != "downloading" {
			e.Status = "downloading"
			e.Error = ""
			claimed = true
		}
	}); err != nil {
		legacyError(result, err)
		return true
	}
	if claimed {
		go a.downloadEpisode(channelID, episode)
	}
	return true
}
func stringValue(value any) string { s, _ := value.(string); return s }
func (a *api) updateEpisode(ctx context.Context, channelID, episodeID string, update func(*podcastEpisode)) error {
	return a.store.Update(ctx, "podcasts", channelID, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var c podcastChannel
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		found := false
		for i := range c.Episodes {
			if c.Episodes[i].ID == episodeID {
				update(&c.Episodes[i])
				found = true
				break
			}
		}
		if !found {
			return nil, state.ErrNotFound
		}
		return json.Marshal(c)
	})
}
func (a *api) downloadEpisode(channelID string, e podcastEpisode) {
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	temporary := ""
	defer func() {
		if temporary != "" {
			_ = os.Remove(temporary)
		}
	}()
	err := func() error {
		if _, err := validSource(e.URL); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", e.URL, nil)
		if err != nil {
			return err
		}
		client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return errors.New("too many redirects")
			}
			_, err := validSource(r.URL.String())
			return err
		}}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errors.New("download failed")
		}
		file, err := os.CreateTemp(a.podcastDir, "episode-")
		if err != nil {
			return err
		}
		name := file.Name()
		temporary = name
		n, err := io.Copy(file, io.LimitReader(response.Body, 1<<30+1))
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if n > 1<<30 {
			return errors.New("episode exceeds 1 GiB limit")
		}
		e.Size = n
		if contentType := response.Header.Get("Content-Type"); contentType != "" {
			e.ContentType = contentType
		}
		return nil
	}()
	status := "completed"
	message := ""
	if err != nil {
		status = "error"
		message = "Episode download failed"
	}
	_ = a.store.Update(a.ctx, "podcasts", channelID, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var c podcastChannel
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		for i := range c.Episodes {
			current := &c.Episodes[i]
			if current.ID != e.ID {
				continue
			}
			if current.Status != "downloading" {
				return raw, nil
			}
			if status == "completed" {
				if err := os.Rename(temporary, filepath.Join(a.podcastDir, e.ID+".media")); err != nil {
					status = "error"
					message = "Episode storage failed"
				} else {
					current.Size = e.Size
					current.ContentType = e.ContentType
				}
			}
			current.Status = status
			current.Error = message
			break
		}
		return json.Marshal(c)
	})
}
func (a *api) feedError(ctx context.Context, id, message string) error {
	_ = a.store.Update(ctx, "podcasts", id, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var c podcastChannel
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		c.Status = "error"
		c.Error = message
		return json.Marshal(c)
	})
	return errors.New(message)
}

func (a *api) podcastMedia(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	key := r.Form.Get("id")
	if !strings.HasPrefix(key, "podcast_") || (endpoint != "getSong" && endpoint != "stream" && endpoint != "download") {
		return false
	}
	key = strings.TrimPrefix(key, "podcast_")
	channels, err := a.podcastChannels(r.Context())
	if err != nil {
		legacyError(result, err)
		return true
	}
	var episode podcastEpisode
	channelID := ""
	for _, c := range channels {
		for _, e := range c.Episodes {
			if e.ID == key {
				episode = e
				channelID = c.ID
			}
		}
	}
	if channelID == "" || episode.Status != "completed" {
		failure(result, 70, "Downloaded podcast not found")
		return true
	}
	if endpoint == "getSong" {
		node := episodeNode(channelID, episode)
		node["id"] = "podcast_" + episode.ID
		payload(result, "song", node)
		return true
	}
	root, err := os.OpenRoot(a.podcastDir)
	if err != nil {
		failure(result, 70, "Podcast storage unavailable")
		return true
	}
	defer root.Close()
	file, err := root.Open(key + ".media")
	if err != nil {
		failure(result, 70, "Podcast file not found")
		return true
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		legacyError(result, err)
		return true
	}
	contentType := episode.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if endpoint == "download" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": episode.Title}))
	}
	http.ServeContent(w, r, episode.Title, info.ModTime(), file)
	result.Status = "streamed"
	return true
}
