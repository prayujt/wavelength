package subsonic

import (
	"net/http"
	"strconv"

	"github.com/prayujt/wavelength/internal/userdata"
)

func (a *api) playlistNode(r *http.Request, p userdata.Playlist, entries bool) (map[string]any, error) {
	songs, err := a.songEntries(r.Context(), p.IDs)
	if err != nil {
		return nil, err
	}
	duration := 0
	for _, song := range songs {
		duration += song.Duration
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

func (a *api) playlists(r *http.Request, result *response, endpoint string) bool {
	ctx, user := r.Context(), r.Form.Get("u")
	if endpoint == "getPlaylists" {
		account, err := a.account(ctx, user)
		if err != nil {
			legacyError(result, err)
			return true
		}
		items, err := a.data.Playlists(ctx, user, r.Form.Get("username"), account.Roles["adminRole"])
		if err != nil {
			legacyError(result, err)
			return true
		}
		nodes := []map[string]any{}
		for _, item := range items {
			node, err := a.playlistNode(r, item, false)
			if err != nil {
				legacyError(result, err)
				return true
			}
			nodes = append(nodes, node)
		}
		payload(result, "playlists", map[string]any{"playlist": nodes})
		return true
	}
	if endpoint == "getPlaylist" {
		item, err := a.data.Playlist(ctx, user, r.Form.Get("id"))
		if err != nil {
			legacyError(result, err)
			return true
		}
		node, err := a.playlistNode(r, item, true)
		if err != nil {
			legacyError(result, err)
			return true
		}
		payload(result, "playlist", node)
		return true
	}
	if endpoint == "deletePlaylist" {
		if err := a.data.DeletePlaylist(ctx, user, r.Form.Get("id")); err != nil {
			legacyError(result, err)
		}
		return true
	}
	var item userdata.Playlist
	var err error
	if endpoint == "createPlaylist" && r.Form.Get("playlistId") == "" {
		item, err = a.data.CreatePlaylist(ctx, user, r.Form.Get("name"), r.Form["songId"])
	} else {
		change := userdata.PlaylistChanges{Name: optionalString(r, "name"), Comment: optionalString(r, "comment")}
		if r.Form.Has("public") {
			value, e := strconv.ParseBool(r.Form.Get("public"))
			if e != nil {
				failure(result, 10, "Invalid public")
				return true
			}
			change.Public = &value
		}
		if endpoint == "createPlaylist" {
			ids := r.Form["songId"]
			change.Replace = &ids
		} else {
			change.Add = r.Form["songIdToAdd"]
			for _, value := range r.Form["songIndexToRemove"] {
				index, e := strconv.Atoi(value)
				if e != nil {
					failure(result, 10, "Invalid playlist index")
					return true
				}
				change.Remove = append(change.Remove, index)
			}
		}
		item, err = a.data.UpdatePlaylist(ctx, user, r.Form.Get("playlistId"), change)
	}
	if err != nil {
		legacyError(result, err)
		return true
	}
	if endpoint == "createPlaylist" {
		node, err := a.playlistNode(r, item, true)
		if err != nil {
			legacyError(result, err)
		} else {
			payload(result, "playlist", node)
		}
	}
	return true
}

func optionalString(r *http.Request, key string) *string {
	if !r.Form.Has(key) {
		return nil
	}
	value := r.Form.Get(key)
	return &value
}
