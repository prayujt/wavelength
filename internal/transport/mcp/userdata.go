package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/userdata"
)

type createPlaylist struct {
	Name     string   `json:"name"`
	TrackIDs []string `json:"track_ids,omitempty" jsonschema:"Ordered track IDs; duplicates are allowed"`
}
type updatePlaylist struct {
	ID      string    `json:"id"`
	Name    *string   `json:"name,omitempty"`
	Comment *string   `json:"comment,omitempty"`
	Public  *bool     `json:"public,omitempty" jsonschema:"Explicitly share with other authenticated listeners; only the owner may edit"`
	Replace *[]string `json:"replace_track_ids,omitempty" jsonschema:"Replace the entire ordered track list; an empty array clears it"`
	Add     []string  `json:"add_track_ids,omitempty"`
	Remove  []int     `json:"remove_indices,omitempty" jsonschema:"Zero-based positions to remove before adding tracks"`
}
type saveQueue struct {
	TrackIDs []string `json:"track_ids"`
	Current  string   `json:"current_track_id,omitempty"`
	Position int64    `json:"position_ms,omitempty"`
}
type bookmarkInput struct {
	ID       string `json:"track_id"`
	Position int64  `json:"position_ms,omitempty"`
	Comment  string `json:"comment,omitempty"`
}
type trackIDInput struct {
	ID string `json:"track_id"`
}
type playInput struct {
	ID             string `json:"track_id"`
	PlayedAt       string `json:"played_at,omitempty" jsonschema:"RFC3339 timestamp; defaults to now"`
	NowPlayingOnly bool   `json:"now_playing_only,omitempty" jsonschema:"Update current playback without adding a history entry"`
}

func owner(ctx context.Context) string { user, _ := auth.PersonalID(ctx, false); return user }
func playlist(p userdata.Playlist) object {
	ids := p.IDs
	if ids == nil {
		ids = []string{}
	}
	return object{"id": p.ID, "name": p.Name, "comment": p.Comment, "owner_id": strings.TrimPrefix(p.Owner, "oidc:"), "public": p.Public, "track_ids": ids, "created_at": p.Created, "changed_at": p.Changed}
}
func queue(q userdata.Queue) object {
	ids := q.IDs
	if ids == nil {
		ids = []string{}
	}
	return object{"track_ids": ids, "current_track_id": q.Current, "position_ms": q.Position, "changed_at": q.Changed, "changed_by": q.ChangedBy}
}
func registerPersonal(s *mcp.Server, data *userdata.Service) {
	add(s, "list_playlists", "List your playlists and playlists explicitly shared with listeners.", false, false, func(ctx context.Context, in pageInput) (object, error) {
		limit, offset, err := pagination(in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		items, err := data.Playlists(ctx, owner(ctx), "", false)
		if err != nil {
			return nil, err
		}
		out := []object{}
		for _, p := range page(items, offset, limit) {
			out = append(out, playlist(p))
		}
		return object{"playlists": out, "total": len(items)}, nil
	})
	add(s, "get_playlist", "Get a playlist you own or can read.", false, false, func(ctx context.Context, in idInput) (object, error) {
		p, err := data.Playlist(ctx, owner(ctx), in.ID)
		return object{"playlist": playlist(p)}, err
	})
	add(s, "create_playlist", "Create a private playlist for the signed-in listener.", true, false, func(ctx context.Context, in createPlaylist) (object, error) {
		p, err := data.CreatePlaylist(ctx, owner(ctx), in.Name, in.TrackIDs)
		return object{"playlist": playlist(p)}, err
	})
	add(s, "update_playlist", "Edit your playlist's metadata or ordered tracks. Setting public shares it with other listeners. Replacement cannot be combined with add/remove.", true, true, func(ctx context.Context, in updatePlaylist) (object, error) {
		p, err := data.UpdatePlaylist(ctx, owner(ctx), in.ID, userdata.PlaylistChanges{Name: in.Name, Comment: in.Comment, Public: in.Public, Replace: in.Replace, Add: in.Add, Remove: in.Remove})
		return object{"playlist": playlist(p)}, err
	})
	add(s, "delete_playlist", "Delete a playlist owned by the signed-in listener.", true, true, func(ctx context.Context, in idInput) (object, error) {
		return object{"deleted": true}, data.DeletePlaylist(ctx, owner(ctx), in.ID)
	})
	add(s, "get_queue", "Get your saved playback queue and position.", false, false, func(ctx context.Context, _ empty) (object, error) {
		q, err := data.Queue(ctx, owner(ctx))
		if errors.Is(err, state.ErrNotFound) {
			err = nil
		}
		return object{"queue": queue(q)}, err
	})
	add(s, "save_queue", "Replace your saved playback queue, including the current track and millisecond position. An empty track list clears it.", true, true, func(ctx context.Context, in saveQueue) (object, error) {
		user := owner(ctx)
		q := userdata.Queue{IDs: in.TrackIDs, Current: in.Current, Position: in.Position, ChangedBy: "mcp"}
		if err := data.SaveQueue(ctx, user, q); err != nil {
			return nil, err
		}
		q, err := data.Queue(ctx, user)
		return object{"queue": queue(q)}, err
	})
	add(s, "list_bookmarks", "List your playback bookmarks.", false, false, func(ctx context.Context, in pageInput) (object, error) {
		limit, offset, err := pagination(in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		items, err := data.Bookmarks(ctx, owner(ctx))
		if err != nil {
			return nil, err
		}
		out := []object{}
		for _, b := range page(items, offset, limit) {
			out = append(out, object{"track_id": b.ID, "position_ms": b.Position, "comment": b.Comment, "created_at": b.Created, "changed_at": b.Changed})
		}
		return object{"bookmarks": out, "total": len(items)}, nil
	})
	add(s, "save_bookmark", "Save your playback position and comment for a track.", true, true, func(ctx context.Context, in bookmarkInput) (object, error) {
		return object{"saved": true}, data.SaveBookmark(ctx, owner(ctx), userdata.Bookmark{ID: in.ID, Position: in.Position, Comment: in.Comment})
	})
	add(s, "delete_bookmark", "Remove your bookmark for a track.", true, true, func(ctx context.Context, in trackIDInput) (object, error) {
		return object{"deleted": true}, data.DeleteBookmark(ctx, owner(ctx), in.ID)
	})
	add(s, "get_history", "List your listening history, most recently played first.", false, false, func(ctx context.Context, in pageInput) (object, error) {
		limit, offset, err := pagination(in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		items, err := data.ListeningHistory(ctx, owner(ctx))
		if err != nil {
			return nil, err
		}
		out := []object{}
		for _, h := range page(items, offset, limit) {
			out = append(out, object{"track_id": h.ID, "plays": h.Plays, "last_played_at": h.LastPlayed})
		}
		return object{"entries": out, "total": len(items)}, nil
	})
	add(s, "record_play", "Record a track you played, or update your now-playing state. This changes listening history; it does not start an audio player.", true, false, func(ctx context.Context, in playInput) (object, error) {
		stamp := time.Now()
		if in.PlayedAt != "" {
			var err error
			stamp, err = time.Parse(time.RFC3339Nano, in.PlayedAt)
			if err != nil {
				return nil, userdata.ErrInvalid
			}
		}
		if err := userdata.ValidatePlayTime(stamp); err != nil {
			return nil, err
		}
		return object{"recorded": true}, data.RecordPlay(ctx, owner(ctx), "mcp", in.ID, stamp, !in.NowPlayingOnly)
	})
	add(s, "get_now_playing", "Show your recently active playback. Other listeners' activity is private.", false, false, func(ctx context.Context, _ empty) (object, error) {
		items, err := data.PersonalNowPlaying(ctx, owner(ctx))
		if err != nil {
			return nil, err
		}
		out := []object{}
		for _, p := range items {
			out = append(out, object{"track_id": p.ID, "played_at": p.Time.UTC().Format(time.RFC3339Nano)})
		}
		return object{"entries": out}, nil
	})
}
