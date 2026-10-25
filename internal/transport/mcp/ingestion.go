package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
)

type Acquisition struct {
	Discovery *discovery.Service
	Ingest    *ingest.Service
}
type metadataRefInput struct {
	Ref discovery.Ref `json:"ref"`
}
type lookupInput struct {
	Refs []discovery.Ref `json:"metadata_refs"`
}
type sourcesInput struct {
	Ref discovery.Ref `json:"metadata_ref"`
}
type retryInput struct {
	ID        string            `json:"id"`
	Overrides []ingest.Override `json:"overrides,omitempty"`
}

func job(j ingest.Job) object {
	items := []object{}
	for _, i := range j.Items {
		warnings := i.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		item := object{"metadata_ref": i.MetadataRef, "source_ref": i.SourceRef, "state": i.State, "error": i.Error, "track_id": i.TrackID, "reused": i.Reused, "warnings": warnings}
		if i.Metadata != nil {
			item["metadata"] = i.Metadata
		}
		items = append(items, item)
	}
	return object{"id": j.ID, "state": j.State, "error": j.Error, "items": items, "release_ref": j.Request.ReleaseRef, "playlist_id": j.Request.PlaylistID, "created_at": j.CreatedAt.Format(time.RFC3339Nano), "updated_at": j.UpdatedAt.Format(time.RFC3339Nano)}
}
func addAcquisition[In any](s *mcp.Server, name, description string, mutate, external bool, run func(context.Context, In) (object, error)) {
	destructive := name == "cancel_import"
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !mutate, DestructiveHint: &destructive, OpenWorldHint: &external}}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, object, error) {
		if _, err := auth.PersonalID(ctx, false); err != nil {
			return toolError(err), nil, nil
		}
		if mutate {
			if err := auth.RequireScope(ctx, auth.ScopeIngest); err != nil {
				return toolError(err), nil, nil
			}
		}
		out, err := run(ctx, in)
		if err != nil {
			return toolError(err), nil, nil
		}
		return nil, out, nil
	})
}
func registerAcquisition(s *mcp.Server, a Acquisition) {
	addAcquisition(s, "lookup_imports", "Find which exact metadata references are already in the shared library. Returns local track IDs for playback; never downloads or contacts a provider.", false, false, func(ctx context.Context, in lookupInput) (object, error) {
		found, err := a.Ingest.Lookup(ctx, in.Refs)
		tracks := []object{}
		for ref, id := range found {
			tracks = append(tracks, object{"metadata_ref": ref, "track_id": id})
		}
		return object{"tracks": tracks}, err
	})
	addAcquisition(s, "search_music", "Search external music metadata for songs, albums and artists. This does not download anything; list_tracks searches the local library.", false, true, func(ctx context.Context, in browseInput) (object, error) {
		if in.Offset != 0 {
			return nil, discovery.ErrInvalid
		}
		result, err := a.Discovery.Search(ctx, in.Query, in.Limit)
		return object{"tracks": result.Tracks, "releases": result.Releases, "artists": result.Artists}, err
	})
	addAcquisition(s, "get_music_artist", "Get an external artist and available releases. Provider coverage is not a guarantee of a complete discography.", false, true, func(ctx context.Context, in metadataRefInput) (object, error) {
		artist, err := a.Discovery.Artist(ctx, in.Ref)
		return object{"artist": artist}, err
	})
	addAcquisition(s, "get_music_release", "Inspect an external release and its ordered tracks before importing.", false, true, func(ctx context.Context, in metadataRefInput) (object, error) {
		release, err := a.Discovery.Release(ctx, in.Ref)
		return object{"release": release}, err
	})
	addAcquisition(s, "find_audio_sources", "Find ranked recordings for a metadata reference. Scores describe title/artist/duration matching, not audio verification. Select a source explicitly when automatic matching is ambiguous.", false, true, func(ctx context.Context, in sourcesInput) (object, error) {
		candidates, err := a.Discovery.FindSources(ctx, in.Ref)
		return object{"candidates": candidates}, err
	})
	addAcquisition(s, "import_music", "Queue music acquisition into the shared library. Supply a request_id and either items or release_ref. Omit source_ref for conservative automatic matching. An optional owned playlist is updated in order once all tracks succeed. Requires wavelength:ingest, plus wavelength:write for a playlist.", true, true, func(ctx context.Context, in ingest.Request) (object, error) {
		if in.PlaylistID != "" {
			if err := auth.RequireScope(ctx, auth.ScopeWrite); err != nil {
				return nil, err
			}
		}
		j, err := a.Ingest.Create(ctx, owner(ctx), in)
		return job(j), err
	})
	addAcquisition(s, "get_import", "Read your import's progress, errors and resulting local track IDs. Poll after import_music returns.", false, false, func(ctx context.Context, in idInput) (object, error) {
		j, err := a.Ingest.Get(ctx, owner(ctx), in.ID)
		return job(j), err
	})
	addAcquisition(s, "list_imports", "List your import jobs; another listener's jobs are private.", false, false, func(ctx context.Context, in pageInput) (object, error) {
		jobs, total, err := a.Ingest.List(ctx, owner(ctx), in.Limit, in.Offset)
		items := []object{}
		for _, j := range jobs {
			items = append(items, job(j))
		}
		return object{"imports": items, "total": total}, err
	})
	addAcquisition(s, "cancel_import", "Cancel your unfinished import. Completed tracks remain in the library; unfinished downloads are stopped and cleaned up. Requires wavelength:ingest.", true, false, func(ctx context.Context, in idInput) (object, error) {
		j, err := a.Ingest.Cancel(ctx, owner(ctx), in.ID)
		return job(j), err
	})
	addAcquisition(s, "retry_import", "Retry your failed, cancelled or ambiguous import without repeating completed tracks. Optional zero-based source overrides resolve needs_selection items. Requires wavelength:ingest.", true, true, func(ctx context.Context, in retryInput) (object, error) {
		current, err := a.Ingest.Get(ctx, owner(ctx), in.ID)
		if err != nil {
			return nil, err
		}
		if current.Request.PlaylistID != "" {
			if err = auth.RequireScope(ctx, auth.ScopeWrite); err != nil {
				return nil, err
			}
		}
		j, err := a.Ingest.Retry(ctx, owner(ctx), in.ID, in.Overrides)
		return job(j), err
	})
}
