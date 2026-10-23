package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

// probe enriches metadata when ffprobe is available; it never opens a raw request path.
func probe(ctx context.Context, file *os.File, track *Track) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration,bit_rate:format_tags=genre,title,artist,album,album_artist,date,track,disc", "-of", "json", "/dev/fd/3")
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
	}
	if json.Unmarshal(output.Bytes(), &data) != nil {
		return
	}
	track.Duration, _ = strconv.ParseFloat(data.Format.Duration, 64)
	rate, _ := strconv.Atoi(data.Format.BitRate)
	track.BitRate = rate / 1000
	tags := data.Format.Tags
	if track.Genre == "" {
		track.Genre = tags["genre"]
	}
}

// Artwork returns embedded art or a conventional adjacent image through the confined root.
func (l *Library) Artwork(ctx context.Context, id string) ([]byte, string, error) {
	file, track, err := l.OpenTrack(ctx, id)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	metadata, err := tag.ReadFrom(file)
	if err == nil {
		if picture := metadata.Picture(); picture != nil && len(picture.Data) > 0 {
			return picture.Data, picture.MIMEType, nil
		}
	}
	for _, name := range []string{"cover.jpg", "cover.png", "folder.jpg", "folder.png", "front.jpg", "front.png"} {
		image, err := l.root.Open(filepath.Join(filepath.Dir(track.Path), name))
		if err != nil {
			continue
		}
		info, err := image.Stat()
		if err != nil || !info.Mode().IsRegular() {
			image.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(image, 20<<20))
		image.Close()
		if err != nil {
			return nil, "", err
		}
		mime := "image/jpeg"
		if strings.HasSuffix(name, ".png") {
			mime = "image/png"
		}
		return data, mime, nil
	}
	return nil, "", ErrNotFound
}

// Lyrics reads an adjacent UTF-8 lyrics file without permitting traversal outside the library.
func (l *Library) Lyrics(ctx context.Context, id string) (string, error) {
	track, err := l.Get(ctx, id)
	if err != nil {
		return "", err
	}
	for _, ext := range []string{".lrc", ".txt"} {
		file, err := l.root.Open(strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + ext)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			file.Close()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(file, 1<<20))
		file.Close()
		return string(body), err
	}
	return "", ErrNotFound
}
