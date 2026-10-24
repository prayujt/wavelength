package subsonic

import (
	"context"
	"errors"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/playback"
	"github.com/prayujt/wavelength/internal/sharing"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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
func (a *api) shareNode(r *http.Request, record sharing.Share) (map[string]any, error) {
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

func (a *api) shareTracks(ctx context.Context, user string, ids []string) ([]string, error) {
	c, err := a.collection(ctx, user)
	if err != nil {
		return nil, err
	}
	resolved := []string{}
	for _, id := range ids {
		found := false
		for _, song := range c.songs {
			if song.ID == id {
				resolved = append(resolved, id)
				found = true
				break
			}
		}
		if !found {
			for _, album := range c.albums {
				if album.ID == id {
					for _, song := range album.Songs {
						resolved = append(resolved, song.ID)
					}
					found = true
					break
				}
			}
		}
		if !found {
			for _, song := range c.directoryTracks(id) {
				resolved = append(resolved, song.ID)
				found = true
			}
		}
		if !found {
			return nil, library.ErrNotFound
		}
	}
	return resolved, nil
}
func (a *api) shares(r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getShares", "createShare", "updateShare", "deleteShare":
	default:
		return false
	}
	ctx, user := r.Context(), r.Form.Get("u")
	account, err := a.account(ctx, user)
	if err != nil {
		legacyError(result, err)
		return true
	}
	admin := account.Roles["adminRole"]
	if endpoint == "getShares" {
		items, err := a.sharesService.List(ctx, user, admin)
		if err != nil {
			legacyError(result, err)
			return true
		}
		nodes := []map[string]any{}
		for _, item := range items {
			node, err := a.shareNode(r, item)
			if err != nil {
				legacyError(result, err)
				return true
			}
			nodes = append(nodes, node)
		}
		payload(result, "shares", map[string]any{"share": nodes})
		return true
	}
	id := r.Form.Get("id")
	if endpoint == "deleteShare" {
		if err := a.sharesService.Delete(ctx, user, id, admin); err != nil {
			legacyError(result, err)
		}
		return true
	}
	expires, err := int64Parameter(r, "expires", 0)
	if err != nil {
		failure(result, 10, err.Error())
		return true
	}
	if endpoint == "createShare" {
		if a.publicURL == "" {
			failure(result, 0, "WAVELENGTH_SUBSONIC_PUBLIC_URL is required for sharing")
			return true
		}
		ids, err := a.shareTracks(ctx, user, r.Form["id"])
		if err != nil {
			legacyError(result, err)
			return true
		}
		item, err := a.sharesService.Create(ctx, user, r.Form.Get("description"), expires, ids)
		if err != nil {
			legacyError(result, err)
			return true
		}
		node, err := a.shareNode(r, item)
		if err != nil {
			legacyError(result, err)
		} else {
			payload(result, "shares", map[string]any{"share": []any{node}})
		}
		return true
	}
	change := sharing.Changes{Description: optionalString(r, "description")}
	if r.Form.Has("expires") {
		change.Expires = &expires
	}
	if err := a.sharesService.Update(ctx, user, id, admin, change); err != nil {
		legacyError(result, err)
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
	record, err := a.sharesService.Resolve(r.Context(), parts[0])
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if len(parts) == 3 {
		if err := a.sharesService.Authorize(r.Context(), parts[0], parts[2]); err != nil {
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
		if err := a.sharesService.Visit(r.Context(), record.ID); err != nil {
			http.NotFound(w, r)
			return true
		}
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
