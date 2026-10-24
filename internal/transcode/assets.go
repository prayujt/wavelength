package transcode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

func (s *Service) OpenConverted(id string) (*os.File, error) {
	if s.directory == "" || id == "" || strings.ContainsAny(id, "/\\.") {
		return nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(id + ".mp4")
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, os.ErrNotExist
	}
	return file, nil
}

func (s *Service) Captions(ctx context.Context, id, format string) ([]byte, error) {
	if format != "srt" && format != "vtt" {
		return nil, ErrInvalid
	}
	container := format
	if format == "vtt" {
		container = "webvtt"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if body, source, err := s.lib.CaptionSidecar(ctx, id); err == nil {
		if source == format {
			return body, nil
		}
		if source == "vtt" {
			source = "webvtt"
		}
		command := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", source, "-i", "pipe:0", "-f", container, "pipe:1")
		command.Stdin = bytes.NewReader(body)
		converted, err := command.Output()
		if err != nil {
			return nil, errors.New("could not convert captions")
		}
		return converted, nil
	}
	streams, err := s.lib.MediaStreams(ctx, id)
	if err != nil {
		return nil, err
	}
	subtitle := -1
	for _, stream := range streams {
		if stream.Type == "subtitle" {
			subtitle = stream.Index
			break
		}
	}
	if subtitle < 0 {
		return nil, library.ErrNotFound
	}
	file, _, err := s.lib.OpenTrack(ctx, id)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	command := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-i", "/dev/fd/3", "-map", fmt.Sprintf("0:%d", subtitle), "-f", container, "pipe:1")
	command.ExtraFiles = []*os.File{file}
	body, err := command.Output()
	if err != nil {
		return nil, errors.New("could not extract captions")
	}
	return body, nil
}
