package subsonic

import (
	"net/http"
	"strconv"
)

// Removed features retain their wire shapes without reading or changing application state.
func (a *api) compatibility(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getPodcasts":
		payload(result, "podcasts", map[string]any{"channel": []any{}})
	case "getNewestPodcasts":
		payload(result, "newestPodcasts", map[string]any{"episode": []any{}})
	case "getVideos":
		payload(result, "videos", map[string]any{"video": []any{}})
	case "getVideoInfo":
		payload(result, "videoInfo", map[string]any{
			"id": r.Form.Get("id"), "captions": []any{},
			"audioTrack": []any{}, "conversion": []any{},
		})
	case "getInternetRadioStations":
		payload(result, "internetRadioStations", map[string]any{"internetRadioStation": []any{}})
	case "getChatMessages":
		payload(result, "chatMessages", map[string]any{"chatMessage": []any{}})
	case "getShares", "createShare":
		payload(result, "shares", map[string]any{"share": []any{}})
	case "getStarred", "getStarred2":
		name := "starred"
		if endpoint == "getStarred2" {
			name = "starred2"
		}
		payload(result, name, map[string]any{"artist": []any{}, "album": []any{}, "song": []any{}})
	case "star", "unstar":
		if len(r.Form["id"])+len(r.Form["albumId"])+len(r.Form["artistId"]) == 0 {
			failure(result, 10, "At least one media ID is required")
		}
	case "setRating":
		if _, err := number(r, "rating", 0, 5); err != nil {
			failure(result, 10, "Invalid rating")
		}
	case "getUsers":
		payload(result, "users", map[string]any{"user": []any{a.compatibilityUser()}})
	case "getUser":
		if r.Form.Get("username") != a.username {
			failure(result, 70, "User not found")
		} else {
			payload(result, "user", a.compatibilityUser())
		}
	case "getAvatar":
		emptyResource(w, r, result, "image/png", "")
	case "getCaptions":
		switch r.Form.Get("format") {
		case "", "srt":
			emptyResource(w, r, result, "text/plain; charset=utf-8", "")
		case "vtt":
			emptyResource(w, r, result, "text/vtt; charset=utf-8", "WEBVTT\n\n")
		default:
			failure(result, 10, "Caption format must be srt or vtt")
		}
	case "createPodcastChannel", "deletePodcastChannel", "refreshPodcasts", "downloadPodcastEpisode", "deletePodcastEpisode",
		"createInternetRadioStation", "updateInternetRadioStation", "deleteInternetRadioStation", "addChatMessage",
		"updateShare", "deleteShare", "createUser", "updateUser", "deleteUser", "changePassword":
		// Acknowledge legacy commands without creating resources or modifying credentials.
	default:
		return false
	}
	return true
}

func (a *api) compatibilityUser() map[string]any {
	return map[string]any{
		"username": a.username, "email": "", "folder": []int{1}, "maxBitRate": 0,
		"adminRole": true, "streamRole": true, "downloadRole": true, "playlistRole": true, "coverArtRole": true,
		"settingsRole": false, "jukeboxRole": false, "uploadRole": false, "commentRole": false,
		"podcastRole": false, "shareRole": false, "videoConversionRole": false, "scrobblingEnabled": false,
	}
}

func emptyResource(w http.ResponseWriter, r *http.Request, result *response, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != "HEAD" {
		_, _ = w.Write([]byte(body))
	}
	result.Status = "streamed"
}
