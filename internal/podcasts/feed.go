package podcasts

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/state"
)

type Episode struct {
	ID, URL, Title, Description, Published, ContentType, Status, Error string
	Size                                                               int64
	Duration                                                           int
}
type Channel struct {
	ID, URL, Title, Description, Image, Status, Error string
	Episodes                                          []Episode
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

func validSource(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("source must be an HTTP(S) URL without embedded credentials")
	}
	return u, nil
}
func (s *Service) fetch(ctx context.Context, address string, limit int64) ([]byte, string, error) {
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
func (s *Service) refreshChannel(ctx context.Context, channel Channel) error {
	body, _, err := s.fetch(ctx, channel.URL, 10<<20)
	if err != nil {
		return s.feedError(ctx, channel.ID, "Could not fetch podcast feed")
	}
	var feed rssFeed
	if err = xml.Unmarshal(body, &feed); err != nil {
		return s.feedError(ctx, channel.ID, "Invalid podcast XML")
	}
	if feed.Channel.Title == "" {
		var atom atomFeed
		if err = xml.Unmarshal(body, &atom); err != nil || atom.Title == "" {
			return s.feedError(ctx, channel.ID, "Source is not an RSS or Atom podcast")
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
	episodes := []Episode{}
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
		episodes = append(episodes, Episode{
			ID:          id(channel.ID + "\x00" + identity),
			URL:         address,
			Title:       item.Title,
			Description: item.Description,
			Published:   podcastDate(item.Published),
			ContentType: item.Enclosure.Type,
			Size:        item.Enclosure.Length,
			Duration:    durationSeconds(item.Duration),
			Status:      "new",
		})
	}
	return s.store.Update(ctx, "podcasts", channel.ID, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &channel); err != nil {
				return nil, err
			}
		}
		previous := map[string]Episode{}
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
func (s *Service) Channels(ctx context.Context) ([]Channel, error) {
	records, err := s.store.List(ctx, "podcasts")
	if err != nil {
		return nil, err
	}
	out := []Channel{}
	for _, raw := range records {
		var channel Channel
		if err = json.Unmarshal(raw, &channel); err != nil {
			return nil, err
		}
		out = append(out, channel)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}
