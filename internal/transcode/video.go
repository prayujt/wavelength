package transcode

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

var videoSize = regexp.MustCompile(`^([1-9][0-9]{0,3})x([1-9][0-9]{0,3})$`)

func ScaleArgument(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	parts := videoSize.FindStringSubmatch(value)
	if parts == nil {
		return "", fmt.Errorf("%w: invalid dimensions", ErrInvalid)
	}
	width, _ := strconv.Atoi(parts[1])
	height, _ := strconv.Atoi(parts[2])
	if width > 4096 || height > 4096 || width%2 != 0 || height%2 != 0 {
		return "", fmt.Errorf("%w: dimensions must be even and at most 4096", ErrInvalid)
	}
	return parts[1] + ":" + parts[2], nil
}

type VideoOptions struct {
	Format, Size  string
	BitRate       int
	OffsetSeconds int64
}

func (s *Service) Video(ctx context.Context, id string, options VideoOptions) (*Stream, error) {
	codec, container, kind := "libx264", options.Format, "video/x-flv"
	switch options.Format {
	case "flv":
	case "mp4":
		kind = "video/mp4"
	case "webm":
		codec, kind = "libvpx-vp9", "video/webm"
	default:
		return nil, fmt.Errorf("%w: unsupported video format", ErrInvalid)
	}
	if options.OffsetSeconds < 0 || options.BitRate < 0 || options.BitRate > 100000 {
		return nil, ErrInvalid
	}
	size, err := ScaleArgument(options.Size)
	if err != nil {
		return nil, err
	}
	file, _, err := s.lib.OpenTrack(ctx, id)
	if err != nil {
		return nil, err
	}
	args := []string{"-v", "error", "-nostdin"}
	if options.OffsetSeconds > 0 {
		args = append(args, "-ss", strconv.FormatInt(options.OffsetSeconds, 10))
	}
	args = append(args, "-i", "/dev/fd/3", "-map", "0:v:0", "-map", "0:a:0?", "-c:v", codec)
	if codec == "libx264" {
		args = append(args, "-preset", "veryfast", "-pix_fmt", "yuv420p")
	}
	if size != "" {
		args = append(args, "-vf", "scale="+size)
	}
	if options.BitRate > 0 {
		args = append(args, "-b:v", strconv.Itoa(options.BitRate)+"k")
	}
	audio := "aac"
	if options.Format == "webm" {
		audio = "libopus"
	}
	args = append(args, "-c:a", audio, "-b:a", "128k")
	if options.Format == "mp4" {
		args = append(args, "-movflags", "frag_keyframe+empty_moov")
	}
	return start(ctx, file, append(args, "-f", container, "pipe:1"), kind)
}
