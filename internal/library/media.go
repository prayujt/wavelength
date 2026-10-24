package library

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhowden/tag"
)

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
	entries, err := l.root.Open(filepath.Dir(track.Path))
	if err != nil {
		return nil, "", err
	}
	defer entries.Close()
	names, err := entries.ReadDir(-1)
	if err != nil {
		return nil, "", err
	}
	sort.Slice(names, func(i, j int) bool { return artworkRank(names[i].Name()) < artworkRank(names[j].Name()) })
	for _, entry := range names {
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" {
			continue
		}
		image, err := l.root.Open(filepath.Join(filepath.Dir(track.Path), entry.Name()))
		if err != nil {
			continue
		}
		info, err := image.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 20<<20 {
			image.Close()
			continue
		}
		data, err := io.ReadAll(image)
		image.Close()
		if err != nil {
			return nil, "", err
		}
		kind := http.DetectContentType(data)
		if strings.HasPrefix(kind, "image/") {
			return data, kind, nil
		}
	}
	return nil, "", ErrNotFound
}

// Lyrics reads an adjacent UTF-8 lyrics file without permitting traversal outside the library.
func (l *Library) Lyrics(ctx context.Context, id string) (string, error) {
	file, track, err := l.OpenTrack(ctx, id)
	if err != nil {
		return "", err
	}
	metadata, tagErr := tag.ReadFrom(file)
	file.Close()
	if tagErr == nil && metadata.Lyrics() != "" {
		return metadata.Lyrics(), nil
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

func artworkRank(name string) string {
	lower := strings.ToLower(name)
	for i, preferred := range []string{"cover.jpg", "cover.png", "cover.gif", "folder.jpg", "folder.png", "front.jpg", "front.png"} {
		if lower == preferred {
			return string(rune('0'+i)) + lower
		}
	}
	return "z" + lower
}
