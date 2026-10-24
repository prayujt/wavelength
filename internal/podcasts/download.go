package podcasts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prayujt/wavelength/internal/state"
)

func (s *Service) updateEpisode(ctx context.Context, channelID, episodeID string, update func(*Episode)) error {
	return s.store.Update(ctx, "podcasts", channelID, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var c Channel
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		found := false
		for i := range c.Episodes {
			if c.Episodes[i].ID == episodeID {
				update(&c.Episodes[i])
				found = true
				break
			}
		}
		if !found {
			return nil, state.ErrNotFound
		}
		return json.Marshal(c)
	})
}
func (s *Service) downloadEpisode(channelID string, e Episode) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	defer cancel()
	temporary := ""
	defer func() {
		if temporary != "" {
			_ = os.Remove(temporary)
		}
	}()
	err := func() error {
		if _, err := validSource(e.URL); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", e.URL, nil)
		if err != nil {
			return err
		}
		client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return errors.New("too many redirects")
			}
			_, err := validSource(r.URL.String())
			return err
		}}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errors.New("download failed")
		}
		file, err := os.CreateTemp(s.directory, "episode-")
		if err != nil {
			return err
		}
		name := file.Name()
		temporary = name
		n, err := io.Copy(file, io.LimitReader(response.Body, 1<<30+1))
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if n > 1<<30 {
			return errors.New("episode exceeds 1 GiB limit")
		}
		e.Size = n
		if contentType := response.Header.Get("Content-Type"); contentType != "" {
			e.ContentType = contentType
		}
		return nil
	}()
	status := "completed"
	message := ""
	if err != nil {
		status = "error"
		message = "Episode download failed"
	}
	// Finishing a cancelled download still needs a short window to persist its status.
	finish, stop := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer stop()
	_ = s.store.Update(finish, "podcasts", channelID, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var c Channel
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		for i := range c.Episodes {
			current := &c.Episodes[i]
			if current.ID != e.ID {
				continue
			}
			if current.Status != "downloading" {
				return raw, nil
			}
			if status == "completed" {
				if err := os.Rename(temporary, filepath.Join(s.directory, e.ID+".media")); err != nil {
					status = "error"
					message = "Episode storage failed"
				} else {
					current.Size = e.Size
					current.ContentType = e.ContentType
				}
			}
			current.Status = status
			current.Error = message
			break
		}
		return json.Marshal(c)
	})
}
func (s *Service) feedError(ctx context.Context, id, message string) error {
	_ = s.store.Update(ctx, "podcasts", id, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var c Channel
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		c.Status = "error"
		c.Error = message
		return json.Marshal(c)
	})
	return errors.New(message)
}
