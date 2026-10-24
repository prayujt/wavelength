package subsonic

import (
	"net/http"
	"strconv"
	"strings"
)

func (a *api) video(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getVideos", "getVideoInfo", "getCaptions":
	default:
		return false
	}
	if endpoint == "getVideos" {
		videos, err := a.lib.Videos(r.Context())
		if err != nil {
			legacyError(result, err)
			return true
		}
		out := []map[string]any{}
		for _, t := range videos {
			node := asMap(asSong(t))
			node["isVideo"] = true
			node["type"] = "video"
			if streams, err := a.lib.MediaStreams(r.Context(), t.ID); err == nil {
				for _, s := range streams {
					if s.Type == "video" {
						node["originalWidth"] = s.Width
						node["originalHeight"] = s.Height
						break
					}
				}
			}
			out = append(out, node)
		}
		payload(result, "videos", map[string]any{"video": out})
		return true
	}
	id := required(r, result, "id")
	if result.Error != nil {
		return true
	}
	track, err := a.lib.Get(r.Context(), id)
	if err != nil {
		legacyError(result, err)
		return true
	}
	if !strings.HasPrefix(track.ContentType, "video/") {
		failure(result, 70, "Video not found")
		return true
	}
	streams, err := a.lib.MediaStreams(r.Context(), id)
	if err != nil {
		failure(result, 0, err.Error())
		return true
	}
	if endpoint == "getVideoInfo" {
		captions := []map[string]any{}
		audio := []map[string]any{}
		for _, s := range streams {
			switch s.Type {
			case "subtitle":
				captions = append(captions, map[string]any{"id": strconv.Itoa(s.Index), "name": s.Tags.Language})
			case "audio":
				audio = append(audio, map[string]any{"id": strconv.Itoa(s.Index), "name": s.Tags.Title, "languageCode": s.Tags.Language})
			}
		}
		if _, format, err := a.lib.CaptionSidecar(r.Context(), id); err == nil {
			captions = append(captions, map[string]any{"id": "sidecar", "name": format})
		}
		conversions := []map[string]any{}
		if file, err := a.transcoder.OpenConverted(id); err == nil {
			file.Close()
			conversions = append(conversions, map[string]any{"id": id})
		}
		payload(result, "videoInfo", map[string]any{"id": id, "captions": captions, "audioTrack": audio, "conversion": conversions})
		return true
	}
	format := r.Form.Get("format")
	if format == "" {
		format = "srt"
	}
	if format != "srt" && format != "vtt" {
		failure(result, 10, "Caption format must be srt or vtt")
		return true
	}
	body, err := a.transcoder.Captions(r.Context(), id, format)
	if err != nil {
		legacyError(result, err)
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if format == "vtt" {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	}
	if r.Method != "HEAD" {
		_, _ = w.Write(body)
	}
	result.Status = "streamed"
	return true
}
