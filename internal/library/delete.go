package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrInvalid        = errors.New("invalid library deletion")
	ErrBusy           = errors.New("an unfinished import may be using this track; finish or cancel it first")
	ErrCleanupPending = errors.New("record deleted; file cleanup is pending, retry deletion to finish")
	ErrReadOnly       = errors.New("library deletion is unavailable")
)

type Deletion struct {
	TrackID string
	Paths   []string
	Done    bool
}

type deletionStore interface {
	BeginDelete(context.Context, string, string, func([]string) error) (Deletion, error)
	PendingDeletions(context.Context, string) ([]Deletion, error)
	FinishDelete(context.Context, string, string) error
}

// ObserveDeletions is installed during application assembly before requests start.
func (l *Library) ObserveDeletions(notify func(string)) {
	l.deleteMu.Lock()
	defer l.deleteMu.Unlock()
	l.deleted = append(l.deleted, notify)
}

// Delete removes the catalog and references atomically, then drains a durable
// file-cleanup receipt. Repeating a completed deletion succeeds without touching
// files again. Only catalog-derived paths are accepted, never client paths.
func (l *Library) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" || len(id) > 256 {
		return ErrInvalid
	}
	store, ok := l.store.(deletionStore)
	if !ok {
		return ErrReadOnly
	}
	l.deleteMu.Lock()
	defer l.deleteMu.Unlock()
	d, err := store.BeginDelete(ctx, l.rootID, id, l.validateDeletion)
	if err != nil {
		return err
	}
	err = l.finishDeletion(ctx, store, d)
	for _, notify := range l.deleted {
		notify(id)
	}
	return err
}

// RecoverDeletions reads explicit pending receipts only; it never walks the library.
func (l *Library) RecoverDeletions(ctx context.Context) error {
	store, ok := l.store.(deletionStore)
	if !ok {
		return nil
	}
	l.deleteMu.Lock()
	defer l.deleteMu.Unlock()
	pending, err := store.PendingDeletions(ctx, l.rootID)
	if err != nil {
		return err
	}
	var failures error
	for _, d := range pending {
		failures = errors.Join(failures, l.finishDeletion(ctx, store, d))
	}
	return failures
}

func (l *Library) validateDeletion(paths []string) error {
	for _, path := range paths {
		if !filepath.IsLocal(path) || filepath.Clean(path) == "." {
			return ErrInvalid
		}
		// os.Root confines removal; rejecting symlink parents also prevents an alias
		// inside the library from unlinking another catalog record's real asset.
		parent := filepath.Dir(path)
		for parent != "." {
			info, err := l.root.Lstat(parent)
			if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
				return ErrInvalid
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			parent = filepath.Dir(parent)
		}
		info, err := l.root.Lstat(path)
		if err == nil && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return ErrInvalid
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (l *Library) finishDeletion(ctx context.Context, store deletionStore, d Deletion) error {
	if d.Done {
		return nil
	}
	if err := l.validateDeletion(d.Paths); err != nil {
		return fmt.Errorf("%w: %v", ErrCleanupPending, err)
	}
	for _, path := range d.Paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := l.root.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrCleanupPending, err)
		}
		parent, err := l.root.Open(filepath.Dir(path))
		if err == nil {
			err = parent.Sync()
			parent.Close()
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrCleanupPending, err)
		}
	}
	if err := store.FinishDelete(ctx, l.rootID, d.TrackID); err != nil {
		return fmt.Errorf("%w: %v", ErrCleanupPending, err)
	}
	// Remove empty album/artist directories only. Unknown files and the root stay.
	for _, path := range d.Paths {
		for dir := filepath.Dir(path); dir != "."; dir = filepath.Dir(dir) {
			if err := l.root.Remove(dir); err != nil {
				break
			}
		}
	}
	return nil
}
