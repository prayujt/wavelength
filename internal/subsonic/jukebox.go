package subsonic

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	rand "math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/prayujt/wavelength/internal/state"
)

// JukeboxBackend blocks until playback ends or ctx is cancelled. Implementations
// must honor cancellation. Tests supply an explicit silent backend, never hardware audio.
type JukeboxBackend interface {
	Play(context.Context, *os.File, float64, float64) error
}
type FFplayBackend struct{ Executable string }

func (b FFplayBackend) Play(ctx context.Context, file *os.File, offset, gain float64) error {
	command := exec.CommandContext(ctx, b.Executable, "-nodisp", "-autoexit", "-loglevel", "error", "-ss", strconv.FormatFloat(offset, 'f', 3, 64), "-volume", strconv.Itoa(int(gain*100)), "-i", "/dev/fd/3")
	command.ExtraFiles = []*os.File{file}
	return command.Run()
}
func WithJukeboxBackend(backend JukeboxBackend) Option {
	return func(a *api) error { a.jukeboxBackend = backend; return nil }
}

type jukeboxState struct {
	mu             sync.Mutex
	loaded         bool
	IDs            []string
	Index          int
	Gain, Position float64
	playing        bool
	started        time.Time
	generation     uint64
	cancel         context.CancelFunc
}
type jukeboxRecord struct {
	IDs            []string
	Index          int
	Gain, Position float64
}

func (a *api) jukeboxPosition() float64 {
	value := a.jukebox.Position
	if a.jukebox.playing {
		value += time.Since(a.jukebox.started).Seconds()
	}
	return value
}
func (a *api) stopJukebox() {
	a.jukebox.Position = a.jukeboxPosition()
	a.jukebox.playing = false
	a.jukebox.generation++
	if a.jukebox.cancel != nil {
		a.jukebox.cancel()
		a.jukebox.cancel = nil
	}
}
func (a *api) persistJukebox() error {
	return a.save(a.ctx, "jukebox", "queue", jukeboxRecord{IDs: a.jukebox.IDs, Index: a.jukebox.Index, Gain: a.jukebox.Gain, Position: a.jukeboxPosition()})
}
func (a *api) startJukebox() error {
	j := &a.jukebox
	if a.jukeboxBackend == nil {
		return errors.New("No jukebox playback backend is configured")
	}
	if j.Index < 0 || j.Index >= len(j.IDs) {
		return errors.New("Jukebox queue is empty")
	}
	file, _, err := a.lib.OpenTrack(a.ctx, j.IDs[j.Index])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	j.cancel = cancel
	j.playing = true
	j.started = time.Now()
	j.generation++
	generation := j.generation
	position, gain := j.Position, j.Gain
	go func() {
		err := a.jukeboxBackend.Play(ctx, file, position, gain)
		file.Close()
		j.mu.Lock()
		defer j.mu.Unlock()
		if j.generation != generation {
			return
		}
		j.playing = false
		j.cancel = nil
		if err == nil && a.ctx.Err() == nil && j.Index+1 < len(j.IDs) {
			j.Index++
			j.Position = 0
			_ = a.startJukebox()
		} else {
			j.Position = 0
		}
		_ = a.persistJukebox()
	}()
	return nil
}
func (a *api) jukeboxControl(r *http.Request, result *response) bool {
	action := required(r, result, "action")
	if result.Error != nil {
		return true
	}
	j := &a.jukebox
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.loaded {
		j.Gain = 1
		j.IDs = []string{}
		raw, err := a.store.Get(r.Context(), "jukebox", "queue")
		if err == nil {
			var record jukeboxRecord
			if err = json.Unmarshal(raw, &record); err != nil {
				legacyError(result, err)
				return true
			}
			j.IDs = record.IDs
			j.Index = record.Index
			j.Gain = record.Gain
			j.Position = record.Position
		} else if !errors.Is(err, state.ErrNotFound) {
			legacyError(result, err)
			return true
		}
		j.loaded = true
	}
	if action == "add" || action == "set" {
		for _, id := range r.Form["id"] {
			if _, err := a.lib.Get(r.Context(), id); err != nil {
				legacyError(result, err)
				return true
			}
		}
	}
	switch action {
	case "get", "status":
	case "set":
		current := ""
		if j.Index >= 0 && j.Index < len(j.IDs) {
			current = j.IDs[j.Index]
		}
		j.IDs = append([]string{}, r.Form["id"]...)
		j.Index = 0
		if j.playing {
			j.Index = -1
			for i, id := range j.IDs {
				if id == current {
					j.Index = i
					break
				}
			}
		} else {
			j.Position = 0
		}
	case "add":
		j.IDs = append(j.IDs, r.Form["id"]...)
	case "clear":
		a.stopJukebox()
		j.IDs = []string{}
		j.Index = 0
		j.Position = 0
	case "start":
		if !j.playing {
			if err := a.startJukebox(); err != nil {
				failure(result, 0, err.Error())
				return true
			}
		}
	case "stop":
		a.stopJukebox()
	case "skip", "remove":
		if !r.Form.Has("index") {
			failure(result, 10, "Parameter index is required")
			return true
		}
		index, err := number(r, "index", 0, int(^uint(0)>>1))
		if err != nil || index >= len(j.IDs) {
			failure(result, 10, "Invalid jukebox index")
			return true
		}
		if action == "skip" {
			offset, err := strconv.ParseFloat(r.Form.Get("offset"), 64)
			if r.Form.Get("offset") == "" {
				offset = 0
				err = nil
			}
			if err != nil || math.IsNaN(offset) || math.IsInf(offset, 0) || offset < 0 {
				failure(result, 10, "Invalid jukebox offset")
				return true
			}
			playing := j.playing
			a.stopJukebox()
			j.Index = index
			j.Position = offset
			if playing {
				if err := a.startJukebox(); err != nil {
					failure(result, 0, err.Error())
					return true
				}
			}
		} else {
			playing := j.playing
			selected := index == j.Index
			if selected {
				a.stopJukebox()
				j.Position = 0
			}
			j.IDs = append(j.IDs[:index], j.IDs[index+1:]...)
			if index < j.Index {
				j.Index--
			}
			if j.Index >= len(j.IDs) {
				j.Index = 0
			}
			if playing && selected && len(j.IDs) > 0 {
				if err := a.startJukebox(); err != nil {
					failure(result, 0, err.Error())
					return true
				}
			}
		}
	case "shuffle":
		if len(j.IDs) > 1 {
			selected := ""
			if j.Index >= 0 && j.Index < len(j.IDs) {
				selected = j.IDs[j.Index]
			}
			shuffleStrings(j.IDs)
			for i, id := range j.IDs {
				if id == selected {
					j.Index = i
					break
				}
			}
		}
	case "setGain":
		gain, err := strconv.ParseFloat(r.Form.Get("gain"), 64)
		if err != nil || math.IsNaN(gain) || math.IsInf(gain, 0) || gain < 0 || gain > 1 {
			failure(result, 10, "Gain must be between zero and one")
			return true
		}
		playing := j.playing
		a.stopJukebox()
		j.Gain = gain
		if playing {
			if err := a.startJukebox(); err != nil {
				failure(result, 0, err.Error())
				return true
			}
		}
	default:
		failure(result, 10, "Unknown jukebox action")
		return true
	}
	if err := a.persistJukebox(); err != nil {
		legacyError(result, err)
		return true
	}
	node := map[string]any{"currentIndex": j.Index, "playing": j.playing, "gain": j.Gain, "position": int(a.jukeboxPosition())}
	name := "jukeboxStatus"
	if action == "get" {
		entries, err := a.songEntries(r.Context(), j.IDs)
		if err != nil {
			legacyError(result, err)
			return true
		}
		node["entry"] = entries
		name = "jukeboxPlaylist"
	}
	payload(result, name, node)
	return true
}

func shuffleStrings(values []string) {
	rand.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
}
