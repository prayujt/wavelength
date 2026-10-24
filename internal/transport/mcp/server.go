// Package mcp exposes existing feature services over authenticated Streamable HTTP.
package mcp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/userdata"
)

type object = map[string]any
type empty struct{}
type pageInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"Page size from 1 to 500; defaults to 100"`
	Offset int `json:"offset,omitempty" jsonschema:"Number of results to skip"`
}
type browseInput struct {
	Query  string `json:"query,omitempty" jsonschema:"Search text in the local library"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}
type idInput struct {
	ID string `json:"id" jsonschema:"ID returned by a library or playlist tool"`
}
type albumsInput struct {
	Query    string `json:"query,omitempty"`
	ArtistID string `json:"artist_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

func Handler(lib *library.Library, data *userdata.Service, manager *auth.Manager) http.Handler {
	s := mcp.NewServer(&mcp.Implementation{Name: "wavelength", Version: "1.0.0"}, &mcp.ServerOptions{
		Instructions: "Manage the authenticated listener's music library and personal collections. Library search is local; external discovery and song acquisition are not available. Playback links require authentication and never contain credentials.",
	})
	add(s, "get_current_user", "Show the signed-in Wavelength listener.", false, false, func(ctx context.Context, _ empty) (object, error) {
		u, ok := auth.UserFromContext(ctx)
		if !ok {
			return nil, auth.ErrUnauthenticated
		}
		return object{"user": object{"id": u.ID, "name": u.Name, "email": u.Email}}, nil
	})
	add(s, "list_tracks", "Browse or search tracks already in the local library.", false, false, func(ctx context.Context, in browseInput) (object, error) {
		limit, offset, err := pagination(in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		page, err := lib.List(ctx, library.Query{Search: in.Query, Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		return object{"tracks": tracks(page.Tracks), "total": page.Total}, nil
	})
	add(s, "get_track", "Get a local track's metadata by ID.", false, false, func(ctx context.Context, in idInput) (object, error) {
		t, err := lib.Get(ctx, in.ID)
		return object{"track": track(t)}, err
	})
	add(s, "list_artists", "Browse or search artists already in the library.", false, false, func(ctx context.Context, in browseInput) (object, error) {
		limit, offset, err := pagination(in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		c, err := lib.Catalog(ctx)
		if err != nil {
			return nil, err
		}
		items := []object{}
		for _, a := range c.Artists {
			if contains(a.Name, in.Query) {
				items = append(items, artist(a))
			}
		}
		return object{"artists": page(items, offset, limit), "total": len(items)}, nil
	})
	add(s, "get_artist", "Get an artist and their albums from the library.", false, false, func(ctx context.Context, in idInput) (object, error) {
		c, err := lib.Catalog(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range c.Artists {
			if a.ID == in.ID {
				albums := []object{}
				for _, b := range a.Albums {
					albums = append(albums, album(b))
				}
				return object{"artist": artist(a), "albums": albums}, nil
			}
		}
		return nil, library.ErrNotFound
	})
	add(s, "list_albums", "Browse or search local albums, optionally for one artist.", false, false, func(ctx context.Context, in albumsInput) (object, error) {
		limit, offset, err := pagination(in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		c, err := lib.Catalog(ctx)
		if err != nil {
			return nil, err
		}
		items := []object{}
		for _, a := range c.Albums {
			if (in.ArtistID == "" || a.ArtistID == in.ArtistID) && contains(a.Name+" "+a.Artist, in.Query) {
				items = append(items, album(a))
			}
		}
		return object{"albums": page(items, offset, limit), "total": len(items)}, nil
	})
	add(s, "get_album", "Get a local album and its ordered tracks.", false, false, func(ctx context.Context, in idInput) (object, error) {
		c, err := lib.Catalog(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range c.Albums {
			if a.ID == in.ID {
				return object{"album": album(a), "tracks": tracks(a.Tracks)}, nil
			}
		}
		return nil, library.ErrNotFound
	})
	add(s, "get_playback_info", "Get an authenticated HTTP playback link. Open it in a signed-in browser, or send an OAuth token with wavelength:stream. Audio bytes are not returned through MCP.", false, false, func(ctx context.Context, in idInput) (object, error) {
		t, err := lib.Get(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		path := "/media/tracks/" + t.ID
		return object{"track": track(t), "http_path": path, "url": strings.TrimSuffix(manager.ResourceURL(), "/mcp") + path, "supports_ranges": true, "authentication_required": true}, nil
	})
	registerPersonal(s, data)
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 1 << 20, PropagateRequestCancellation: true,
	})
	// The SDK does not enable Origin checks by default. Native public URL is
	// authoritative; no forwarding headers or client-provided host is trusted.
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin != strings.TrimSuffix(manager.ResourceURL(), "/mcp") {
			http.Error(w, "origin is not allowed", 403)
			return
		}
		stream.ServeHTTP(w, r)
	})
	return manager.ProtectMCP(protected)
}

func add[In any](s *mcp.Server, name, description string, write, destructive bool, run func(context.Context, In) (object, error)) {
	closed := false
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !write, DestructiveHint: &destructive, OpenWorldHint: &closed}},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, object, error) {
			if _, err := auth.PersonalID(ctx, write); err != nil {
				return toolError(err), nil, nil
			}
			out, err := run(ctx, in)
			if err != nil {
				return toolError(err), nil, nil
			}
			return nil, out, nil
		})
}
func toolError(err error) *mcp.CallToolResult {
	message := "Wavelength operation failed."
	var denied *auth.RequestError
	switch {
	case errors.As(err, &denied):
		message = denied.Message
	case errors.Is(err, userdata.ErrForbidden):
		message = "You do not have permission to access or change this collection."
	case errors.Is(err, userdata.ErrInvalid):
		message = "Invalid input. Check IDs, limits, positions and timestamps."
	case errors.Is(err, state.ErrNotFound), errors.Is(err, library.ErrNotFound):
		message = "The requested item was not found."
	case errors.Is(err, context.Canceled):
		message = "Operation cancelled."
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
func pagination(limit, offset int) (int, int, error) {
	if limit < 0 || limit > 500 || offset < 0 {
		return 0, 0, userdata.ErrInvalid
	}
	if limit == 0 {
		limit = 100
	}
	return limit, offset, nil
}
func page[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := len(items)
	if limit < end-offset {
		end = offset + limit
	}
	return items[offset:end]
}
func contains(value, query string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(query))
}
func track(t library.Track) object {
	return object{"id": t.ID, "title": t.Title, "artist": t.Artist, "album": t.Album, "album_artist": t.AlbumArtist, "track_number": t.TrackNumber, "disc_number": t.DiscNumber, "year": t.Year, "genre": t.Genre, "duration_seconds": t.Duration, "size_bytes": t.Size, "content_type": t.ContentType, "bit_rate_kbps": t.BitRate, "created_at": t.CreatedAt, "modified_at": t.ModifiedAt}
}
func tracks(items []library.Track) []object {
	out := []object{}
	for _, t := range items {
		out = append(out, track(t))
	}
	return out
}
func artist(a library.Artist) object {
	return object{"id": a.ID, "name": a.Name, "album_count": len(a.Albums)}
}
func album(a library.Album) object {
	return object{"id": a.ID, "name": a.Name, "artist_id": a.ArtistID, "artist": a.Artist, "year": a.Year, "genre": a.Genre, "track_count": len(a.Tracks), "duration_seconds": a.Duration, "created_at": a.CreatedAt}
}
