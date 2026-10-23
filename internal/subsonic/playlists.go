package subsonic

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/prayujt/wavelength/internal/state"
)

func (a *api) playlists(r *http.Request, result *response, endpoint string) bool {
	ctx := r.Context()
	user := r.Form.Get("u")
	render := func(p savedPlaylist, entries bool) (map[string]any, error) {
		songs, err := a.songEntries(ctx, p.IDs)
		if err != nil {
			return nil, err
		}
		duration := 0
		for _, s := range songs {
			duration += s.Duration
		}
		out := map[string]any{"id": p.ID, "name": p.Name, "comment": p.Comment, "owner": p.Owner, "public": p.Public, "songCount": len(songs), "duration": duration, "created": p.Created, "changed": p.Changed}
		if len(songs) > 0 {
			out["coverArt"] = songs[0].ID
		}
		if entries {
			out["entry"] = songs
		}
		return out, nil
	}
	if endpoint == "getPlaylists" {
		records, err := a.store.List(ctx, "playlists")
		if err != nil {
			legacyError(result, err)
			return true
		}
		items := []map[string]any{}
		requested := r.Form.Get("username")

		for _, raw := range records {
			var p savedPlaylist
			if err = json.Unmarshal(raw, &p); err != nil {
				legacyError(result, err)
				return true
			}
			if (requested != "" && p.Owner != requested) || (p.Owner != user && !p.Public) {
				continue
			}
			node, err := render(p, false)
			if err != nil {
				legacyError(result, err)
				return true
			}
			items = append(items, node)
		}
		sort.Slice(items, func(i, j int) bool { return items[i]["name"].(string) < items[j]["name"].(string) })
		payload(result, "playlists", map[string]any{"playlist": items})
		return true
	}
	if endpoint == "getPlaylist" {
		id := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		raw, err := a.store.Get(ctx, "playlists", id)
		if errors.Is(err, state.ErrNotFound) {
			failure(result, 70, "Playlist not found")
			return true
		}
		if err != nil {
			legacyError(result, err)
			return true
		}
		var p savedPlaylist
		if err = json.Unmarshal(raw, &p); err != nil {
			legacyError(result, err)
			return true
		}
		if p.Owner != user && !p.Public {
			failure(result, 50, "Playlist is private")
			return true
		}
		node, err := render(p, true)
		if err != nil {
			legacyError(result, err)
			return true
		}
		payload(result, "playlist", node)
		return true
	}
	id := r.Form.Get("playlistId")
	if endpoint == "deletePlaylist" {
		id = required(r, result, "id")
		if result.Error != nil {
			return true
		}
	}
	creating := endpoint == "createPlaylist" && id == ""
	if creating {
		required(r, result, "name")
		if result.Error != nil {
			return true
		}
		id = rand.Text()
	} else if id == "" {
		failure(result, 10, "Parameter playlistId is required")
		return true
	}
	ids := r.Form["songIdToAdd"]
	if endpoint == "createPlaylist" {
		ids = r.Form["songId"]
	}
	for _, trackID := range ids {
		if _, err := a.lib.Get(ctx, trackID); err != nil {
			legacyError(result, err)
			return true
		}
	}
	var changed savedPlaylist
	err := a.store.Update(ctx, "playlists", id, func(raw json.RawMessage) (json.RawMessage, error) {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		p := savedPlaylist{ID: id, Owner: user, Created: now, Changed: now, IDs: []string{}}
		if !creating {
			if len(raw) == 0 {
				return nil, state.ErrNotFound
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			if p.Owner != user {
				return nil, errForbidden
			}
		}
		if endpoint == "deletePlaylist" {
			return nil, nil
		}
		if r.Form.Has("name") {
			p.Name = r.Form.Get("name")
		}
		if p.Name == "" {
			return nil, errInvalid
		}
		if r.Form.Has("comment") {
			p.Comment = r.Form.Get("comment")
		}
		if r.Form.Has("public") {
			public, err := strconv.ParseBool(r.Form.Get("public"))
			if err != nil {
				return nil, errInvalid
			}
			p.Public = public
		}
		if endpoint == "createPlaylist" {
			p.IDs = append([]string{}, ids...)
		} else {
			remove := map[int]bool{}
			for _, value := range r.Form["songIndexToRemove"] {
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || index >= len(p.IDs) {
					return nil, errInvalid
				}
				remove[index] = true
			}
			kept := []string{}
			for i, value := range p.IDs {
				if !remove[i] {
					kept = append(kept, value)
				}
			}
			p.IDs = append(kept, ids...)
		}
		p.Changed = now
		changed = p
		return json.Marshal(p)
	})
	if err != nil {
		switch {
		case errors.Is(err, state.ErrNotFound):
			failure(result, 70, "Playlist not found")
		case errors.Is(err, errForbidden):
			failure(result, 50, "Only the playlist owner can modify it")
		case errors.Is(err, errInvalid):
			failure(result, 10, "Invalid playlist parameters")
		default:
			legacyError(result, err)
		}
		return true
	}
	if endpoint == "createPlaylist" {
		node, err := render(changed, true)
		if err != nil {
			legacyError(result, err)
		} else {
			payload(result, "playlist", node)
		}
	}
	return true
}

var errForbidden = errors.New("forbidden")
var errInvalid = errors.New("invalid parameters")
