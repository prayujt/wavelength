package library

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// probe fills gaps left by the tag reader, particularly RIFF and AIFF containers.
func probe(ctx context.Context, file *os.File, track *Track) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries",
		"format=duration,bit_rate:format_tags:stream=codec_type:stream_tags:stream_disposition=attached_pic", "-of", "json", "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{file}
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return
	}
	var data struct {
		Format struct {
			Duration string            `json:"duration"`
			BitRate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Type        string            `json:"codec_type"`
			Tags        map[string]string `json:"tags"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output.Bytes(), &data); err != nil {
		return
	}
	track.Duration, _ = strconv.ParseFloat(data.Format.Duration, 64)
	rate, _ := strconv.Atoi(data.Format.BitRate)
	track.BitRate = rate / 1000
	tags := make(map[string]string, len(data.Format.Tags))
	for key, value := range data.Format.Tags {
		key = strings.ReplaceAll(strings.ToLower(key), "_", "")
		tags[key] = strings.TrimSpace(value)
	}
	for _, stream := range data.Streams {
		for key, value := range stream.Tags {
			key = strings.ReplaceAll(strings.ToLower(key), "_", "")
			if tags[key] == "" {
				tags[key] = strings.TrimSpace(value)
			}
		}
	}
	fillTags(track, tags)
	if track.ContentType == "video/mp4" {
		var audio, video bool
		for _, stream := range data.Streams {
			audio = audio || stream.Type == "audio"
			video = video || (stream.Type == "video" && stream.Disposition.AttachedPic == 0)
		}
		if audio && !video {
			track.ContentType = "audio/mp4"
		}
	}
}
