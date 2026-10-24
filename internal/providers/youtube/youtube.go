// Package youtube searches and downloads audio through yt-dlp.
package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/prayujt/wavelength/internal/discovery"
)

type Client struct{ binary string }

func New() *Client { return &Client{binary: "yt-dlp"} }

// Command output is diagnostic data, not an unbounded in-memory log.
type boundedOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (2 << 20) - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.binary, args...)
	// yt-dlp starts FFmpeg and JavaScript helpers; cancel the whole process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr boundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp: %w: %.500s", err, stderr.String())
	}
	if stdout.truncated {
		return nil, fmt.Errorf("yt-dlp response exceeds limit")
	}
	return stdout.Bytes(), nil
}
func (c *Client) Candidates(ctx context.Context, t discovery.Track) ([]discovery.Candidate, error) {
	raw, err := c.run(ctx, "--ignore-config", "--no-warnings", "--no-cache-dir", "--js-runtimes", "deno", "--socket-timeout", "15", "--retries", "2", "--flat-playlist", "-J", "--", "ytsearch15:"+t.Title+" "+t.Artist)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", discovery.ErrUnavailable, err)
	}
	var response struct {
		Entries []struct {
			ID       string  `json:"id"`
			Title    string  `json:"title"`
			Uploader string  `json:"uploader"`
			Channel  string  `json:"channel"`
			Duration float64 `json:"duration"`
			Live     bool    `json:"is_live"`
		} `json:"entries"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return nil, discovery.ErrUnavailable
	}
	out := []discovery.Candidate{}
	for _, e := range response.Entries {
		r := discovery.Ref{Provider: "youtube", ID: e.ID}
		if !r.ValidSource() || e.Live {
			continue
		}
		uploader := e.Uploader
		if uploader == "" {
			uploader = e.Channel
		}
		score, suitable, reason := match(t, e.Title, uploader, e.Duration)
		out = append(out, discovery.Candidate{Ref: r, Title: e.Title, Uploader: uploader, Duration: e.Duration, Score: score, Suitable: suitable, Reason: reason})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}
func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}
func overlap(want, have string) float64 {
	required := tokens(want)
	if len(required) == 0 {
		return 0
	}
	available := map[string]bool{}
	for _, word := range tokens(have) {
		available[word] = true
	}
	hits := 0
	for _, word := range required {
		if available[word] {
			hits++
		}
	}
	return float64(hits) / float64(len(required))
}
func match(t discovery.Track, title, uploader string, duration float64) (float64, bool, string) {
	titleMatch := overlap(t.Title, title)
	artistMatch := overlap(t.Artist, title+" "+uploader)
	delta := math.Abs(duration - t.Duration)
	durationMatch := math.Max(0, 1-delta/math.Max(t.Duration, 1))
	score := .45*titleMatch + .3*artistMatch + .25*durationMatch
	suitable := duration > 0 && t.Duration > 0 && delta <= math.Max(10, t.Duration*.08) && titleMatch >= .8 && artistMatch >= .8
	reason := "title, artist and duration match"
	if !suitable {
		reason = "title, artist or duration does not match closely"
	}
	// Alternative versions are valid explicit choices, but should not be automatic substitutions.
	for _, version := range []string{"live", "cover", "remix", "acoustic", "karaoke", "instrumental", "slowed", "sped"} {
		if overlap(version, title) > 0 && overlap(version, t.Title) == 0 {
			suitable = false
			score -= .3
			reason = "different version: " + version
			break
		}
	}
	return math.Max(0, score), suitable, reason
}
func (c *Client) Download(ctx context.Context, ref discovery.Ref, path string) error {
	if !ref.ValidSource() {
		return discovery.ErrInvalid
	}
	template := strings.TrimSuffix(path, ".mp3") + ".%(ext)s"
	_, err := c.run(ctx, "--ignore-config", "--no-warnings", "--no-cache-dir", "--js-runtimes", "deno", "--no-playlist", "--no-progress", "--socket-timeout", "20", "--retries", "3", "--fragment-retries", "3", "--max-filesize", "256M", "--match-filter", "!is_live & duration <= 7200", "-f", "bestaudio/best", "-x", "--audio-format", "mp3", "--audio-quality", "0", "-o", template, "--", "https://www.youtube.com/watch?v="+ref.ID)
	return err
}

var _ discovery.Sources = (*Client)(nil)
