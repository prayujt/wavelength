package subsonic

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/userdata"
)

func (a *api) userData(r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getPlaylists", "getPlaylist", "createPlaylist", "updatePlaylist", "deletePlaylist":
		return a.playlists(r, result, endpoint)
	case "scrobble", "getNowPlaying":
		return a.listening(r, result, endpoint)
	case "getBookmarks", "createBookmark", "deleteBookmark":
		return a.bookmarks(r, result, endpoint)
	case "getPlayQueue", "savePlayQueue":
		return a.queue(r, result, endpoint)

	}
	return false
}

func (a *api) listening(r *http.Request, result *response, endpoint string) bool {
	ctx, user := r.Context(), r.Form.Get("u")
	if endpoint == "getNowPlaying" {
		items, err := a.data.NowPlaying(ctx)
		if err != nil {
			legacyError(result, err)
			return true
		}
		entries := []map[string]any{}
		for _, item := range items {
			track, err := a.lib.Get(ctx, item.ID)
			if errors.Is(err, library.ErrNotFound) {
				continue
			}
			if err != nil {
				legacyError(result, err)
				return true
			}
			node := asMap(asSong(track))
			node["username"] = item.Username
			node["playerName"] = item.Player
			node["playerId"] = len(entries) + 1
			node["minutesAgo"] = int(time.Since(item.Time).Minutes())
			entries = append(entries, node)
		}
		payload(result, "nowPlaying", map[string]any{"entry": entries})
		return true
	}
	ids, times := r.Form["id"], r.Form["time"]
	if len(times) > 0 && len(times) != len(ids) {
		failure(result, 10, "One time is required per media ID")
		return true
	}
	submitted := true
	if r.Form.Has("submission") {
		value, err := strconv.ParseBool(r.Form.Get("submission"))
		if err != nil {
			failure(result, 10, "Invalid submission")
			return true
		}
		submitted = value
	}
	stamps := make([]time.Time, len(ids))
	for i, id := range ids {
		if _, err := a.lib.Get(ctx, id); err != nil {
			legacyError(result, err)
			return true
		}
		stamps[i] = time.Now()
		if len(times) > 0 {
			millis, err := strconv.ParseInt(times[i], 10, 64)
			if err != nil || millis < 0 {
				failure(result, 10, "Invalid scrobble time")
				return true
			}
			stamps[i] = time.UnixMilli(millis)
		}
	}
	for i, id := range ids {
		if err := a.data.RecordPlay(ctx, user, r.Form.Get("c"), id, stamps[i], submitted); err != nil {
			legacyError(result, err)
			return true
		}
	}
	return true
}

func (a *api) bookmarks(r *http.Request, result *response, endpoint string) bool {
	ctx, user := r.Context(), r.Form.Get("u")
	if endpoint == "getBookmarks" {
		items, err := a.data.Bookmarks(ctx, user)
		if err != nil {
			legacyError(result, err)
			return true
		}
		entries := []map[string]any{}
		for _, b := range items {
			track, err := a.lib.Get(ctx, b.ID)
			if errors.Is(err, library.ErrNotFound) {
				continue
			}
			if err != nil {
				legacyError(result, err)
				return true
			}
			entries = append(entries, map[string]any{"position": b.Position, "username": user, "comment": b.Comment, "created": b.Created, "changed": b.Changed, "entry": asSong(track)})
		}
		payload(result, "bookmarks", map[string]any{"bookmark": entries})
		return true
	}
	var err error
	if endpoint == "deleteBookmark" {
		err = a.data.DeleteBookmark(ctx, user, r.Form.Get("id"))
	} else {
		position, e := int64Parameter(r, "position", 0)
		if e != nil {
			failure(result, 10, e.Error())
			return true
		}
		err = a.data.SaveBookmark(ctx, user, userdata.Bookmark{ID: r.Form.Get("id"), Position: position, Comment: r.Form.Get("comment")})
	}
	if err != nil {
		legacyError(result, err)
	}
	return true
}

func (a *api) queue(r *http.Request, result *response, endpoint string) bool {
	ctx, user := r.Context(), r.Form.Get("u")
	if endpoint == "savePlayQueue" {
		position, err := int64Parameter(r, "position", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		if err := a.data.SaveQueue(ctx, user, userdata.Queue{IDs: r.Form["id"], Current: r.Form.Get("current"), Position: position, ChangedBy: r.Form.Get("c")}); err != nil {
			legacyError(result, err)
		}
		return true
	}
	q, err := a.data.Queue(ctx, user)
	if errors.Is(err, state.ErrNotFound) {
		return true
	}
	if err != nil {
		legacyError(result, err)
		return true
	}
	songs, err := a.songEntries(ctx, q.IDs)
	if err != nil {
		legacyError(result, err)
		return true
	}
	payload(result, "playQueue", map[string]any{"current": q.Current, "position": q.Position, "username": user, "changed": q.Changed, "changedBy": q.ChangedBy, "entry": songs})
	return true
}

func (a *api) songEntries(ctx context.Context, ids []string) ([]song, error) {
	out := []song{}
	for _, id := range ids {
		track, err := a.lib.Get(ctx, id)
		if errors.Is(err, library.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, asSong(track))
	}
	return out, nil
}
