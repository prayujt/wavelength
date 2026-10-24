package transcode

import (
	"context"
	"fmt"
	"strconv"
)

type AudioOptions struct {
	Format  string
	BitRate int
}

func (s *Service) Audio(ctx context.Context, id string, options AudioOptions) (*Stream, error) {
	codec, container, kind := "", "", ""
	switch options.Format {
	case "mp3":
		codec, container, kind = "libmp3lame", "mp3", "audio/mpeg"
	case "ogg":
		codec, container, kind = "libvorbis", "ogg", "audio/ogg"
	case "opus":
		codec, container, kind = "libopus", "opus", "audio/ogg"
	case "aac":
		codec, container, kind = "aac", "adts", "audio/aac"
	case "m4a":
		codec, container, kind = "aac", "mp4", "audio/mp4"
	case "flac":
		codec, container, kind = "flac", "flac", "audio/flac"
	case "wav":
		codec, container, kind = "pcm_s16le", "wav", "audio/wav"
	default:
		return nil, fmt.Errorf("%w: unsupported audio format", ErrInvalid)
	}
	if options.BitRate < 0 || options.BitRate > 100000 {
		return nil, ErrInvalid
	}
	file, _, err := s.lib.OpenTrack(ctx, id)
	if err != nil {
		return nil, err
	}
	args := []string{"-v", "error", "-nostdin", "-i", "/dev/fd/3", "-map", "0:a:0", "-vn", "-c:a", codec}
	if options.Format != "wav" && options.Format != "flac" {
		args = append(args, "-b:a", strconv.Itoa(options.BitRate)+"k")
	}
	if options.Format == "m4a" {
		args = append(args, "-movflags", "frag_keyframe+empty_moov")
	}
	return start(ctx, file, append(args, "-f", container, "pipe:1"), kind)
}
