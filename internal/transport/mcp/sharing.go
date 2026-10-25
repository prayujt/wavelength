package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/sharing"
)

type playbackLinkInput struct {
	TrackID   string `json:"track_id" jsonschema:"Local library track ID"`
	ExpiresIn int    `json:"expires_in_seconds,omitempty" jsonschema:"Lifetime from 60 to 86400 seconds; defaults to 3600"`
}
type shareInput struct {
	Name      string   `json:"name,omitempty"`
	TrackIDs  []string `json:"track_ids" jsonschema:"Ordered local track IDs; snapshots an album or playlist when all its IDs are supplied"`
	ExpiresAt string   `json:"expires_at,omitempty" jsonschema:"Optional RFC3339 expiry; omit for no expiry"`
}
type accessListInput struct {
	Kind   string `json:"kind,omitempty" jsonschema:"share (default) or playback"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

func accessRecord(g sharing.Grant) object {
	out := object{"id": g.ID, "kind": g.Kind, "name": g.Name, "track_ids": g.TrackIDs, "created_at": g.CreatedAt}
	if !g.ExpiresAt.IsZero() {
		out["expires_at"] = g.ExpiresAt
	}
	if !g.RevokedAt.IsZero() {
		out["revoked_at"] = g.RevokedAt
	}
	return out
}
func registerSharing(s *mcp.Server, links *sharing.Service) {
	// Creating a playback credential needs stream permission, not permission to edit collections.
	destructive, closed := false, false
	mcp.AddTool(s, &mcp.Tool{Name: "create_playback_link", Description: "Create a temporary direct audio URL for Sonos or another player. Requires wavelength:stream. Anyone holding the URL may fetch this track until expiry; do not create a public share page. Returns URL, expiry and revocable link ID.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &closed}}, func(ctx context.Context, _ *mcp.CallToolRequest, in playbackLinkInput) (*mcp.CallToolResult, object, error) {
		owner, err := auth.PersonalID(ctx, false)
		if err == nil {
			err = auth.RequireScope(ctx, auth.ScopeStream)
		}
		if err != nil {
			return toolError(err), nil, nil
		}
		if in.ExpiresIn < 0 || in.ExpiresIn > 86400 {
			return toolError(sharing.ErrInvalid), nil, nil
		}
		link, err := links.CreatePlayback(ctx, owner, in.TrackID, time.Duration(in.ExpiresIn)*time.Second)
		if err != nil {
			return toolError(err), nil, nil
		}
		return nil, object{"link": accessRecord(link.Grant), "url": link.URL, "authentication_required": false, "supports_ranges": true}, nil
	})
	add(s, "create_share", "Create an explicitly public, revocable player link for selected songs. Requires wavelength:write and wavelength:stream. Anyone with the link can listen; URL is returned only on creation.", true, false, func(ctx context.Context, in shareInput) (object, error) {
		if err := auth.RequireScope(ctx, auth.ScopeStream); err != nil {
			return nil, err
		}
		owner, _ := auth.PersonalID(ctx, true)
		var expires time.Time
		if in.ExpiresAt != "" {
			var err error
			expires, err = time.Parse(time.RFC3339, in.ExpiresAt)
			if err != nil {
				return nil, sharing.ErrInvalid
			}
		}
		link, err := links.CreateShare(ctx, owner, in.Name, in.TrackIDs, expires)
		return object{"link": accessRecord(link.Grant), "url": link.URL}, err
	})
	add(s, "list_access_links", "List your shares or temporary playback grants. Does not reveal bearer tokens or URLs.", false, false, func(ctx context.Context, in accessListInput) (object, error) {
		owner, _ := auth.PersonalID(ctx, false)
		if in.Kind == "" {
			in.Kind = sharing.Share
		}
		if in.Limit == 0 {
			in.Limit = 100
		}
		grants, err := links.List(ctx, owner, in.Kind, in.Limit, in.Offset)
		out := []object{}
		for _, g := range grants {
			out = append(out, accessRecord(g))
		}
		return object{"links": out}, err
	})
	add(s, "revoke_access_link", "Revoke your share or temporary playback link. Revoking a share also blocks playback links derived from it. Requires wavelength:write.", true, true, func(ctx context.Context, in idInput) (object, error) {
		owner, _ := auth.PersonalID(ctx, true)
		return object{"revoked": true}, links.Revoke(ctx, owner, in.ID)
	})
}
