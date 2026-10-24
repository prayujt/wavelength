package rpc

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/userdata"
)

type personalService struct{ data *userdata.Service }

func UserDataHandler(data *userdata.Service, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewUserDataServiceHandler(&personalService{data: data}, connect.WithReadMaxBytes(1<<20))
	return path, protect(handler, manager)
}
func personal[T any](ctx context.Context, write bool, run func(string) (*T, error)) (*connect.Response[T], error) {
	user, err := auth.PersonalID(ctx, write)
	if err != nil {
		return nil, failure(err)
	}
	out, err := run(user)
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(out), nil
}
func playlist(p userdata.Playlist) *pb.Playlist {
	return &pb.Playlist{Id: p.ID, Name: p.Name, Comment: p.Comment, OwnerId: strings.TrimPrefix(p.Owner, "oidc:"), Public: p.Public, TrackIds: p.IDs, CreatedAt: p.Created, ChangedAt: p.Changed}
}
func queue(q userdata.Queue) *pb.Queue {
	return &pb.Queue{TrackIds: q.IDs, CurrentTrackId: q.Current, PositionMs: q.Position, ChangedAt: q.Changed, ChangedBy: q.ChangedBy}
}
func (s *personalService) GetCurrentUser(ctx context.Context, _ *connect.Request[pb.GetCurrentUserRequest]) (*connect.Response[pb.GetCurrentUserResponse], error) {
	return personal(ctx, false, func(string) (*pb.GetCurrentUserResponse, error) {
		u, _ := auth.UserFromContext(ctx)
		return &pb.GetCurrentUserResponse{User: &pb.User{Id: u.ID, Name: u.Name, Email: u.Email}}, nil
	})
}
func (s *personalService) ListPlaylists(ctx context.Context, r *connect.Request[pb.ListPlaylistsRequest]) (*connect.Response[pb.ListPlaylistsResponse], error) {
	limit, offset, err := pagination(r.Msg.Limit, r.Msg.Offset)
	if err != nil {
		return nil, err
	}
	return personal(ctx, false, func(user string) (*pb.ListPlaylistsResponse, error) {
		items, err := s.data.Playlists(ctx, user, "", false)
		if err != nil {
			return nil, err
		}
		out := &pb.ListPlaylistsResponse{Total: int32(len(items))}
		for _, p := range slice(items, offset, limit) {
			out.Playlists = append(out.Playlists, playlist(p))
		}
		return out, nil
	})
}
func (s *personalService) GetPlaylist(ctx context.Context, r *connect.Request[pb.GetPlaylistRequest]) (*connect.Response[pb.GetPlaylistResponse], error) {
	return personal(ctx, false, func(user string) (*pb.GetPlaylistResponse, error) {
		p, err := s.data.Playlist(ctx, user, r.Msg.Id)
		return &pb.GetPlaylistResponse{Playlist: playlist(p)}, err
	})
}
func (s *personalService) CreatePlaylist(ctx context.Context, r *connect.Request[pb.CreatePlaylistRequest]) (*connect.Response[pb.GetPlaylistResponse], error) {
	return personal(ctx, true, func(user string) (*pb.GetPlaylistResponse, error) {
		p, err := s.data.CreatePlaylist(ctx, user, r.Msg.Name, r.Msg.TrackIds)
		return &pb.GetPlaylistResponse{Playlist: playlist(p)}, err
	})
}
func (s *personalService) UpdatePlaylist(ctx context.Context, r *connect.Request[pb.UpdatePlaylistRequest]) (*connect.Response[pb.GetPlaylistResponse], error) {
	return personal(ctx, true, func(user string) (*pb.GetPlaylistResponse, error) {
		change := userdata.PlaylistChanges{Name: r.Msg.Name, Comment: r.Msg.Comment, Public: r.Msg.Public, Add: r.Msg.AddTrackIds}
		if r.Msg.ReplaceTracks != nil {
			change.Replace = &r.Msg.ReplaceTracks.Ids
		}
		for _, i := range r.Msg.RemoveIndices {
			change.Remove = append(change.Remove, int(i))
		}
		p, err := s.data.UpdatePlaylist(ctx, user, r.Msg.Id, change)
		return &pb.GetPlaylistResponse{Playlist: playlist(p)}, err
	})
}
func (s *personalService) DeletePlaylist(ctx context.Context, r *connect.Request[pb.DeletePlaylistRequest]) (*connect.Response[pb.EmptyResponse], error) {
	return personal(ctx, true, func(user string) (*pb.EmptyResponse, error) {
		return &pb.EmptyResponse{}, s.data.DeletePlaylist(ctx, user, r.Msg.Id)
	})
}
func (s *personalService) GetQueue(ctx context.Context, _ *connect.Request[pb.GetQueueRequest]) (*connect.Response[pb.GetQueueResponse], error) {
	return personal(ctx, false, func(user string) (*pb.GetQueueResponse, error) {
		q, err := s.data.Queue(ctx, user)
		if errors.Is(err, state.ErrNotFound) {
			err = nil
		}
		return &pb.GetQueueResponse{Queue: queue(q)}, err
	})
}
func (s *personalService) SaveQueue(ctx context.Context, r *connect.Request[pb.SaveQueueRequest]) (*connect.Response[pb.GetQueueResponse], error) {
	return personal(ctx, true, func(user string) (*pb.GetQueueResponse, error) {
		q := userdata.Queue{IDs: r.Msg.TrackIds, Current: r.Msg.CurrentTrackId, Position: r.Msg.PositionMs, ChangedBy: "native"}
		if err := s.data.SaveQueue(ctx, user, q); err != nil {
			return nil, err
		}
		q, err := s.data.Queue(ctx, user)
		return &pb.GetQueueResponse{Queue: queue(q)}, err
	})
}
func (s *personalService) ListBookmarks(ctx context.Context, r *connect.Request[pb.ListBookmarksRequest]) (*connect.Response[pb.ListBookmarksResponse], error) {
	limit, offset, err := pagination(r.Msg.Limit, r.Msg.Offset)
	if err != nil {
		return nil, err
	}
	return personal(ctx, false, func(user string) (*pb.ListBookmarksResponse, error) {
		items, err := s.data.Bookmarks(ctx, user)
		if err != nil {
			return nil, err
		}
		out := &pb.ListBookmarksResponse{Total: int32(len(items))}
		for _, b := range slice(items, offset, limit) {
			out.Bookmarks = append(out.Bookmarks, &pb.Bookmark{TrackId: b.ID, PositionMs: b.Position, Comment: b.Comment, CreatedAt: b.Created, ChangedAt: b.Changed})
		}
		return out, nil
	})
}
func (s *personalService) SaveBookmark(ctx context.Context, r *connect.Request[pb.SaveBookmarkRequest]) (*connect.Response[pb.EmptyResponse], error) {
	return personal(ctx, true, func(user string) (*pb.EmptyResponse, error) {
		return &pb.EmptyResponse{}, s.data.SaveBookmark(ctx, user, userdata.Bookmark{ID: r.Msg.TrackId, Position: r.Msg.PositionMs, Comment: r.Msg.Comment})
	})
}
func (s *personalService) DeleteBookmark(ctx context.Context, r *connect.Request[pb.DeleteBookmarkRequest]) (*connect.Response[pb.EmptyResponse], error) {
	return personal(ctx, true, func(user string) (*pb.EmptyResponse, error) {
		return &pb.EmptyResponse{}, s.data.DeleteBookmark(ctx, user, r.Msg.TrackId)
	})
}
func (s *personalService) GetHistory(ctx context.Context, r *connect.Request[pb.GetHistoryRequest]) (*connect.Response[pb.GetHistoryResponse], error) {
	limit, offset, err := pagination(r.Msg.Limit, r.Msg.Offset)
	if err != nil {
		return nil, err
	}
	return personal(ctx, false, func(user string) (*pb.GetHistoryResponse, error) {
		items, err := s.data.ListeningHistory(ctx, user)
		if err != nil {
			return nil, err
		}
		out := &pb.GetHistoryResponse{Total: int32(len(items))}
		for _, h := range slice(items, offset, limit) {
			out.Entries = append(out.Entries, &pb.HistoryEntry{TrackId: h.ID, Plays: int32(h.Plays), LastPlayedAt: h.LastPlayed})
		}
		return out, nil
	})
}
func (s *personalService) RecordPlay(ctx context.Context, r *connect.Request[pb.RecordPlayRequest]) (*connect.Response[pb.EmptyResponse], error) {
	return personal(ctx, true, func(user string) (*pb.EmptyResponse, error) {
		stamp := time.Now()
		if r.Msg.PlayedAt != nil {
			if err := r.Msg.PlayedAt.CheckValid(); err != nil {
				return nil, userdata.ErrInvalid
			}
			stamp = r.Msg.PlayedAt.AsTime()
		}
		if err := userdata.ValidatePlayTime(stamp); err != nil {
			return nil, err
		}
		return &pb.EmptyResponse{}, s.data.RecordPlay(ctx, user, "native", r.Msg.TrackId, stamp, !r.Msg.NowPlayingOnly)
	})
}
func (s *personalService) GetNowPlaying(ctx context.Context, _ *connect.Request[pb.GetNowPlayingRequest]) (*connect.Response[pb.GetNowPlayingResponse], error) {
	return personal(ctx, false, func(user string) (*pb.GetNowPlayingResponse, error) {
		items, err := s.data.PersonalNowPlaying(ctx, user)
		if err != nil {
			return nil, err
		}
		out := &pb.GetNowPlayingResponse{}
		for _, p := range items {
			out.Entries = append(out.Entries, &pb.NowPlaying{TrackId: p.ID, PlayedAt: p.Time.UTC().Format(time.RFC3339Nano)})
		}
		return out, nil
	})
}
