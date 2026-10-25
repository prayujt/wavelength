package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func (s *Service) stagePath(id string, index int) string {
	return filepath.Join(s.staging, id, fmt.Sprint(index), "audio.mp3")
}
func checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Service) publishFile(ctx context.Context, id string, index int, p Prepared) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if existing, err := s.files.Open(p.Track.Path); err == nil {
		h := sha256.New()
		_, readErr := io.Copy(h, existing)
		existing.Close()
		if readErr == nil && hex.EncodeToString(h.Sum(nil)) == p.SHA256 {
			return nil
		}
		return fmt.Errorf("destination exists with different content")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.files.MkdirAll(filepath.Dir(p.Track.Path), 0755); err != nil {
		return err
	}
	source, err := os.Open(s.stagePath(id, index))
	if err != nil {
		return err
	}
	defer source.Close()
	pending := pendingPath(p)
	_ = s.files.Remove(pending)
	out, err := s.files.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), source)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		copyErr = errors.New("staging checksum changed")
	}
	if copyErr == nil {
		copyErr = ctx.Err()
	}
	if copyErr != nil {
		s.files.Remove(pending)
		return copyErr
	}
	if err = s.files.Rename(pending, p.Track.Path); err != nil {
		return err
	}
	directory, err := s.files.Open(filepath.Dir(p.Track.Path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (s *Service) cleanItem(j Job, n int) error {
	item := j.Items[n]
	var result error
	if item.Prepared != nil && item.State != Completed {
		existing, err := s.deps.Store.Lookup(s.ctx, s.root.ID, item.MetadataRef)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if errors.Is(err, ErrNotFound) || (err == nil && existing.ID != item.Prepared.Track.ID) {
			for _, path := range []string{item.Prepared.Track.Path, pendingPath(*item.Prepared)} {
				if err := s.files.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					result = errors.Join(result, err)
				}
			}
		}
	}
	return errors.Join(result, os.RemoveAll(filepath.Dir(s.stagePath(j.ID, n))))
}
func (s *Service) cleanCancelled(j Job) error {
	if j.State != Cancelled {
		return nil
	}
	for n := range j.Items {
		if j.Items[n].State != Completed {
			if err := s.cleanItem(j, n); err != nil {
				return err
			}
		}
	}
	_, err := s.updateJob(s.ctx, j.ID, func(j *Job) error {
		if j.State == Cancelled {
			j.Cleaned = true
		}
		return nil
	})
	return err
}

func pendingPath(p Prepared) string {
	return filepath.Join(filepath.Dir(p.Track.Path), ".pending-"+p.Track.ID)
}

// Provider text may name a folder, but never supplies filesystem syntax.
func component(value, fallback string) string {
	if value == "" {
		return fallback
	}
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, value)
	safe = strings.Trim(strings.TrimSpace(safe), ". ")
	for len(safe) > 180 {
		runes := []rune(safe)
		safe = string(runes[:len(runes)-1])
	}
	if safe == "" {
		safe = fallback
	}
	if safe != value {
		sum := sha256.Sum256([]byte(value))
		safe += "-" + hex.EncodeToString(sum[:4])
	}
	return safe
}
