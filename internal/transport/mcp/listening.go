package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prayujt/wavelength/internal/listening"
)

type startListeningInput struct {
	TrackID   string `json:"track_id"`
	Origin    string `json:"origin" jsonschema:"direct, queue, or restored"`
	RequestID string `json:"request_id" jsonschema:"Stable client-generated ID; reuse for retries"`
}
type listeningProgressInput struct {
	SessionID  string `json:"session_id"`
	ListenedMS int64  `json:"listened_ms" jsonschema:"Cumulative listening time, excluding pauses, buffering and seek jumps"`
	PositionMS int64  `json:"position_ms"`
	Playing    bool   `json:"playing"`
}
type finishListeningInput struct {
	SessionID  string `json:"session_id"`
	Outcome    string `json:"outcome" jsonschema:"completed, skipped, or stopped"`
	ListenedMS int64  `json:"listened_ms"`
	PositionMS int64  `json:"position_ms"`
}
type listeningQueryInput struct {
	TrackID  string `json:"track_id,omitempty"`
	ArtistID string `json:"artist_id,omitempty" jsonschema:"Local library artist ID"`
	AlbumID  string `json:"album_id,omitempty" jsonschema:"Local library album ID"`
	Since    string `json:"since,omitempty" jsonschema:"Inclusive RFC3339 session start time"`
	Until    string `json:"until,omitempty" jsonschema:"Exclusive RFC3339 session start time"`
	Origin   string `json:"origin,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

func listeningRecord(s listening.Session) object {
	out := object{"id": s.ID, "track": track(s.Track), "artist_id": s.ArtistID, "album_id": s.AlbumID, "origin": s.Origin, "outcome": s.Outcome, "started_at": s.StartedAt, "updated_at": s.UpdatedAt, "listened_ms": s.ListenedMS, "position_ms": s.PositionMS, "playing": s.Playing}
	if !s.EndedAt.IsZero() {
		out["ended_at"] = s.EndedAt
	}
	return out
}
func registerListening(s *mcp.Server, data *listening.Service) {
	add(s, "start_listening_session", "Report a track that your player started. Records activity; does not start playback. Requires wavelength:write.", true, false, func(ctx context.Context, in startListeningInput) (object, error) {
		item, err := data.Start(ctx, owner(ctx), in.TrackID, in.Origin, in.RequestID)
		return object{"session": listeningRecord(item)}, err
	})
	add(s, "report_listening_progress", "Report cumulative listening time and current position for your session. Seeking changes position, not listened time.", true, false, func(ctx context.Context, in listeningProgressInput) (object, error) {
		item, err := data.Report(ctx, owner(ctx), in.SessionID, listening.Progress{ListenedMS: in.ListenedMS, PositionMS: in.PositionMS, Playing: in.Playing})
		return object{"session": listeningRecord(item)}, err
	})
	add(s, "finish_listening_session", "Report completion, a skip, or an explicit stop. Repeating the same outcome is safe.", true, false, func(ctx context.Context, in finishListeningInput) (object, error) {
		item, err := data.Finish(ctx, owner(ctx), in.SessionID, in.Outcome, listening.Progress{ListenedMS: in.ListenedMS, PositionMS: in.PositionMS})
		return object{"session": listeningRecord(item)}, err
	})
	add(s, "list_listening_sessions", "Read your detailed listening history, newest first. Combine track, album, artist, start-date, origin and outcome filters. Metadata is captured at playback time; anonymous shares and raw audio requests are not attributed to you.", false, false, func(ctx context.Context, in listeningQueryInput) (object, error) {
		var since, until time.Time
		for i, value := range []string{in.Since, in.Until} {
			if value == "" {
				continue
			}
			stamp, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return nil, listening.ErrInvalid
			}
			if i == 0 {
				since = stamp
			} else {
				until = stamp
			}
		}
		page, err := data.List(ctx, owner(ctx), listening.Query{TrackID: in.TrackID, ArtistID: in.ArtistID, AlbumID: in.AlbumID, Since: since, Until: until, Origin: in.Origin, Outcome: in.Outcome, Limit: in.Limit, Offset: in.Offset})
		if err != nil {
			return nil, err
		}
		out := []object{}
		for _, item := range page.Sessions {
			out = append(out, listeningRecord(item))
		}
		return object{"sessions": out, "total": page.Total}, nil
	})
}
