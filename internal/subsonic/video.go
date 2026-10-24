package subsonic

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
		if file, err := a.openConverted(id); err == nil {
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
	if body, source, err := a.lib.CaptionSidecar(r.Context(), id); err == nil {
		if source != format {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			input, output := source, format
			if input == "vtt" {
				input = "webvtt"
			}
			if output == "vtt" {
				output = "webvtt"
			}
			command := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", input, "-i", "pipe:0", "-f", output, "pipe:1")
			command.Stdin = bytes.NewReader(body)
			converted, err := command.Output()
			if err != nil {
				failure(result, 0, "Could not convert captions")
				return true
			}
			body = converted
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
	subtitle := -1
	for _, s := range streams {
		if s.Type == "subtitle" {
			subtitle = s.Index
			break
		}
	}
	if subtitle < 0 {
		failure(result, 70, "Captions not found")
		return true
	}
	file, _, err := a.lib.OpenTrack(r.Context(), id)
	if err != nil {
		legacyError(result, err)
		return true
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	container := format
	if format == "vtt" {
		container = "webvtt"
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-i", "/dev/fd/3", "-map", fmt.Sprintf("0:%d", subtitle), "-f", container, "pipe:1")
	cmd.ExtraFiles = []*os.File{file}
	var body bytes.Buffer
	cmd.Stdout = &body
	if err = cmd.Run(); err != nil {
		failure(result, 0, "Could not extract captions")
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if format == "vtt" {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	}
	if r.Method != "HEAD" {
		_, _ = w.Write(body.Bytes())
	}
	result.Status = "streamed"
	return true
}
