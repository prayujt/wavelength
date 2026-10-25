// Package youtube searches and downloads audio through yt-dlp.
package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/telemetry"
)

type Client struct {
	metrics *telemetry.Metrics
	binary  string
	slots   chan struct{}
}

func (c *Client) SetMetrics(metrics *telemetry.Metrics) { c.metrics = metrics }

func New() *Client { return NewWithConcurrency(2) }
func NewWithConcurrency(limit int) *Client {
	return &Client{binary: "yt-dlp", slots: make(chan struct{}, max(1, limit))}
}

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
	waited := time.Now()
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		c.metrics.Wait("youtube", "command_slot", time.Since(waited))
		return nil, ctx.Err()
	}
	c.metrics.Wait("youtube", "command_slot", time.Since(waited))
	defer func() { <-c.slots }()
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
	operation := "download"
	for _, arg := range args {
		if arg == "--flat-playlist" {
			operation = "search"
			break
		}
	}
	started := time.Now()
	runErr := cmd.Run()
	metricErr := runErr
	if ctx.Err() != nil {
		metricErr = ctx.Err()
	}
	c.metrics.Provider("youtube", operation, 0, metricErr, time.Since(started))
	if err := runErr; err != nil {
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
		Entries []json.RawMessage `json:"entries"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return nil, discovery.ErrUnavailable
	}
	out := []discovery.Candidate{}
	for _, entry := range response.Entries {
		var e struct {
			ID       string  `json:"id"`
			Title    string  `json:"title"`
			Uploader string  `json:"uploader"`
			Channel  string  `json:"channel"`
			Duration float64 `json:"duration"`
			Live     bool    `json:"is_live"`
		}
		if err := json.Unmarshal(entry, &e); err != nil {
			return nil, discovery.ErrUnavailable
		}
		r := discovery.Ref{Provider: "youtube", ID: e.ID}
		if !r.ValidSource() || e.Live {
			continue
		}
		uploader := e.Uploader
		if uploader == "" {
			uploader = e.Channel
		}
		score, suitable, reason := match(t, e.Title, uploader, e.Duration)
		out = append(out, discovery.Candidate{Ref: r, Title: e.Title, Uploader: uploader, Duration: e.Duration, Score: score, Suitable: suitable, Reason: reason, Evidence: entry})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
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
