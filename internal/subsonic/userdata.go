package subsonic

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
)

type annotation struct {
	Starred    string `json:"starred,omitempty"`
	Rating     int    `json:"rating"`
	Plays      int    `json:"plays"`
	LastPlayed string `json:"last_played,omitempty"`
}

func (a *api) annotations(ctx context.Context, user string) (map[string]annotation, error) {
	values, err := a.store.List(ctx, "annotations/"+user)
	if err != nil {
		return nil, err
	}
	out := map[string]annotation{}
	for key, value := range values {
		var record annotation
		if err = json.Unmarshal(value, &record); err != nil {
			return nil, err
		}
		out[key] = record
	}
	return out, nil
}
func (a *api) save(ctx context.Context, scope, key string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return a.store.Update(ctx, scope, key, func(json.RawMessage) (json.RawMessage, error) { return body, nil })
}
func (a *api) remove(ctx context.Context, scope, key string) error {
	return a.store.Update(ctx, scope, key, func(json.RawMessage) (json.RawMessage, error) { return nil, nil })
}
func (a *api) annotation(ctx context.Context, user, id string, update func(*annotation)) error {
	return a.store.Update(ctx, "annotations/"+user, id, func(old json.RawMessage) (json.RawMessage, error) {
		var record annotation
		if len(old) > 0 {
			if err := json.Unmarshal(old, &record); err != nil {
				return nil, err
			}
		}
		update(&record)
		return json.Marshal(record)
	})
}

type savedPlaylist struct {
	ID, Name, Comment, Owner, Created, Changed string
	Public                                     bool
	IDs                                        []string
}
type bookmark struct {
	ID, Comment, Created, Changed string
	Position                      int64
}
type savedQueue struct {
	IDs                         []string
	Current, Changed, ChangedBy string
	Position                    int64
}
type nowPlaying struct {
	ID, Username, Player string
	Time                 time.Time
}

func (a *api) userData(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	user := r.Form.Get("u")
	ctx := r.Context()
	switch endpoint {

	case "star", "unstar", "setRating":
		c, err := a.collection(ctx, user)
		if err != nil {
			legacyError(result, err)
			return true
		}
		valid := c.directoryIDs()
		for _, s := range c.songs {
			valid[s.ID] = true
		}
		for _, v := range c.albums {
			valid[v.ID] = true
		}
		for _, v := range c.artists {
			valid[v.ID] = true
		}
		ids := append(append(append([]string{}, r.Form["id"]...), r.Form["albumId"]...), r.Form["artistId"]...)
		if len(ids) == 0 {
			failure(result, 10, "At least one media ID is required")
			return true
		}
		for _, id := range ids {
			if !valid[id] {
				failure(result, 70, "Media not found")
				return true
			}
		}
		rating := 0
		if endpoint == "setRating" {
			if len(r.Form["id"]) != 1 || !r.Form.Has("rating") {
				failure(result, 10, "Parameters id and rating are required")
				return true
			}
			rating, err = number(r, "rating", 0, 5)
			if err != nil {
				failure(result, 10, err.Error())
				return true
			}
		}
		for _, id := range ids {
			err = a.annotation(ctx, user, id, func(record *annotation) {
				switch endpoint {
				case "star":
					record.Starred = time.Now().UTC().Format(time.RFC3339)
				case "unstar":
					record.Starred = ""
				case "setRating":
					record.Rating = rating
				}
			})
			if err != nil {
				legacyError(result, err)
				return true
			}
		}
		return true
	case "getPlaylists", "getPlaylist", "createPlaylist", "updatePlaylist", "deletePlaylist":
		return a.playlists(r, result, endpoint)
	case "scrobble":
		ids := r.Form["id"]
		if len(ids) == 0 {
			failure(result, 10, "Parameter id is required")
			return true
		}
		times := r.Form["time"]
		if len(times) > 0 && len(times) != len(ids) {
			failure(result, 10, "One time is required per media ID")
			return true
		}
		submission := true
		var err error
		if r.Form.Has("submission") {
			submission, err = strconv.ParseBool(r.Form.Get("submission"))
			if err != nil {
				failure(result, 10, "Invalid submission")
				return true
			}
		}
		stamps := make([]time.Time, len(ids))
		for i, id := range ids {
			if _, err = a.lib.Get(ctx, id); err != nil {
				legacyError(result, err)
				return true
			}
			stamps[i] = time.Now()
			if len(times) > 0 {
				millis, e := strconv.ParseInt(times[i], 10, 64)
				if e != nil || millis < 0 {
					failure(result, 10, "Invalid scrobble time")
					return true
				}
				stamps[i] = time.UnixMilli(millis)
			}
		}
		for i, id := range ids {
			if submission {
				err = a.annotation(ctx, user, id, func(record *annotation) {
					record.Plays++
					stamp := stamps[i].UTC().Format(time.RFC3339Nano)
					if stamp > record.LastPlayed {
						record.LastPlayed = stamp
					}
				})
			} else {
				err = a.save(ctx, "now-playing", user+"/"+r.Form.Get("c"), nowPlaying{ID: id, Username: user, Player: r.Form.Get("c"), Time: stamps[i]})
			}
			if err != nil {
				legacyError(result, err)
				return true
			}
		}
		return true
	case "getNowPlaying":
		values, err := a.store.List(ctx, "now-playing")
		if err != nil {
			legacyError(result, err)
			return true
		}
		entries := []map[string]any{}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			var record nowPlaying
			if json.Unmarshal(values[key], &record) != nil {
				failure(result, 0, "Invalid now-playing record")
				return true
			}
			if time.Since(record.Time) > 10*time.Minute {
				continue
			}
			track, err := a.lib.Get(ctx, record.ID)
			if err != nil {
				continue
			}
			node := asMap(asSong(track))
			node["username"] = record.Username
			node["playerName"] = record.Player
			node["playerId"] = len(entries) + 1
			node["minutesAgo"] = int(time.Since(record.Time).Minutes())
			entries = append(entries, node)
		}
		payload(result, "nowPlaying", map[string]any{"entry": entries})
		return true
	case "getBookmarks", "createBookmark", "deleteBookmark":
		scope := "bookmarks/" + user
		if endpoint == "getBookmarks" {
			records, err := a.store.List(ctx, scope)
			if err != nil {
				legacyError(result, err)
				return true
			}
			entries := []map[string]any{}
			keys := make([]string, 0, len(records))
			for key := range records {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				var b bookmark
				if json.Unmarshal(records[key], &b) != nil {
					failure(result, 0, "Invalid bookmark")
					return true
				}
				track, err := a.lib.Get(ctx, b.ID)
				if err != nil {
					continue
				}
				entries = append(entries, map[string]any{"position": b.Position, "username": user, "comment": b.Comment, "created": b.Created, "changed": b.Changed, "entry": asSong(track)})
			}
			payload(result, "bookmarks", map[string]any{"bookmark": entries})
			return true
		}
		id := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		if endpoint == "deleteBookmark" {
			if err := a.remove(ctx, scope, id); err != nil {
				legacyError(result, err)
			}
			return true
		}
		if _, err := a.lib.Get(ctx, id); err != nil {
			legacyError(result, err)
			return true
		}
		if !r.Form.Has("position") {
			failure(result, 10, "Parameter position is required")
			return true
		}
		position, err := int64Parameter(r, "position", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		err = a.store.Update(ctx, scope, id, func(old json.RawMessage) (json.RawMessage, error) {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			b := bookmark{ID: id, Position: position, Comment: r.Form.Get("comment"), Created: now, Changed: now}
			if len(old) > 0 {
				var previous bookmark
				if err := json.Unmarshal(old, &previous); err != nil {
					return nil, err
				}
				b.Created = previous.Created
			}
			return json.Marshal(b)
		})
		if err != nil {
			legacyError(result, err)
		}
		return true
	case "getPlayQueue", "savePlayQueue":
		if endpoint == "savePlayQueue" {
			ids := r.Form["id"]
			for _, id := range ids {
				if _, err := a.lib.Get(ctx, id); err != nil {
					legacyError(result, err)
					return true
				}
			}
			current := r.Form.Get("current")
			if current != "" {
				found := false
				for _, id := range ids {
					found = found || id == current
				}
				if !found {
					failure(result, 10, "Current song must belong to the queue")
					return true
				}
			}
			position, err := int64Parameter(r, "position", 0)
			if err != nil {
				failure(result, 10, err.Error())
				return true
			}
			if err = a.save(ctx, "queues", user, savedQueue{IDs: ids, Current: current, Position: position, Changed: time.Now().UTC().Format(time.RFC3339Nano), ChangedBy: r.Form.Get("c")}); err != nil {
				legacyError(result, err)
			}
			return true
		}
		record, err := a.store.Get(ctx, "queues", user)
		if errors.Is(err, state.ErrNotFound) {
			return true
		}
		if err != nil {
			legacyError(result, err)
			return true
		}
		var q savedQueue
		if err = json.Unmarshal(record, &q); err != nil {
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
	case "getInternetRadioStations", "createInternetRadioStation", "updateInternetRadioStation", "deleteInternetRadioStation":
		if endpoint == "getInternetRadioStations" {
			records, err := a.store.List(ctx, "radio")
			if err != nil {
				legacyError(result, err)
				return true
			}
			keys := make([]string, 0, len(records))
			for key := range records {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			stations := []json.RawMessage{}
			for _, key := range keys {
				stations = append(stations, records[key])
			}
			payload(result, "internetRadioStations", map[string]any{"internetRadioStation": stations})
			return true
		}
		id := r.Form.Get("id")
		if endpoint == "createInternetRadioStation" {
			id = rand.Text()
		} else {
			required(r, result, "id")
			if result.Error != nil {
				return true
			}
			if _, err := a.store.Get(ctx, "radio", id); err != nil {
				failure(result, 70, "Radio station not found")
				return true
			}
		}
		if endpoint == "deleteInternetRadioStation" {
			if err := a.remove(ctx, "radio", id); err != nil {
				legacyError(result, err)
			}
			return true
		}
		name := required(r, result, "name")
		stream := required(r, result, "streamUrl")
		if result.Error != nil {
			return true
		}
		if err := a.save(ctx, "radio", id, map[string]any{"id": id, "name": name, "streamUrl": stream, "homePageUrl": r.Form.Get("homepageUrl")}); err != nil {
			legacyError(result, err)
		}
		return true
	case "getChatMessages", "addChatMessage":
		if endpoint == "addChatMessage" {
			message := required(r, result, "message")
			if result.Error != nil {
				return true
			}
			if len(message) > 8192 {
				failure(result, 10, "Message is too long")
				return true
			}
			if err := a.save(ctx, "chat", rand.Text(), map[string]any{"username": user, "time": time.Now().UnixMilli(), "message": message}); err != nil {
				legacyError(result, err)
			}
			return true
		}
		since, err := int64Parameter(r, "since", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		records, err := a.store.List(ctx, "chat")
		if err != nil {
			legacyError(result, err)
			return true
		}
		messages := []map[string]any{}
		for _, raw := range records {
			var message map[string]any
			if json.Unmarshal(raw, &message) != nil {
				continue
			}
			if int64(message["time"].(float64)) > since {
				messages = append(messages, message)
			}
		}
		sort.Slice(messages, func(i, j int) bool { return messages[i]["time"].(float64) < messages[j]["time"].(float64) })
		payload(result, "chatMessages", map[string]any{"chatMessage": messages})
		return true
	}
	return false
}
func (a *api) songEntries(ctx context.Context, ids []string) ([]song, error) {
	out := []song{}
	for _, id := range ids {
		t, err := a.lib.Get(ctx, id)
		if errors.Is(err, library.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, asSong(t))
	}
	return out, nil
}
