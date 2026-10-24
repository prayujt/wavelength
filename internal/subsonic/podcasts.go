package subsonic

import (
	"github.com/prayujt/wavelength/internal/podcasts"
	"mime"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

func WithPodcastDirectory(dir string) Option {
	return func(a *api) error { a.podcastDir = dir; return nil }
}
func episodeNode(channelID string, e podcasts.Episode) map[string]any {
	out := map[string]any{"id": e.ID, "channelId": channelID, "isDir": false, "title": e.Title, "description": e.Description, "status": e.Status, "size": e.Size, "duration": e.Duration, "contentType": e.ContentType, "type": "podcast"}
	if e.Published != "" {
		out["publishDate"] = e.Published
	}
	if e.Status == "completed" {
		out["streamId"] = "podcast_" + e.ID
	}
	return out
}
func channelNode(c podcasts.Channel, include bool) map[string]any {
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
	var err error
	switch endpoint {
	case "getPodcasts", "getNewestPodcasts":
		channels, e := a.podcastsService.Channels(ctx)
		if e != nil {
			legacyError(result, e)
			return true
		}
		if endpoint == "getNewestPodcasts" {
			count, e := number(r, "count", 20, 500)
			if e != nil {
				failure(result, 10, e.Error())
				return true
			}
			episodes := []map[string]any{}
			for _, channel := range channels {
				for _, episode := range channel.Episodes {
					if episode.Status == "completed" {
						episodes = append(episodes, episodeNode(channel.ID, episode))
					}
				}
			}
			sort.SliceStable(episodes, func(i, j int) bool {
				return stringValue(episodes[i]["publishDate"]) > stringValue(episodes[j]["publishDate"])
			})
			payload(result, "newestPodcasts", map[string]any{"episode": pageSlice(episodes, 0, count)})
			return true
		}
		include := true
		if r.Form.Has("includeEpisodes") {
			include, e = strconv.ParseBool(r.Form.Get("includeEpisodes"))
			if e != nil {
				failure(result, 10, "Invalid includeEpisodes")
				return true
			}
		}
		nodes := []map[string]any{}
		for _, channel := range channels {
			if id := r.Form.Get("id"); id == "" || id == channel.ID {
				nodes = append(nodes, channelNode(channel, include))
			}
		}
		if r.Form.Get("id") != "" && len(nodes) == 0 {
			failure(result, 70, "Podcast channel not found")
			return true
		}
		payload(result, "podcasts", map[string]any{"channel": nodes})
		return true
	case "createPodcastChannel":
		err = a.podcastsService.Subscribe(ctx, r.Form.Get("url"))
	case "refreshPodcasts":
		err = a.podcastsService.Refresh(ctx)
	case "deletePodcastChannel":
		err = a.podcastsService.DeleteChannel(ctx, r.Form.Get("id"))
	case "deletePodcastEpisode":
		err = a.podcastsService.DeleteEpisode(ctx, r.Form.Get("id"))
	case "downloadPodcastEpisode":
		err = a.podcastsService.Download(ctx, r.Form.Get("id"))
	}
	if err != nil {
		legacyError(result, err)
	}
	return true
}
func stringValue(value any) string { s, _ := value.(string); return s }
func (a *api) podcastMedia(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	id := r.Form.Get("id")
	if !strings.HasPrefix(id, "podcast_") || (endpoint != "getSong" && endpoint != "stream" && endpoint != "download") {
		return false
	}
	id = strings.TrimPrefix(id, "podcast_")
	channel, episode, err := a.podcastsService.Episode(r.Context(), id)
	if err != nil || episode.Status != "completed" {
		failure(result, 70, "Downloaded podcast not found")
		return true
	}
	if endpoint == "getSong" {
		node := episodeNode(channel, episode)
		node["id"] = "podcast_" + episode.ID
		payload(result, "song", node)
		return true
	}
	file, episode, err := a.podcastsService.Open(r.Context(), id)
	if err != nil {
		if os.IsNotExist(err) {
			failure(result, 70, "Podcast file not found")
		} else {
			legacyError(result, err)
		}
		return true
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		legacyError(result, err)
		return true
	}
	kind := episode.ContentType
	if kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
	if endpoint == "download" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": episode.Title}))
	}
	http.ServeContent(w, r, episode.Title, info.ModTime(), file)
	result.Status = "streamed"
	return true
}
