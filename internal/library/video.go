package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type MediaStream struct {
	Index  int    `json:"index"`
	Type   string `json:"codec_type"`
	Codec  string `json:"codec_name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Tags   struct {
		Language string `json:"language"`
		Title    string `json:"title"`
	} `json:"tags"`
}

func (l *Library) MediaStreams(ctx context.Context, id string) ([]MediaStream, error) {
	file, _, err := l.OpenTrack(ctx, id)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "stream=index,codec_type,codec_name,width,height:stream_tags=language,title", "-of", "json", "/dev/fd/3")
	command.ExtraFiles = []*os.File{file}
	var output bytes.Buffer
	command.Stdout = &output
	if err = command.Run(); err != nil {
		return nil, errors.New("media inspection requires ffprobe and a decodable source")
	}
	var data struct {
		Streams []MediaStream `json:"streams"`
	}
	err = json.Unmarshal(output.Bytes(), &data)
	return data.Streams, err
}
func (l *Library) Videos(ctx context.Context) ([]Track, error) {
	out := []Track{}
	for offset := 0; ; offset += 500 {
		page, err := l.List(ctx, Query{IncludeVideo: true, Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, t := range page.Tracks {
			if strings.HasPrefix(t.ContentType, "video/") {
				out = append(out, t)
			}
		}
		if len(page.Tracks) < 500 {
			break
		}
	}
	return out, nil
}
func (l *Library) CaptionSidecar(ctx context.Context, id string) ([]byte, string, error) {
	track, err := l.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	for _, format := range []string{"vtt", "srt"} {
		file, err := l.root.Open(strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + "." + format)
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 10<<20 {
			file.Close()
			continue
		}
		body := make([]byte, info.Size())
		_, err = file.ReadAt(body, 0)
		file.Close()
		if err != nil {
			return nil, "", err
		}
		return body, format, nil
	}
	return nil, "", ErrNotFound
}
