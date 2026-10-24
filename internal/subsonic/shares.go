package subsonic

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/playback"
	"github.com/prayujt/wavelength/internal/state"
)

type share struct {
	ID, Owner, Description, Created, LastVisited string
	Expires                                      int64
	VisitCount                                   int64
	IDs                                          []string
}

func WithPublicURL(value string) Option {
	return func(a *api) error {
		if value == "" {
			return nil
		}
		u, err := url.Parse(value)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return errors.New("invalid Subsonic public URL")
		}
		a.publicURL = strings.TrimSuffix(value, "/")
		return nil
	}
}
func (a *api) shareNode(r *http.Request, record share) (map[string]any, error) {
	entries, err := a.songEntries(r.Context(), record.IDs)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"id": record.ID, "url": a.publicURL + "/share/" + record.ID, "description": record.Description, "username": record.Owner, "created": record.Created, "visitCount": record.VisitCount, "entry": entries}
	if record.Expires > 0 {
		out["expires"] = time.UnixMilli(record.Expires).UTC().Format(time.RFC3339)
	}
	if record.LastVisited != "" {
		out["lastVisited"] = record.LastVisited
	}
	return out, nil
}
func (a *api) shares(r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getShares", "createShare", "updateShare", "deleteShare":
	default:
		return false
	}
	ctx := r.Context()
	user := r.Form.Get("u")
	current, err := a.account(ctx, user)
	if err != nil {
		legacyError(result, err)
		return true
	}
	if endpoint == "getShares" {
		values, err := a.store.List(ctx, "shares")
		if err != nil {
			legacyError(result, err)
			return true
		}
		out := []map[string]any{}
		for _, raw := range values {
			var record share
			if err = json.Unmarshal(raw, &record); err != nil {
				legacyError(result, err)
				return true
			}
			if record.Owner != user && !current.Roles["adminRole"] {
				continue
			}
			node, err := a.shareNode(r, record)
			if err != nil {
				legacyError(result, err)
				return true
			}
			out = append(out, node)
		}
		payload(result, "shares", map[string]any{"share": out})
		return true
	}
	id := r.Form.Get("id")
	var record share
	if endpoint == "createShare" {
		if a.publicURL == "" {
			failure(result, 0, "WAVELENGTH_SUBSONIC_PUBLIC_URL is required for sharing")
			return true
		}
		ids := r.Form["id"]
		if len(ids) == 0 {
			failure(result, 10, "At least one media ID is required")
			return true
		}
		c, err := a.collection(ctx, user)
		if err != nil {
			legacyError(result, err)
			return true
		}
		expanded := []string{}
		for _, key := range ids {
			found := false
			for _, s := range c.songs {
				if s.ID == key {
					expanded = append(expanded, key)
					found = true
					break
				}
			}
			if !found {
				for _, alb := range c.albums {
					if alb.ID == key {
						for _, s := range alb.Songs {
							expanded = append(expanded, s.ID)
						}
						found = true
						break
					}
				}
			}
			if !found {
				for _, s := range c.directoryTracks(key) {
					expanded = append(expanded, s.ID)
					found = true
				}
			}
			if !found {
				failure(result, 70, "Media not found")
				return true
			}
		}
		expires, err := int64Parameter(r, "expires", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		record = share{ID: rand.Text(), Owner: user, Description: r.Form.Get("description"), Created: time.Now().UTC().Format(time.RFC3339), Expires: expires, IDs: expanded}
		id = record.ID
	} else {
		required(r, result, "id")
		if result.Error != nil {
			return true
		}
		raw, err := a.store.Get(ctx, "shares", id)
		if errors.Is(err, state.ErrNotFound) {
			failure(result, 70, "Share not found")
			return true
		}
		if err != nil {
			legacyError(result, err)
			return true
		}
		if err = json.Unmarshal(raw, &record); err != nil {
			legacyError(result, err)
			return true
		}
		if record.Owner != user && !current.Roles["adminRole"] {
			failure(result, 50, "Cannot modify another user's share")
			return true
		}
	}
	if endpoint == "deleteShare" {
		if err = a.remove(ctx, "shares", id); err != nil {
			legacyError(result, err)
		}
		return true
	}
	if endpoint == "updateShare" {
		if r.Form.Has("description") {
			record.Description = r.Form.Get("description")
		}
		if r.Form.Has("expires") {
			record.Expires, err = int64Parameter(r, "expires", 0)
			if err != nil {
				failure(result, 10, err.Error())
				return true
			}
		}
	}
	if err = a.save(ctx, "shares", id, record); err != nil {
		legacyError(result, err)
		return true
	}
	if endpoint == "createShare" {
		node, err := a.shareNode(r, record)
		if err != nil {
			legacyError(result, err)
		} else {
			payload(result, "shares", map[string]any{"share": []any{node}})
		}
	}
	return true
}

var sharePage = template.Must(template.New("share").Parse(`<!doctype html><html><head><meta name="viewport" content="width=device-width"><title>Wavelength shared music</title></head><body><h1>Shared music</h1><p>{{.Description}}</p>{{range .Tracks}}<section><h2>{{.Title}}</h2><audio controls preload="none" src="{{.URL}}"></audio></section>{{end}}</body></html>`))

func (a *api) publicShare(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/share/") {
		return false
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return true
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/share/"), "/")
	if len(parts) != 1 && (len(parts) != 3 || parts[1] != "stream") {
		http.NotFound(w, r)
		return true
	}
	raw, err := a.store.Get(r.Context(), "shares", parts[0])
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	var record share
	if json.Unmarshal(raw, &record) != nil || record.Expires > 0 && time.Now().UnixMilli() >= record.Expires {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if len(parts) == 3 {
		allowed := false
		for _, id := range record.IDs {
			allowed = allowed || id == parts[2]
		}
		if !allowed {
			http.NotFound(w, r)
			return true
		}
		if err = playback.Stream(w, r, a.lib, parts[2]); err != nil {
			http.NotFound(w, r)
		}
		return true
	}
	tracks := []struct{ Title, URL string }{}
	for _, id := range record.IDs {
		track, err := a.lib.Get(r.Context(), id)
		if err != nil {
			continue
		}
		tracks = append(tracks, struct{ Title, URL string }{track.Title, "/share/" + record.ID + "/stream/" + id})
	}
	if r.Method == "GET" {
		_ = a.store.Update(r.Context(), "shares", record.ID, func(old json.RawMessage) (json.RawMessage, error) {
			if len(old) == 0 {
				return nil, state.ErrNotFound
			}
			var latest share
			if err := json.Unmarshal(old, &latest); err != nil {
				return nil, err
			}
			latest.VisitCount++
			latest.LastVisited = time.Now().UTC().Format(time.RFC3339)
			return json.Marshal(latest)
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; media-src 'self'; style-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	if r.Method != "HEAD" {
		_ = sharePage.Execute(w, struct {
			Description string
			Tracks      []struct{ Title, URL string }
		}{record.Description, tracks})
	}
	return true
}
