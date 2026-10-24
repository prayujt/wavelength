package subsonic

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var videoSize = regexp.MustCompile(`^([1-9][0-9]{0,3})x([1-9][0-9]{0,3})$`)

func scaleArgument(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	parts := videoSize.FindStringSubmatch(value)
	if parts == nil {
		return "", errors.New("invalid video size")
	}
	width, _ := strconv.Atoi(parts[1])
	height, _ := strconv.Atoi(parts[2])
	if width > 4096 || height > 4096 || width%2 != 0 || height%2 != 0 {
		return "", errors.New("video size must use even dimensions at most 4096")
	}
	return parts[1] + ":" + parts[2], nil
}
func (a *api) videoTranscode(w http.ResponseWriter, r *http.Request, id, format string, bitrate int) (bool, error) {
	if format == "" {
		format = "flv"
	}
	codec, container, contentType := "libx264", format, "video/x-flv"
	switch format {
	case "flv":
	case "mp4":
		contentType = "video/mp4"
	case "webm":
		codec = "libvpx-vp9"
		contentType = "video/webm"
	default:
		return false, errors.New("unsupported video conversion format")
	}
	offset, err := int64Parameter(r, "timeOffset", 0)
	if err != nil {
		return false, err
	}
	size, err := scaleArgument(r.Form.Get("size"))
	if err != nil {
		return false, err
	}
	if _, err = exec.LookPath("ffmpeg"); err != nil {
		return false, errors.New("video conversion requires ffmpeg")
	}
	file, _, err := a.lib.OpenTrack(r.Context(), id)
	if err != nil {
		return false, err
	}
	defer file.Close()
	args := []string{"-v", "error", "-nostdin"}
	if offset > 0 {
		args = append(args, "-ss", strconv.FormatInt(offset, 10))
	}
	args = append(args, "-i", "/dev/fd/3", "-map", "0:v:0", "-map", "0:a:0?", "-c:v", codec)
	if codec == "libx264" {
		args = append(args, "-preset", "veryfast", "-pix_fmt", "yuv420p")
	}
	if size != "" {
		args = append(args, "-vf", "scale="+size)
	}
	if bitrate > 0 {
		args = append(args, "-b:v", strconv.Itoa(bitrate)+"k")
	}
	audio := "aac"
	if format == "webm" {
		audio = "libopus"
	}
	args = append(args, "-c:a", audio, "-b:a", "128k")
	if format == "mp4" {
		args = append(args, "-movflags", "frag_keyframe+empty_moov")
	}
	args = append(args, "-f", container, "pipe:1")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	command := exec.CommandContext(ctx, "ffmpeg", args...)
	command.ExtraFiles = []*os.File{file}
	return serveEncoded(w, r, command, cancel, contentType)
}
func (a *api) maybeVideoTranscode(w http.ResponseWriter, r *http.Request, result *response, contentType, id, format string, bitrate int) bool {
	if !strings.HasPrefix(contentType, "video/") || format == "raw" {
		return false
	}
	if format == "" && bitrate == 0 && r.Form.Get("timeOffset") == "" && r.Form.Get("size") == "" {
		return false
	}
	started, err := a.videoTranscode(w, r, id, format, bitrate)
	if started {
		result.Status = "streamed"
	} else if err != nil {
		failure(result, 0, err.Error())
	}
	return true
}
